package smtp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	gosmtp "github.com/emersion/go-smtp"
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
}

// Server wraps go-smtp with MXHook configuration.
type Server struct {
	cfg     Config
	enqueue queue.Enqueuer
	smtpSrv *gosmtp.Server
}

// NewServer creates a new SMTP server. enqueue may be nil (uses NoopEnqueuer).
func NewServer(cfg Config, enqueue queue.Enqueuer) *Server {
	if enqueue == nil {
		enqueue = &queue.NoopEnqueuer{}
	}
	s := &Server{cfg: cfg, enqueue: enqueue}
	srv := gosmtp.NewServer(s)
	srv.Addr = cfg.Addr
	srv.Domain = cfg.Domain
	srv.MaxMessageBytes = cfg.MaxMessageBytes
	srv.MaxRecipients = cfg.MaxRecipients
	srv.AllowInsecureAuth = true
	s.smtpSrv = srv
	return s
}

// ListenAndServe starts the SMTP server.
func (s *Server) ListenAndServe() error {
	return s.smtpSrv.ListenAndServe()
}

// Close shuts down the SMTP server.
func (s *Server) Close() error {
	return s.smtpSrv.Close()
}

// NewSession is called when a client connects.
func (s *Server) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{server: s}, nil
}

type session struct {
	server *Server
	from   string
	to     []string
}

func (s *session) AuthPlain(username, _ string) error {
	return nil // auth not required for inbound MX
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if !s.isAllowedRecipient(to) {
		return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 1, 1}, Message: "recipient not accepted"}
	}
	s.to = append(s.to, to)
	return nil
}

func (s *session) isAllowedRecipient(addr string) bool {
	if len(s.server.cfg.AllowedDomains) == 0 {
		return true
	}
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])
	for _, d := range s.server.cfg.AllowedDomains {
		if strings.ToLower(d) == domain {
			return true
		}
	}
	return false
}

func (s *session) Data(r io.Reader) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("smtp: read data: %w", err)
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	msg := queue.Message{
		ID:   id,
		Body: body,
		Meta: map[string]string{
			"from": s.from,
			"to":   strings.Join(s.to, ","),
		},
	}
	return s.server.enqueue.Enqueue(context.Background(), msg)
}

func (s *session) Reset() {
	s.from = ""
	s.to = nil
}

func (s *session) Logout() error {
	return nil
}
