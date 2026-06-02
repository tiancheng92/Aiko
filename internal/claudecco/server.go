// Package claudecco provides an HTTP server that receives Claude Code hook
// events and synchronizes pet state via Wails events.
package claudecco

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	Name               string      // display name
	CWD                string      // working directory
	hasTranscriptTitle bool        // true once Name was set from transcript ai-title/rename
	state              string      // "thinking" | "idle" | "error"
	debounce           *time.Timer // per-session debounce timer
}

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
	lastSID  string // most recently active session (for status panel)
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

// Stop gracefully shuts down the HTTP server and cleans up timers.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, si := range s.sessions {
		if si.debounce != nil {
			si.debounce.Stop()
		}
	}
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
	s.cfg = cfg
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
	case "SessionStart", "PreToolUse", "PermissionRequest":
		s.setSessionState(si, "thinking")
		s.emitStatus("thinking", si, &input)

	case "UserPromptSubmit":
		s.setSessionNameFromPrompt(si, &input)
		s.setSessionState(si, "thinking")
		s.emitStatus("thinking", si, &input)

	case "Stop":
		s.setSessionState(si, "idle")
		s.emit("pet:state:change", s.aggregateState())
		s.emit("notification:show", map[string]any{
			"title":        "Claude Code",
			"message":      "Claude Code 已完成",
			"durationSecs": s.cfg.NotificationSecs,
		})
		s.refreshSessionTitle(si, &input)
		s.emitStatus("idle", si, &input)

	case "StopFailure":
		s.setSessionState(si, "error")
		s.emit("pet:state:change", s.aggregateState())
		s.emitStatus("error", si, &input)

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

// ensureSession returns the sessionInfo for the given input, creating one
// with a cwd-based fallback name if this is the first time we see it.
func (s *Server) ensureSession(input *hookInput) *sessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	si, ok := s.sessions[input.SessionID]
	if ok {
		return si
	}

	name := filepath.Base(input.CWD)
	if name == "" || name == "." || name == "/" {
		name = "session"
	}
	si = &sessionInfo{Name: name, CWD: input.CWD}
	s.sessions[input.SessionID] = si
	return si
}

// setSessionState transitions a session to a new state, managing its debounce timer.
func (s *Server) setSessionState(si *sessionInfo, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	si.state = state
	if si.debounce != nil {
		si.debounce.Stop()
		si.debounce = nil
	}

	if state == "thinking" {
		// Fire thinking after a short debounce to avoid flickering across rapid tool calls.
		sid := s.lastSID
		si.debounce = time.AfterFunc(3*time.Second, func() {
			s.mu.Lock()
			if ss, ok := s.sessions[sid]; ok && ss.state == "thinking" {
				s.emit("pet:state:change", "thinking")
			}
			s.mu.Unlock()
		})
	}
}

// aggregateState returns the highest-priority state across all sessions.
// thinking > error > idle
func (s *Server) aggregateState() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, si := range s.sessions {
		if si.state == "thinking" {
			return "thinking"
		}
	}
	for _, si := range s.sessions {
		if si.state == "error" {
			return "error"
		}
	}
	return "idle"
}

// setSessionNameFromPrompt uses the first line of the user's prompt as the
// session name. Only sets if we haven't already got a title from the transcript.
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

// refreshSessionTitle reads the session title from the transcript JSONL file.
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

// sessionSnapshot is a lightweight copy of sessionInfo for the status event.
type sessionSnapshot struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	ToolName       string `json:"toolName,omitempty"`
	HookEventName  string `json:"hookEventName,omitempty"`
}

// emitStatus sends all active sessions to the frontend.
func (s *Server) emitStatus(_ string, si *sessionInfo, input *hookInput) {
	s.mu.Lock()
	// Snapshot all sessions with state != idle.
	active := make([]sessionSnapshot, 0)
	for id, ses := range s.sessions {
		if ses.state == "idle" {
			continue
		}
		snap := sessionSnapshot{
			ID:    id,
			Name:  ses.Name,
			State: ses.state,
		}
		if id == input.SessionID {
			snap.ToolName = input.ToolName
			snap.HookEventName = input.HookEventName
		}
		active = append(active, snap)
	}
	// If nothing is active, include at least the triggering session (idle).
	if len(active) == 0 {
		active = append(active, sessionSnapshot{
			ID:             input.SessionID,
			Name:           si.Name,
			State:          "idle",
			HookEventName:  input.HookEventName,
		})
	}
	s.mu.Unlock()

	s.emit("claudecco:status", map[string]any{
		"sessions": active,
	})
}

// ── transcript reading ──

// readTranscriptTitle scans the last 64KB of a transcript JSONL file for the
// latest ai-title or rename metadata entry.
func readTranscriptTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const chunkSize = 64 * 1024
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := fi.Size() - chunkSize
	if offset < 0 {
		offset = 0
	}
	f.Seek(offset, io.SeekStart)

	scanner := bufio.NewScanner(f)
	var title string
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.Contains(line, []byte(`"ai-title"`)) {
			var entry struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(line, &entry) == nil && entry.Title != "" {
				title = entry.Title
			}
		}
		if bytes.Contains(line, []byte(`"type":"rename"`)) {
			var entry struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(line, &entry) == nil && entry.Name != "" {
				title = entry.Name
			}
		}
	}
	if title != "" && len([]rune(title)) > 40 {
		title = string([]rune(title)[:40]) + "…"
	}
	return title
}
