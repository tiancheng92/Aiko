// Package claudecco provides an HTTP server that receives Claude Code hook
// events and synchronizes pet state via Wails events.
package claudecco

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
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
	Name     string // display name: first prompt > cwd basename
	CWD      string
	hasName  bool // true once Name was set from a user prompt
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
	debounce *time.Timer
	sessions map[string]*sessionInfo
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

	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
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

	// Track session and set name from first user prompt.
	if input.SessionID != "" {
		s.updateSession(&input)
	}

	switch input.HookEventName {
	case "SessionStart", "PreToolUse", "PermissionRequest":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
		s.emit("claudecco:status", s.statusPayload("thinking", &input))

	case "UserPromptSubmit":
		// UserPromptSubmit carries the user's message — use it as session name
		// (only the first one sticks; subsequent prompts don't overwrite).
		s.setSessionNameFromPrompt(&input)
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
		s.emit("claudecco:status", s.statusPayload("thinking", &input))

	case "Stop":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "idle")
		s.emit("notification:show", map[string]any{
			"title":        "Claude Code",
			"message":      "Claude Code 已完成",
			"durationSecs": s.cfg.NotificationSecs,
		})
		s.emit("claudecco:status", s.statusPayload("idle", &input))

	case "StopFailure":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "error")
		s.emit("claudecco:status", s.statusPayload("error", &input))

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

// updateSession ensures a sessionInfo exists and tracks CWD changes.
func (s *Server) updateSession(input *hookInput) {
	s.mu.Lock()
	defer s.mu.Unlock()

	si, ok := s.sessions[input.SessionID]
	if !ok {
		name := filepath.Base(input.CWD)
		if name == "" || name == "." || name == "/" {
			name = input.SessionID[:min(8, len(input.SessionID))]
		}
		si = &sessionInfo{Name: name, CWD: input.CWD}
		s.sessions[input.SessionID] = si
	}
	si.CWD = input.CWD
}

// setSessionNameFromPrompt uses the first line of the user's prompt as the
// session display name. Only sets the name once — subsequent prompts in the
// same session won't overwrite.
func (s *Server) setSessionNameFromPrompt(input *hookInput) {
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

	si, ok := s.sessions[input.SessionID]
	if !ok {
		si = &sessionInfo{Name: name, hasName: true}
		s.sessions[input.SessionID] = si
	} else if !si.hasName {
		si.Name = name
		si.hasName = true
	}
}

// statusPayload builds the structured status event sent to the ClaudeStatusPanel.
func (s *Server) statusPayload(state string, input *hookInput) map[string]any {
	p := map[string]any{
		"state":         state,
		"hookEventName": input.HookEventName,
		"sessionID":     input.SessionID,
	}
	if input.ToolName != "" {
		p["toolName"] = input.ToolName
	}

	s.mu.Lock()
	si := s.sessions[input.SessionID]
	s.mu.Unlock()
	if si != nil {
		p["sessionName"] = si.Name
	}

	return p
}
