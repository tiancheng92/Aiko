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
	Prompt         string `json:"prompt,omitempty"`  // UserPromptSubmit
	Message        string `json:"message,omitempty"` // Notification
			NotificationType string `json:"notification_type,omitempty"` // Notification type
			Title            string `json:"title,omitempty"`             // Notification title
	AgentID        string `json:"agent_id,omitempty"`
	AgentType      string `json:"agent_type,omitempty"`
}

// sessionInfo tracks per-session state keyed by session_id (or session_id/agent_id for subagents).
type sessionInfo struct {
	Name               string    // display name
	CWD                string    // working directory
	ParentID           string    // non-empty when this is a subagent; set to the parent session_id
	hasTranscriptTitle bool      // true once Name was set from transcript ai-title/rename
	lastAgentPrompt    string    // cached from parent's PreToolUse for Agent tool
	state            string    // "thinking" | "idle" | "error" | "compact"
	idleSince        time.Time // when the session last went idle
	preCompactState  string    // state before compaction, restored on PostCompact
	toolOk               *bool     // last tool result: true=success, false=failure, nil=no result yet
	lastNotification     string    // most recent Notification message
	lastNotificationType string    // most recent Notification type
}

const idleRetention = 5 * time.Minute

// Emitter is the interface for emitting Wails events.
type Emitter func(event string, data any)

// Config provides the current Claude Code settings.
type Config struct {
	Port int
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
		s.emitStatus(si, &input)

		case "PermissionDenied":
			s.updateSessionState(si, "idle")
			s.emitStatus(si, &input)

	case "SessionStart", "PermissionRequest":
		s.updateSessionState(si, "thinking")
		s.emitStatus(si, &input)

	case "PreToolUse":
		// Cache Agent tool prompt on parent session for subagent naming.
		if input.AgentID == "" && input.ToolName == "Agent" && input.ToolInput != nil {
			s.cacheAgentPrompt(input.SessionID, input.ToolInput)
		}
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

	case "SubagentStart":
		s.updateSessionState(si, "thinking")
		s.emitStatus(si, &input)

	case "SubagentStop":
		s.updateSessionState(si, "idle")
		s.emitStatus(si, &input)

	case "PostToolUse":
		s.mu.Lock()
		ok := true
		si.toolOk = &ok
		s.mu.Unlock()
		s.emitStatus(si, &input)

	case "PostToolUseFailure":
		s.mu.Lock()
		ok := false
		si.toolOk = &ok
		s.mu.Unlock()
		s.emitStatus(si, &input)

	case "Notification":
		s.mu.Lock()
		si.lastNotification = input.Message
		si.lastNotificationType = input.NotificationType
		s.mu.Unlock()
		s.emitStatus(si, &input)

	case "PreCompact":
		s.mu.Lock()
		// PreCompact may carry agent_id when triggered inside a subagent;
		// always update the root session (session_id alone), not a subagent entry.
		if root, ok := s.sessions[input.SessionID]; ok {
			root.preCompactState = root.state
			root.state = "compact"
			si = root
		} else {
			si.preCompactState = si.state
			si.state = "compact"
		}
		s.mu.Unlock()
		s.emitStatus(si, &input)

	case "PostCompact":
		s.mu.Lock()
		// A compacted session is done — mark it and all its subagents idle.
		if root, ok := s.sessions[input.SessionID]; ok {
			root.state = "idle"
			root.idleSince = time.Now()
			root.preCompactState = ""
			si = root
		} else {
			si.state = "idle"
			si.idleSince = time.Now()
			si.preCompactState = ""
		}
		// Also idle all subagents belonging to this session.
		for key, ses := range s.sessions {
			if ses.ParentID == input.SessionID {
				ses.state = "idle"
				ses.idleSince = time.Now()
				_ = key
			}
		}
		s.mu.Unlock()
		s.emitStatus(si, &input)

	default:
		log.Info().Str("hook_event_name", input.HookEventName).Msg("claudecco: unhandled event")
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

// sessionKey returns the map key for a hook input: session_id for root sessions,
// session_id/agent_id for subagent sessions.
func sessionKey(input *hookInput) string {
	if input.AgentID != "" {
		return input.SessionID + "/" + input.AgentID
	}
	return input.SessionID
}

func (s *Server) ensureSession(input *hookInput) *sessionInfo {
	key := sessionKey(input)
	s.mu.Lock()
	si, ok := s.sessions[key]

	s.mu.Unlock()
	if ok {
		return si
	}

	// Subagent: use agent_id or parent's Agent tool prompt as name; skip transcript I/O.
	name := ""
	parentID := ""
	if input.AgentID != "" {
		parentID = input.SessionID
		// Try cached prompt from parent session.
		s.mu.Lock()
		if p, ok := s.sessions[input.SessionID]; ok && p.lastAgentPrompt != "" {
			name = p.lastAgentPrompt
		}
		s.mu.Unlock()
		if name == "" {
			name = input.AgentType
		}
		if name == "" {
			name = "subagent"
		}
	} else {
		// Root session: read transcript title.
		if input.TranscriptPath != "" {
			if t := readTranscriptTitle(input.TranscriptPath); t != "" {
				name = t
			}
		}
		if name == "" {
			name = filepath.Base(input.CWD)
			if name == "" || name == "." || name == "/" {
				name = "session"
			}
		}
	}

	hasTitle := name != "" && name != filepath.Base(input.CWD) && name != "session" && name != input.AgentType

	s.mu.Lock()
	// Double-check: another goroutine may have created it while we built the name.
	if si, ok = s.sessions[key]; ok {
		s.mu.Unlock()
		return si
	}
	si = &sessionInfo{Name: name, CWD: input.CWD, ParentID: parentID, hasTranscriptTitle: hasTitle}
	s.sessions[key] = si

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

// cacheAgentPrompt extracts the prompt from an Agent tool's tool_input and
// caches it on the parent session so that SubagentStart can use it as the
// subagent display name.
func (s *Server) cacheAgentPrompt(sessionID string, toolInput any) {
	var prompt string
	switch v := toolInput.(type) {
	case map[string]interface{}:
		if p, ok := v["prompt"].(string); ok && p != "" {
			prompt = p
		} else if d, ok := v["description"].(string); ok && d != "" {
			prompt = d
		}
	case string:
		prompt = v
	}
	if prompt == "" {
		return
	}
	name := strings.TrimSpace(prompt)
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}

	s.mu.Lock()
	if si, ok := s.sessions[sessionID]; ok {
		si.lastAgentPrompt = name
	}

	s.mu.Unlock()
}

// ── status emission ──

// stateOrder maps state strings to sort priority (lower = higher priority).
var stateOrder = map[string]int{"thinking": 0, "compact": 0, "error": 1, "idle": 2}

type sessionSnapshot struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CWD           string `json:"cwd"`
	ParentID      string `json:"parentId,omitempty"`
	State         string `json:"state"`
	ToolName      string `json:"toolName,omitempty"`
	HookEventName string `json:"hookEventName,omitempty"`
	ToolInput     string `json:"toolInput,omitempty"`
	ToolOk           *bool  `json:"toolOk,omitempty"`           // last tool result
	Notification     string `json:"notification,omitempty"`     // most recent Notification message
	NotificationType string `json:"notificationType,omitempty"` // Notification type
}

func (s *Server) emitStatus(si *sessionInfo, input *hookInput) {
	s.mu.Lock()
	now := time.Now()
	active := make([]sessionSnapshot, 0)
	hasActive := false
	for id, ses := range s.sessions {
		switch ses.state {
		case "thinking", "error", "compact":
			hasActive = true
			snap := sessionSnapshot{ID: id, Name: ses.Name, CWD: ses.CWD, ParentID: ses.ParentID, State: ses.state, ToolOk: ses.toolOk, Notification: ses.lastNotification, NotificationType: ses.lastNotificationType}
			if id == sessionKey(input) {
				snap.ToolName = input.ToolName
				snap.HookEventName = input.HookEventName
				if input.ToolInput != nil {
					if b, err := json.Marshal(input.ToolInput); err == nil {
						snap.ToolInput = string(b)
					}
				}
			}
			active = append(active, snap)
		case "idle":
			// Keep idle sessions visible for 5 minutes; prune older entries.
			if now.Sub(ses.idleSince) < idleRetention {
				snap := sessionSnapshot{ID: id, Name: ses.Name, CWD: ses.CWD, ParentID: ses.ParentID, State: "idle", ToolOk: ses.toolOk, Notification: ses.lastNotification, NotificationType: ses.lastNotificationType}
				active = append(active, snap)
			} else {
				delete(s.sessions, id)
			}
		}
		// Prune orphaned children whose parent no longer exists.
		for oid, oses := range s.sessions {
			if oses.ParentID != "" {
				if _, ok := s.sessions[oses.ParentID]; !ok {
					delete(s.sessions, oid)
				}
			}
		}

	}
	// If nothing active at all, show the triggering session as idle.
	if !hasActive && len(active) == 0 {
		active = append(active, sessionSnapshot{
			ID:            sessionKey(input),
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


		si.lastNotification = ""
		si.lastNotificationType = ""

		s.mu.Unlock()

	s.emit("claudecco:status", map[string]any{"sessions": active})
}

// ── transcript reading ──


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

// truncateTitle returns the title as-is (visual truncation is handled by the frontend CSS).
func truncateTitle(title string) string {
	return title
}
