package api

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/mxhook/mxhook/webhook"
)

//go:embed openapi.yaml
var openapiSpec []byte

// Config holds API server configuration.
type Config struct {
	ListenAddr string
	APIKey     string
}

// Option configures a Server.
type Option func(*Server)

// WithDLQ attaches a DLQ to the server.
func WithDLQ(dlq webhook.DLQ) Option {
	return func(s *Server) { s.dlq = dlq }
}

// Server is the MXHook REST API server.
type Server struct {
	cfg Config
	mux *http.ServeMux
	dlq webhook.DLQ
}

// NewServer creates an API Server.
func NewServer(cfg Config, opts ...Option) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	for _, o := range opts {
		o(s)
	}
	s.routes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("GET /dlq", s.requireAPIKey(s.handleListDLQ))
	s.mux.HandleFunc("DELETE /dlq/{id}", s.requireAPIKey(s.handleDeleteDLQ))
	s.mux.HandleFunc("POST /domains", s.requireAPIKey(s.handleCreateDomain))
	s.mux.HandleFunc("POST /routes", s.requireAPIKey(s.handleCreateRoute))
	s.mux.HandleFunc("GET /messages", s.requireAPIKey(s.handleListMessages))
	s.mux.HandleFunc("GET /messages/{id}", s.requireAPIKey(s.handleGetMessage))
	s.mux.HandleFunc("POST /replay/{id}", s.requireAPIKey(s.handleReplay))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(openapiSpec) //nolint:errcheck
}

func (s *Server) handleListDLQ(w http.ResponseWriter, r *http.Request) {
	if s.dlq == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	entries, err := s.dlq.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteDLQ(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.dlq == nil {
		http.Error(w, "dlq not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.dlq.Remove(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (s *Server) handleReplay(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey != "" && r.Header.Get("X-API-Key") != s.cfg.APIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
