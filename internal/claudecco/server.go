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

// Event represents an incoming Claude Code hook event.
type Event struct {
	Event   string `json:"event"`
	Summary string `json:"summary,omitempty"`
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

	var evt Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Debug().Str("event", evt.Event).Str("summary", evt.Summary).Msg("claudecco: received event")

	switch evt.Event {
	case "thinking":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
	case "done":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "idle")
		summary := evt.Summary
		if summary == "" {
			summary = "Claude Code 已完成"
		}
		s.emit("notification:show", map[string]any{
			"title":        "Claude Code",
			"message":      summary,
			"durationSecs": s.cfg.NotificationSecs,
		})
	default:
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}
