// Package claudecco provides an HTTP server that receives Claude Code hook
// events and synchronizes pet state via Wails events.
package claudecco

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// hookInput is the standard JSON payload that Claude Code sends to HTTP hooks.
// All hook events share this structure; event type is identified by hook_event_name.
// Ref: https://code.claude.com/docs/en/hooks
type hookInput struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name,omitempty"`
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
}

// New creates a new Server.
func New(cfg Config, emit Emitter) *Server {
	return &Server{cfg: cfg, emit: emit}
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

	var input hookInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Info().
		Str("hook_event_name", input.HookEventName).
		Str("tool_name", input.ToolName).
		Msg("claudecco: received event")

	switch input.HookEventName {
	case "PreToolUse", "PermissionRequest":
		// Both indicate Claude is actively working — debounce to avoid flickering
		// across rapid tool calls. Inspired by clawdex's state mapping:
		//   PreToolUse(Bash/Write/Edit) → running
		//   PreToolUse(Read/Grep)      → reviewing
		//   PermissionRequest          → waiting
		// We unify to "thinking" since Aiko's state system is simpler.
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
		s.emit("claudecco:status", statusPayload("thinking", input))

	case "Stop":
		// Claude finished a turn — clear thinking, show completion bubble.
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
		s.emit("claudecco:status", statusPayload("idle", input))

	case "StopFailure":
		// API error (rate limit, auth failure, server error, etc.) —
		// briefly show error state; usePetState auto-resets to idle after 3s.
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "error")
		s.emit("claudecco:status", statusPayload("error", input))

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

// statusPayload builds the structured status event sent to the ClaudeStatusPanel.
func statusPayload(state string, input hookInput) map[string]any {
	p := map[string]any{
		"state":         state,
		"hookEventName": input.HookEventName,
	}
	if input.ToolName != "" {
		p["toolName"] = input.ToolName
	}
	return p
}
