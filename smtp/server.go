package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/mxhook/mxhook/internal/metrics"
	"github.com/mxhook/mxhook/queue"
)

// Config holds SMTP server configuration.
type Config struct {
	Addr            string
	Domain          string
	AllowedDomains  []string
	MaxMessageBytes int64
	MaxRecipients   int
	TLSCertFile     string
	TLSKeyFile      string
	RequireTLS      bool
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxConnections  int
	// DomainCacheTTL bounds reuse of the domain set loaded from the durable
	// store. Zero selects defaultDomainCacheTTL.
	DomainCacheTTL time.Duration
}

// Option customises a Server at construction time.
type Option func(*Server)

// WithDomainSource makes recipient acceptance consult the durable domain
// registry in addition to the statically configured domains, so a domain added
// through the control plane is accepted without a restart.
func WithDomainSource(source DomainSource) Option {
	return func(s *Server) { s.domainSource = source }
}

// Server wraps go-smtp with MXHook configuration.
type Server struct {
	cfg           Config
	enqueue       queue.Enqueuer
	smtpSrv       *gosmtp.Server
	enqueueCtx    context.Context
	enqueueCancel context.CancelFunc
	mu            sync.Mutex
	listener      net.Listener
	stopping      bool
	dataWG        sync.WaitGroup
	domainSource  DomainSource
	domains       *domainResolver
}

// NewServer creates a validated SMTP server.
func NewServer(cfg Config, enqueue queue.Enqueuer, opts ...Option) (*Server, error) {
	if enqueue == nil {
		return nil, errors.New("smtp: durable enqueuer is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, errors.New("smtp: listen address is required")
	}
	if strings.TrimSpace(cfg.Domain) == "" {
		return nil, errors.New("smtp: server domain is required")
	}
	if len(cfg.AllowedDomains) == 0 {
		return nil, errors.New("smtp: at least one allowed recipient domain is required")
	}
	for _, domain := range cfg.AllowedDomains {
		normalized := strings.ToLower(strings.TrimRight(strings.TrimSpace(domain), "."))
		if normalized == "" || strings.Contains(normalized, "@") || strings.ContainsAny(normalized, ", \t\r\n") {
			return nil, fmt.Errorf("smtp: invalid allowed recipient domain %q", domain)
		}
	}
	allowedDomains := make([]string, len(cfg.AllowedDomains))
	for index, domain := range cfg.AllowedDomains {
		allowedDomains[index] = strings.ToLower(strings.TrimRight(strings.TrimSpace(domain), "."))
	}
	cfg.AllowedDomains = allowedDomains
	if cfg.MaxMessageBytes <= 0 {
		return nil, errors.New("smtp: maximum message size must be positive")
	}
	if cfg.MaxRecipients <= 0 {
		return nil, errors.New("smtp: maximum recipients must be positive")
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return nil, errors.New("smtp: TLS certificate and key must be configured together")
	}
	if cfg.RequireTLS && cfg.TLSCertFile == "" {
		return nil, errors.New("smtp: TLS-required mode needs a certificate and key")
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 60 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 60 * time.Second
	}
	if cfg.ReadTimeout < 0 || cfg.WriteTimeout < 0 {
		return nil, errors.New("smtp: timeouts must be positive")
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 1000
	}
	if cfg.MaxConnections < 1 {
		return nil, errors.New("smtp: maximum connections must be positive")
	}
	enqueueCtx, enqueueCancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, enqueue: enqueue, enqueueCtx: enqueueCtx, enqueueCancel: enqueueCancel}
	for _, opt := range opts {
		opt(s)
	}
	s.domains = newDomainResolver(cfg.AllowedDomains, s.domainSource, cfg.DomainCacheTTL)
	srv := gosmtp.NewServer(s)
	srv.Addr = cfg.Addr
	srv.Domain = cfg.Domain
	srv.MaxMessageBytes = cfg.MaxMessageBytes
	srv.MaxRecipients = cfg.MaxRecipients
	srv.ReadTimeout = cfg.ReadTimeout
	srv.WriteTimeout = cfg.WriteTimeout
	if cfg.TLSCertFile != "" {
		reloader, err := newCertReloader(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("smtp: load TLS certificate: %w", err)
		}
		srv.TLSConfig = &tls.Config{
			GetCertificate: reloader.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
	}
	s.smtpSrv = srv
	return s, nil
}

// ListenAndServe starts the SMTP server.
func (s *Server) ListenAndServe() error {
	// ListenAndServe has no caller context; shutdown is driven by Shutdown
	// and Close, not by cancellation.
	baseListener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("smtp: listen: %w", err)
	}
	listener := newLimitedListener(baseListener, s.cfg.MaxConnections)
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	s.listener = listener
	s.mu.Unlock()
	err = s.smtpSrv.Serve(listener)
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping || err == nil {
		return nil
	}
	return fmt.Errorf("smtp: serve: %w", err)
}

// Close shuts down the SMTP server.
func (s *Server) Close() error {
	s.mu.Lock()
	s.stopping = true
	s.enqueueCancel()
	listener := s.listener
	s.mu.Unlock()
	var listenerErr error
	if listener != nil {
		listenerErr = listener.Close()
	}
	serverErr := s.smtpSrv.Close()
	s.dataWG.Wait()
	return errors.Join(ignoreClosed(listenerErr), ignoreSMTPClosed(serverErr))
}

// Shutdown stops accepting new mail and waits for active DATA transactions.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	listener := s.listener
	s.mu.Unlock()
	var listenerErr error
	if listener != nil {
		// Filter before joining: a pre-closed listener must not let the
		// errors.Is check in ignoreClosed swallow a joined deadline error.
		listenerErr = ignoreClosed(listener.Close())
	}
	done := make(chan struct{})
	go func() {
		s.dataWG.Wait()
		close(done)
	}()
	var serverErr error
	select {
	case <-done:
		serverErr = s.smtpSrv.Close()
	case <-ctx.Done():
		s.enqueueCancel()
		// Force-close active connections so sessions blocked reading DATA
		// cannot hold the drain past the shutdown deadline.
		serverErr = s.smtpSrv.Close()
		<-done
		listenerErr = errors.Join(listenerErr, ctx.Err())
	}
	return errors.Join(listenerErr, ignoreSMTPClosed(serverErr))
}

func ignoreClosed(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func ignoreSMTPClosed(err error) error {
	if errors.Is(err, gosmtp.ErrServerClosed) {
		return nil
	}
	return ignoreClosed(err)
}

func (s *Server) beginData() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return &gosmtp.SMTPError{Code: 421, EnhancedCode: gosmtp.EnhancedCode{4, 3, 2}, Message: "server shutting down"}
	}
	s.dataWG.Add(1)
	return nil
}

type limitedListener struct {
	net.Listener
	semaphore chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newLimitedListener(listener net.Listener, maximum int) *limitedListener {
	return &limitedListener{Listener: listener, semaphore: make(chan struct{}, maximum), done: make(chan struct{})}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	select {
	case l.semaphore <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.semaphore
		return nil, err
	}
	return &limitedConnection{Conn: connection, release: func() { <-l.semaphore }}, nil
}

func (l *limitedListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type limitedConnection struct {
	net.Conn
	releaseOnce sync.Once
	release     func()
}

func (c *limitedConnection) Close() error {
	err := c.Conn.Close()
	c.releaseOnce.Do(c.release)
	return err
}

// NewSession is called when a client connects.
func (s *Server) NewSession(conn *gosmtp.Conn) (gosmtp.Session, error) {
	_, isTLS := conn.TLSConnectionState()
	return &session{server: s, tls: isTLS}, nil
}

type session struct {
	server *Server
	from   string
	to     []string
	tls    bool
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	if s.server.cfg.RequireTLS && !s.tls {
		return &gosmtp.SMTPError{Code: 530, EnhancedCode: gosmtp.EnhancedCode{5, 7, 0}, Message: "STARTTLS is required"}
	}
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	allowed, decidable := s.isAllowedRecipient(to)
	if !decidable {
		// The domain registry could not be read and nothing else vouches for
		// this recipient. Answering 550 would bounce mail for a domain that
		// may well be registered, so defer with a temporary failure and let
		// the sender retry.
		return &gosmtp.SMTPError{Code: 451, EnhancedCode: gosmtp.EnhancedCode{4, 3, 0}, Message: "recipient validation temporarily unavailable"}
	}
	if !allowed {
		return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 1, 1}, Message: "recipient not accepted"}
	}
	s.to = append(s.to, to)
	return nil
}

// isAllowedRecipient reports whether the recipient is accepted and whether the
// question could be answered at all; see domainResolver for the policy.
func (s *session) isAllowedRecipient(addr string) (allowed bool, decidable bool) {
	if strings.EqualFold(addr, "postmaster") {
		// RFC 5321 section 4.5.1: RCPT TO:<Postmaster> without a domain
		// must be supported. go-smtp's RCPT parser currently replies 501
		// to the bare form before this backend sees it (documented
		// deviation); this branch makes the backend compliant if the
		// parser starts admitting it.
		return true, true
	}
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return false, true
	}
	return s.server.domains.allows(s.server.enqueueCtx, addr[at+1:])
}

// InvalidateDomains drops the cached domain set so the next recipient check
// reloads it. Called when a domain is created or deleted through the API in
// this process; other processes converge within the cache TTL.
func (s *Server) InvalidateDomains() {
	if s.domains != nil {
		s.domains.invalidate()
	}
}

// errIngestionTemporary is the only failure detail SMTP peers see; real
// errors are logged server-side so backend internals (paths, DSNs) are
// never disclosed to arbitrary internet senders.
var errIngestionTemporary = &gosmtp.SMTPError{Code: 451, EnhancedCode: gosmtp.EnhancedCode{4, 3, 0}, Message: "temporary ingestion failure"}

func (s *session) Data(r io.Reader) error {
	if err := s.server.beginData(); err != nil {
		return err
	}
	defer s.server.dataWG.Done()
	body, err := io.ReadAll(r)
	if err != nil {
		var smtpErr *gosmtp.SMTPError
		if errors.As(err, &smtpErr) {
			// Protocol-level errors (e.g. 552 message too large) are
			// client-facing by design; pass them through unchanged.
			return smtpErr
		}
		slog.Error("smtp: read data", "err", err)
		return errIngestionTemporary
	}
	receipt, err := queue.NewReceiptHandle()
	if err != nil {
		slog.Error("smtp: create message ID", "err", err)
		return errIngestionTemporary
	}
	msg := queue.NewMessage("smtp:"+receipt, "smtp", "", s.from, s.to, time.Now(), body)
	if err := s.server.enqueue.Enqueue(s.server.enqueueCtx, msg); err != nil {
		slog.Error("smtp: enqueue message", "err", err, "message_id", msg.ID)
		return errIngestionTemporary
	}
	metrics.MessagesReceived.Inc()
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.to = nil
}

func (s *session) Logout() error {
	return nil
}
