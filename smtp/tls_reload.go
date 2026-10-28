package smtp

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"
)

// certReloader serves a TLS certificate pair from disk, reloading it when
// either file's modification time changes. This lets long-running servers
// pick up rotated certificates (e.g. 90-day ACME renewals) without a restart.
type certReloader struct {
	certFile string
	keyFile  string

	mu          sync.Mutex
	certificate *tls.Certificate
	certModTime time.Time
	keyModTime  time.Time
}

// newCertReloader loads the initial certificate pair; construction fails if
// the pair is unreadable or invalid.
func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// GetCertificate implements tls.Config.GetCertificate. A reload failure
// (e.g. half-written files mid-rotation) keeps serving the cached pair.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changedLocked() {
		if err := r.reload(); err != nil && r.certificate == nil {
			return nil, err
		}
	}
	return r.certificate, nil
}

// reload stats before loading so a swap racing the load is caught by the
// next handshake's mtime comparison.
func (r *certReloader) reload() error {
	certInfo, err := os.Stat(r.certFile)
	if err != nil {
		return fmt.Errorf("smtp: stat TLS certificate: %w", err)
	}
	keyInfo, err := os.Stat(r.keyFile)
	if err != nil {
		return fmt.Errorf("smtp: stat TLS key: %w", err)
	}
	certificate, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("smtp: load TLS key pair: %w", err)
	}
	r.certificate = &certificate
	r.certModTime = certInfo.ModTime()
	r.keyModTime = keyInfo.ModTime()
	return nil
}

func (r *certReloader) changedLocked() bool {
	certInfo, err := os.Stat(r.certFile)
	if err != nil {
		return false
	}
	keyInfo, err := os.Stat(r.keyFile)
	if err != nil {
		return false
	}
	return !certInfo.ModTime().Equal(r.certModTime) || !keyInfo.ModTime().Equal(r.keyModTime)
}
