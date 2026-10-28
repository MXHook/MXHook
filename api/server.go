package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mxhook/mxhook/webhook"
)

//go:embed openapi.yaml
var openapiSpec []byte

// Permission represents an API capability.
type Permission string

const (
	PermManageDomains  Permission = "domains:write"
	PermViewDomains    Permission = "domains:read"
	PermManageRoutes   Permission = "routes:write"
	PermViewRoutes     Permission = "routes:read"
	PermManageWebhooks Permission = "webhooks:write"
	PermViewLogs       Permission = "logs:read"
	PermManageKeys     Permission = "keys:write"
	PermViewAnalytics  Permission = "analytics:read"
	PermViewAudit      Permission = "audit:read"
)

// Authorizer handles API key validation and permission checking (implemented by EE).
type Authorizer interface {
	Authorize(r *http.Request, perm Permission) (*http.Request, error)
}

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

// WithAuthorizer enables custom authorization (e.g. EE RBAC).
func WithAuthorizer(a Authorizer) Option {
	return func(s *Server) { s.auth = a }
}

// WithRepository attaches durable control-plane storage.
func WithRepository(repository Repository) Option {
	return func(s *Server) { s.repository = repository }
}

// DomainInvalidator is notified when the registered domain set changes, so an
// ingress component caching that set can drop its copy immediately instead of
// waiting for its TTL.
type DomainInvalidator interface {
	InvalidateDomains()
}

// WithDomainInvalidator attaches a component to notify after domain writes.
func WithDomainInvalidator(invalidator DomainInvalidator) Option {
	return func(s *Server) { s.domainInvalidator = invalidator }
}

// WithReadinessChecker attaches a dependency readiness check.
func WithReadinessChecker(checker ReadinessChecker) Option {
	return func(s *Server) { s.readiness = checker }
}

// WithMetricsHandler exposes a Prometheus-compatible metrics handler.
func WithMetricsHandler(handler http.Handler) Option {
	return func(s *Server) { s.metrics = handler }
}

// WithEndpointValidator validates webhook policy before route persistence.
func WithEndpointValidator(deliverer *webhook.Deliverer) Option {
	return func(s *Server) { s.endpointValidator = deliverer }
}

// Server is the MXHook REST API server.
type Server struct {
	cfg               Config
	mux               *http.ServeMux
	dlq               webhook.DLQ
	domains           *DomainStore
	routes            *RouteStore
	auth              Authorizer
	repository        Repository
	readiness         ReadinessChecker
	httpServer        *http.Server
	handler           http.Handler
	metrics           http.Handler
	endpointValidator *webhook.Deliverer
	domainInvalidator DomainInvalidator
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
	s.handler = requestIDMiddleware(s.mux)
	s.httpServer = &http.Server{
		Addr: s.cfg.ListenAddr, Handler: s.handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 32 * 1024,
	}
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) ListenAndServe() error {
	if err := s.httpServer.ListenAndServe(); err != nil {
		return fmt.Errorf("api: serve: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("api: shutdown: %w", err)
	}
	return nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleReady)
	s.mux.HandleFunc("GET /healthz", s.handleLive)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics)
	}
	s.mux.HandleFunc("GET /openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("GET /dlq", s.requirePerm(PermViewLogs, s.handleListDLQ))
	s.mux.HandleFunc("DELETE /dlq/{id}", s.requirePerm(PermManageWebhooks, s.handleDeleteDLQ))
	s.mux.HandleFunc("POST /domains", s.requirePerm(PermManageDomains, s.handleCreateDomain))
	s.mux.HandleFunc("GET /domains", s.requirePerm(PermViewDomains, s.handleListDomains))
	s.mux.HandleFunc("DELETE /domains/{id}", s.requirePerm(PermManageDomains, s.handleDeleteDomain))
	s.mux.HandleFunc("POST /routes", s.requirePerm(PermManageRoutes, s.handleCreateRoute))
	s.mux.HandleFunc("GET /routes", s.requirePerm(PermViewRoutes, s.handleListRoutes))
	s.mux.HandleFunc("DELETE /routes/{id}", s.requirePerm(PermManageRoutes, s.handleDeleteRoute))
	s.mux.HandleFunc("GET /messages", s.requirePerm(PermViewLogs, s.handleListMessages))
	s.mux.HandleFunc("GET /messages/{id}", s.requirePerm(PermViewLogs, s.handleGetMessage))
	s.mux.HandleFunc("POST /replay/{id}", s.requirePerm(PermManageWebhooks, s.handleReplay))
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{keyStatus: "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.readiness.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{keyStatus: "not_ready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{keyStatus: "ok"})
}

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiSpec) // headers already sent; a client disconnect is not actionable
}

func (s *Server) handleListDLQ(w http.ResponseWriter, r *http.Request) {
	if s.repository != nil {
		page, err := parsePage(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		entries, err := s.repository.ListDeadDeliveries(r.Context(), page)
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entries)
		return
	}
	if s.dlq == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	entries, err := s.dlq.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteDLQ(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.repository != nil {
		const ingressPrefix = "ingress:"
		if len(id) > len(ingressPrefix) && id[:len(ingressPrefix)] == ingressPrefix {
			if err := s.repository.DeleteIngressFailure(r.Context(), id[len(ingressPrefix):]); err != nil {
				handleRepositoryError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "dead deliveries are retained; schedule a replay instead")
		return
	}
	if s.dlq == nil {
		writeError(w, http.StatusServiceUnavailable, "dlq not configured")
		return
	}
	if err := s.dlq.Remove(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if s.repository != nil {
		domain, err := s.repository.CreateDomain(r.Context(), body.Name)
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		s.invalidateDomains()
		writeJSON(w, http.StatusCreated, domain)
		return
	}
	d := s.domains.Add(body.Name)
	s.invalidateDomains()
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	if s.repository != nil {
		domains, err := s.repository.ListDomains(r.Context())
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, domains)
		return
	}
	writeJSON(w, http.StatusOK, s.domains.List())
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.repository != nil {
		if err := s.repository.DeleteDomain(r.Context(), id); err != nil {
			handleRepositoryError(w, err)
			return
		}
		s.invalidateDomains()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.domains.Delete(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.invalidateDomains()
	w.WriteHeader(http.StatusNoContent)
}

// invalidateDomains notifies the ingress cache, if one is attached, that the
// registered domain set changed.
func (s *Server) invalidateDomains() {
	if s.domainInvalidator != nil {
		s.domainInvalidator.InvalidateDomains()
	}
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Domain           string `json:"domain"`
		RecipientPattern string `json:"recipient_pattern"`
		WebhookURL       string `json:"webhook_url"`
		WebhookSecret    string `json:"webhook_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WebhookURL == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	route := Route{Domain: body.Domain, RecipientPattern: body.RecipientPattern, WebhookURL: body.WebhookURL, WebhookSecret: body.WebhookSecret}
	if s.repository != nil {
		if body.WebhookSecret == "" {
			writeError(w, http.StatusBadRequest, "webhook_secret is required")
			return
		}
		if s.endpointValidator != nil {
			if err := s.endpointValidator.ValidateEndpoint(r.Context(), webhook.Endpoint{URL: body.WebhookURL, Secret: body.WebhookSecret}); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		created, err := s.repository.CreateRoute(r.Context(), route)
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
		return
	}
	rt := s.routes.Add(route)
	writeJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	if s.repository != nil {
		routes, err := s.repository.ListRoutes(r.Context())
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, routes)
		return
	}
	writeJSON(w, http.StatusOK, s.routes.List())
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.repository != nil {
		if err := s.repository.DeleteRoute(r.Context(), id); err != nil {
			handleRepositoryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.routes.Delete(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	if s.repository != nil {
		page, err := parsePage(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		messages, err := s.repository.ListMessages(r.Context(), page)
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messages)
		return
	}
	writeJSON(w, http.StatusOK, []Message{})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.repository != nil {
		message, err := s.repository.GetMessage(r.Context(), id)
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, message)
		return
	}
	writeError(w, http.StatusNotFound, "message not found: "+id)
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if s.repository != nil {
		count, err := s.repository.ScheduleReplay(r.Context(), r.PathValue("id"))
		if err != nil {
			handleRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{keyStatus: "queued", "deliveries": count})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{keyStatus: "queued"})
}

func (s *Server) requirePerm(p Permission, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth != nil {
			newReq, err := s.auth.Authorize(r, p)
			if err != nil {
				// A caller who presented no credential is unauthenticated;
				// one who presented a valid credential without the required
				// permission is forbidden. An unclassified error stays
				// forbidden so a new failure mode cannot leak access.
				if errors.Is(err, ErrUnauthenticated) {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next(w, newReq)
			return
		}

		// OSS path: static key comparison. With no key configured there is no
		// way to authenticate anyone, so the control plane fails closed
		// rather than serving every caller.
		if s.cfg.APIKey == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(s.cfg.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func handleRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// keyStatus is the JSON field carrying health and replay outcomes.
const keyStatus = "status"

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	// Headers are already sent; an encode failure here is a client
	// disconnect, which the handler cannot act on.
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}

func parsePage(r *http.Request) (Page, error) {
	page := Page{Limit: 50}
	for name, target := range map[string]*int{"limit": &page.Limit, "offset": &page.Offset} {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || name == "limit" && value == 0 {
			return Page{}, errors.New("invalid pagination")
		}
		*target = value
	}
	if page.Limit > 100 {
		page.Limit = 100
	}
	return page, nil
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var random [16]byte
		if _, err := rand.Read(random[:]); err == nil {
			w.Header().Set("X-Request-ID", hex.EncodeToString(random[:]))
		}
		next.ServeHTTP(w, r)
	})
}
