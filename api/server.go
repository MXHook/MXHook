package api

import (
	"encoding/json"
	"net/http"

	"github.com/mxhook/mxhook/webhook"
)

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
	cfg     Config
	mux     *http.ServeMux
	dlq     webhook.DLQ
	domains *DomainStore
	routes  *RouteStore
}

// NewServer creates an API Server.
func NewServer(cfg Config, opts ...Option) *Server {
	s := &Server{
		cfg:     cfg,
		mux:     http.NewServeMux(),
		domains: NewDomainStore(),
		routes:  NewRouteStore(),
	}
	for _, o := range opts {
		o(s)
	}
	s.registerRoutes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /dlq", s.requireAPIKey(s.handleListDLQ))
	s.mux.HandleFunc("DELETE /dlq/{id}", s.requireAPIKey(s.handleDeleteDLQ))
	s.mux.HandleFunc("POST /domains", s.requireAPIKey(s.handleCreateDomain))
	s.mux.HandleFunc("GET /domains", s.requireAPIKey(s.handleListDomains))
	s.mux.HandleFunc("DELETE /domains/{id}", s.requireAPIKey(s.handleDeleteDomain))
	s.mux.HandleFunc("POST /routes", s.requireAPIKey(s.handleCreateRoute))
	s.mux.HandleFunc("GET /routes", s.requireAPIKey(s.handleListRoutes))
	s.mux.HandleFunc("DELETE /routes/{id}", s.requireAPIKey(s.handleDeleteRoute))
	s.mux.HandleFunc("GET /messages", s.requireAPIKey(s.handleListMessages))
	s.mux.HandleFunc("GET /messages/{id}", s.requireAPIKey(s.handleGetMessage))
	s.mux.HandleFunc("POST /replay/{id}", s.requireAPIKey(s.handleReplay))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	d := s.domains.Add(body.Name)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleListDomains(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.domains.List())
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.domains.Delete(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var body Route
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WebhookURL == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	rt := s.routes.Add(body)
	writeJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleListRoutes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.routes.List())
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.routes.Delete(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMessages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []Message{})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	http.Error(w, "message not found: "+id, http.StatusNotFound)
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
