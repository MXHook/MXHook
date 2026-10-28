package smtp_test

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"testing"
	"time"

	mxsmtp "github.com/mxhook/mxhook/smtp"
)

func TestNewServerRejectsPartialTLSConfiguration(t *testing.T) {
	cfg := defaultCfg()
	cfg.TLSCertFile = "certificate.pem"
	if _, err := mxsmtp.NewServer(cfg, &countingEnqueuer{}); err == nil {
		t.Fatal("NewServer accepted a TLS certificate without a key")
	}
}

func TestSMTPRequireTLSRejectsPlaintextMail(t *testing.T) {
	certFile, keyFile, roots := testCertificate(t)
	cfg := defaultCfg()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	cfg.RequireTLS = true
	enqueuer := &countingEnqueuer{}
	addr := startServer(t, cfg, enqueuer)

	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail("sender@example.com"); err == nil {
		t.Fatal("plaintext MAIL accepted while TLS was required")
	}
	if err := client.StartTLS(&tls.Config{ServerName: "example.com", RootCAs: roots, MinVersion: tls.VersionTLS12}); err != nil {
		t.Fatalf("STARTTLS: %v", err)
	}
	if err := client.Mail("sender@example.com"); err != nil {
		t.Fatalf("MAIL after STARTTLS: %v", err)
	}
	if err := client.Rcpt("recipient@example.com"); err != nil {
		t.Fatalf("RCPT after STARTTLS: %v", err)
	}
	w, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: TLS\r\n\r\nsecure\r\n")); err != nil {
		t.Fatalf("write DATA: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}
	if enqueuer.Count() != 1 {
		t.Fatalf("enqueued %d messages, want 1", enqueuer.Count())
	}
}

func TestSMTPRejectsLegacyTLSVersions(t *testing.T) {
	certFile, keyFile, roots := testCertificate(t)
	cfg := defaultCfg()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	addr := startServer(t, cfg, &countingEnqueuer{})

	for name, version := range map[string]uint16{
		"TLS1.0": tls.VersionTLS10,
		"TLS1.1": tls.VersionTLS11,
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer func() { _ = conn.Close() }()
			reader := bufio.NewReader(conn)
			expectSMTPReply(t, reader, "220")
			if _, err := fmt.Fprint(conn, "EHLO client.example.net\r\n"); err != nil {
				t.Fatalf("write EHLO: %v", err)
			}
			expectSMTPReply(t, reader, "250")
			if _, err := fmt.Fprint(conn, "STARTTLS\r\n"); err != nil {
				t.Fatalf("write STARTTLS: %v", err)
			}
			expectSMTPReply(t, reader, "220")
			tlsConn := tls.Client(conn, &tls.Config{
				ServerName: "example.com",
				RootCAs:    roots,
				MinVersion: version,
				MaxVersion: version,
			})
			if err := tlsConn.Handshake(); err == nil {
				_ = tlsConn.Close()
				t.Fatalf("%s handshake accepted; server must require TLS 1.2+", name)
			}
		})
	}
}

func TestSMTPTLSCertificateHotReload(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "certificate.pem")
	keyFile := filepath.Join(dir, "key.pem")
	first := writeCertificatePair(t, certFile, keyFile, "first.example.com")
	cfg := defaultCfg()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	addr := startServer(t, cfg, &countingEnqueuer{})

	roots := x509.NewCertPool()
	roots.AddCert(first)
	if got := starttlsLeafSerial(t, addr, roots); got.Cmp(first.SerialNumber) != 0 {
		t.Fatalf("initial handshake serial = %v, want %v", got, first.SerialNumber)
	}
	second := writeCertificatePair(t, certFile, keyFile, "second.example.com")
	roots.AddCert(second)
	bumpModTime(t, certFile, keyFile)
	if got := starttlsLeafSerial(t, addr, roots); got.Cmp(second.SerialNumber) != 0 {
		t.Fatalf("post-rotation handshake serial = %v, want %v (rotated certificate not served)", got, second.SerialNumber)
	}
}

func starttlsLeafSerial(t *testing.T, addr string, roots *x509.CertPool) *big.Int {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	expectSMTPReply(t, reader, "220")
	if _, err := fmt.Fprint(conn, "EHLO client.example.net\r\n"); err != nil {
		t.Fatalf("write EHLO: %v", err)
	}
	expectSMTPReply(t, reader, "250")
	if _, err := fmt.Fprint(conn, "STARTTLS\r\n"); err != nil {
		t.Fatalf("write STARTTLS: %v", err)
	}
	expectSMTPReply(t, reader, "220")
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: "example.com",
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake: %v", err)
	}
	defer func() { _ = tlsConn.Close() }()
	peers := tlsConn.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		t.Fatal("no peer certificates presented")
	}
	// Quit gracefully so the server session finishes its STARTTLS upgrade
	// before test cleanup closes the server.
	tlsReader := bufio.NewReader(tlsConn)
	if _, err := fmt.Fprint(tlsConn, "QUIT\r\n"); err != nil {
		t.Fatalf("write QUIT: %v", err)
	}
	expectSMTPReply(t, tlsReader, "221")
	return peers[0].SerialNumber
}

func bumpModTime(t *testing.T, files ...string) {
	t.Helper()
	future := time.Now().Add(2 * time.Second)
	for _, file := range files {
		if err := os.Chtimes(file, future, future); err != nil {
			t.Fatalf("chtimes %s: %v", file, err)
		}
	}
}

func testCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	certFile := filepath.Join(dir, "certificate.pem")
	keyFile := filepath.Join(dir, "key.pem")
	certificate := writeCertificatePair(t, certFile, keyFile, "example.com")
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return certFile, keyFile, roots
}

func writeCertificatePair(t *testing.T, certFile, keyFile, commonName string) *x509.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     []string{"example.com", commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return mustParseCertificate(t, der)
}

func mustParseCertificate(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}
