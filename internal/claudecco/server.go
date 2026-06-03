// Package claudecco provides an HTTP server that receives Claude Code hook
// events and synchronizes pet state via Wails events.
package claudecco

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// hookInput is the standard JSON payload that Claude Code sends to HTTP hooks.
// Ref: https://code.claude.com/docs/en/hooks
type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	PermissionMode string `json:"permission_mode"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolInput      any    `json:"tool_input,omitempty"`
	ErrorType      string `json:"error_type,omitempty"`
	Prompt         string `json:"prompt,omitempty"` // UserPromptSubmit
}

// sessionInfo tracks per-session state keyed by session_id.
type sessionInfo struct {
	Name               string    // display name
	CWD                string    // working directory
	ParentID           string    // non-empty when this is a subagent; set to the parent session_id
	hasTranscriptTitle bool      // true once Name was set from transcript ai-title/rename
	state              string    // "thinking" | "idle" | "error"
	idleSince          time.Time // when the session last went idle
}

const idleRetention = 5 * time.Minute

// Emitter is the interface for emitting Wails events.
type Emitter func(event string, data any)

// Config provides the current Claude Code settings.
type Config struct {
	Port             int
	NotificationSecs int
}

// Server listens for Claude Code hook HTTP requests and emits pet state events.
type Server struct {
	cfg      Config
	emit     Emitter
	srv      *http.Server
	mu       sync.Mutex
	sessions map[string]*sessionInfo
	lastSID  string // most recently active session ID
}

// New creates a new Server.
func New(cfg Config, emit Emitter) *Server {
	return &Server{cfg: cfg, emit: emit, sessions: make(map[string]*sessionInfo)}
}

// Start begins listening on 127.0.0.1:<port>.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil {
		s.srv.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/event", s.handleEvent)
	mux.HandleFunc("/health", s.handleHealth)

	s.srv = &http.Server{
		Addr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.Port)),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("claudecco: server error")
		}
	}()

	log.Info().Str("addr", s.srv.Addr).Msg("claudecco: server started")
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
		s.srv = nil
		log.Info().Msg("claudecco: server stopped")
	}
}

// UpdateConfig applies new settings. Restarts the server if the port changed.
func (s *Server) UpdateConfig(cfg Config) error {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	if s.srv != nil {
		return s.Start()
	}
	return nil
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusInternalServerError)
		return
	}
	log.Info().Str("body", string(body)).Msg("claudecco: raw hook payload")

	var input hookInput
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Info().
		Str("hook_event_name", input.HookEventName).
		Str("tool_name", input.ToolName).
		Str("session_id", input.SessionID).
		Str("cwd", input.CWD).
		Msg("claudecco: received event")

	if input.SessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
		return
	}

	si := s.ensureSession(&input)
	s.lastSID = input.SessionID

	switch input.HookEventName {

	case "SessionEnd":
		s.markAllIdle()

	case "SessionStart", "PreToolUse", "PermissionRequest":
		s.updateSessionState(si, "thinking")
		s.emitStatus(si, &input)

	case "UserPromptSubmit":
		s.setSessionNameFromPrompt(si, &input)
		s.updateSessionState(si, "thinking")
		s.emitStatus(si, &input)

	case "Stop":
		s.updateSessionState(si, "idle")
		s.refreshSessionTitle(si, &input)
		s.emitStatus(si, &input)

	case "StopFailure":
		s.updateSessionState(si, "error")
		s.emitStatus(si, &input)

	default:
		log.Debug().Str("hook_event_name", input.HookEventName).Msg("claudecco: ignored event")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","port":` + strconv.Itoa(s.cfg.Port) + `}`))
}

// ── session helpers ──

func (s *Server) ensureSession(input *hookInput) *sessionInfo {
	s.mu.Lock()
	si, ok := s.sessions[input.SessionID]
	s.mu.Unlock()
	if ok {
		return si
	}

	// Primary source: JSONL transcript (catches resumed sessions with existing titles).
	// Read title outside the lock — it performs filesystem I/O and should not block other sessions.
	name := ""
	if input.TranscriptPath != "" {
		if t := readTranscriptTitle(input.TranscriptPath); t != "" {
			name = t
		}
	}
	// Fallback 1: cwd directory name.
	if name == "" {
		name = filepath.Base(input.CWD)
		if name == "" || name == "." || name == "/" {
			name = "session"
		}
	}

	hasTitle := name != "" && name != filepath.Base(input.CWD) && name != "session"
	parentID := parentSessionID(input.TranscriptPath)

	s.mu.Lock()
	// Double-check: another goroutine may have created it while we read the transcript.
	if si, ok = s.sessions[input.SessionID]; ok {
		s.mu.Unlock()
		return si
	}
	si = &sessionInfo{Name: name, CWD: input.CWD, ParentID: parentID, hasTranscriptTitle: hasTitle}
	s.sessions[input.SessionID] = si
	s.mu.Unlock()
	return si
}

// updateSessionState transitions a session to a new state (panel only, no pet/notification).
func (s *Server) updateSessionState(si *sessionInfo, state string) {
	s.mu.Lock()
	if state == "idle" && si.state != "idle" {
		si.idleSince = time.Now()
	}
	si.state = state
	s.mu.Unlock()
}

// markAllIdle sets all sessions to idle — used on SessionEnd (exit/interrupt).
func (s *Server) markAllIdle() {
	s.mu.Lock()
	now := time.Now()
	for _, si := range s.sessions {
		si.state = "idle"
		si.idleSince = now
	}
	s.mu.Unlock()
}

// aggregateState returns the highest-priority state across all sessions.
func (s *Server) aggregateState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var hasError bool
	for _, si := range s.sessions {
		if si.state == "thinking" {
			return "thinking"
		}
		if si.state == "error" {
			hasError = true
		}
	}
	if hasError {
		return "error"
	}
	return "idle"
}

// aggregateStateLocked is the locked variant (caller holds s.mu). Not used
// outside of this file — kept for potential internal use.
func (s *Server) aggregateStateLocked() string {
	var hasError bool
	for _, si := range s.sessions {
		if si.state == "thinking" {
			return "thinking"
		}
		if si.state == "error" {
			hasError = true
		}
	}
	if hasError {
		return "error"
	}
	return "idle"
}

func (s *Server) setSessionNameFromPrompt(si *sessionInfo, input *hookInput) {
	if input.Prompt == "" {
		return
	}
	name := strings.TrimSpace(input.Prompt)
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	if name == "" {
		return
	}
	if len([]rune(name)) > 40 {
		name = string([]rune(name)[:40]) + "…"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !si.hasTranscriptTitle {
		si.Name = name
	}
}

func (s *Server) refreshSessionTitle(si *sessionInfo, input *hookInput) {
	if input.TranscriptPath == "" {
		return
	}
	title := readTranscriptTitle(input.TranscriptPath)
	if title == "" {
		return
	}

	s.mu.Lock()
	si.Name = title
	si.hasTranscriptTitle = true
	s.mu.Unlock()
}

// ── status emission ──

// stateOrder maps state strings to sort priority (lower = higher priority).
var stateOrder = map[string]int{"thinking": 0, "error": 1, "idle": 2}

type sessionSnapshot struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CWD           string `json:"cwd"`
	ParentID      string `json:"parentId,omitempty"`
	State         string `json:"state"`
	ToolName      string `json:"toolName,omitempty"`
	HookEventName string `json:"hookEventName,omitempty"`
	ToolInput     string `json:"toolInput,omitempty"`
}

func (s *Server) emitStatus(si *sessionInfo, input *hookInput) {
	s.mu.Lock()
	now := time.Now()
	active := make([]sessionSnapshot, 0)
	hasActive := false
	for id, ses := range s.sessions {
		switch ses.state {
		case "thinking", "error":
			hasActive = true
			snap := sessionSnapshot{ID: id, Name: ses.Name, CWD: ses.CWD, ParentID: ses.ParentID, State: ses.state}
			if id == input.SessionID {
				snap.ToolName = input.ToolName
				snap.HookEventName = input.HookEventName
				if input.ToolInput != nil {
					if b, err := json.Marshal(input.ToolInput); err == nil {
						inputStr := string(b)
						runes := []rune(inputStr)
						if len(runes) > 120 {
							inputStr = string(runes[:120]) + "…"
						}
						snap.ToolInput = inputStr
					}
				}
			}
			active = append(active, snap)
		case "idle":
			// Keep idle sessions visible for 5 minutes; prune older entries.
			if now.Sub(ses.idleSince) < idleRetention {
				snap := sessionSnapshot{ID: id, Name: ses.Name, CWD: ses.CWD, ParentID: ses.ParentID, State: "idle"}
				active = append(active, snap)
			} else {
				delete(s.sessions, id)
			}
		}
	}
	// If nothing active at all, show the triggering session as idle.
	if !hasActive && len(active) == 0 {
		active = append(active, sessionSnapshot{
			ID:            input.SessionID,
			Name:          si.Name,
			CWD:           si.CWD,
			ParentID:      si.ParentID,
			State:         "idle",
			HookEventName: input.HookEventName,
		})
	}

	// Sort: CWD → state (thinking > error > idle) → name.
	sort.SliceStable(active, func(i, j int) bool {
		a, b := active[i], active[j]
		if a.CWD != b.CWD {
			return a.CWD < b.CWD
		}
		if a.State != b.State {
			return stateOrder[a.State] < stateOrder[b.State]
		}
		return a.Name < b.Name
	})

	s.mu.Unlock()

	s.emit("claudecco:status", map[string]any{"sessions": active})
}

// ── transcript reading ──

// parentSessionID extracts the parent session ID from a subagent transcript path.
// Subagent paths follow the pattern: .../projects/<proj>/<parentID>/subagents/agent-*.jsonl
// Returns empty string for root session transcripts.
func parentSessionID(transcriptPath string) string {
	const marker = "/subagents/"
	idx := strings.LastIndex(transcriptPath, marker)
	if idx < 0 {
		return ""
	}
	return filepath.Base(transcriptPath[:idx])
}

func readTranscriptTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const chunkSize = 64 * 1024

	// ── Pass 1: scan the tail for custom-title / ai-title ──
	title := readTailTitles(f, chunkSize)
	if title != "" {
		return truncateTitle(title)
	}

	// ── Pass 2: fallback to the first user message from the head ──
	title = readHeadUserMessage(f)
	return truncateTitle(title)
}

// readTailTitles scans the last chunkSize bytes for custom-title or ai-title entries.
func readTailTitles(f *os.File, chunkSize int64) string {
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := max(fi.Size()-chunkSize, 0)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}

	scanner := bufio.NewScanner(f)
	var title string
	var hasCustom bool // once custom-title is found, skip ai-title

	for scanner.Scan() {
		line := scanner.Bytes()

		var entryType struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &entryType) != nil {
			continue
		}

		switch entryType.Type {
		case "custom-title":
			var entry struct {
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(line, &entry) == nil && entry.CustomTitle != "" {
				title = entry.CustomTitle
				hasCustom = true
			}

		case "ai-title":
			if hasCustom {
				continue
			}
			var entry struct {
				AITitle string `json:"aiTitle"`
			}
			if json.Unmarshal(line, &entry) == nil && entry.AITitle != "" {
				title = entry.AITitle
			}
		}
	}

	return title
}

// readHeadUserMessage scans from the beginning of the file for the first
// user message with text content (skipping tool_result blocks).
func readHeadUserMessage(f *os.File) string {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()

		var entryType struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &entryType) != nil {
			continue
		}
		if entryType.Type != "user" {
			continue
		}

		var entry struct {
			UUID    string `json:"uuid"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.UUID == "" {
			continue
		}
		for _, c := range entry.Message.Content {
			if c.Type == "text" {
				if t := strings.TrimSpace(c.Text); t != "" {
					return t
				}
			}
		}
	}

	return ""
}

// truncateTitle truncates a title to 40 runes and appends "…" if needed.
func truncateTitle(title string) string {
	if title == "" {
		return ""
	}
	if len([]rune(title)) > 40 {
		title = string([]rune(title)[:40]) + "…"
	}
	return title
}
