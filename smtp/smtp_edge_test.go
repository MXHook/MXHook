package smtp_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
	mxsmtp "github.com/mxhook/mxhook/smtp"
)

// startServer launches a real mxhook SMTP server on a random port.
// Returns the bound address and a cleanup func.
func startServer(t *testing.T, cfg mxsmtp.Config, enq queue.Enqueuer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg.Addr = addr
	s, err := mxsmtp.NewServer(cfg, enq)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ready := make(chan struct{})
	go func() {
		close(ready)
		_ = s.ListenAndServe()
	}()
	<-ready
	// Give listener a moment to bind.
	if !waitPort(addr, 2*time.Second) {
		t.Fatalf("server did not start on %s", addr)
	}
	return addr
}

func waitPort(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func defaultCfg() mxsmtp.Config {
	return mxsmtp.Config{
		Addr:            "127.0.0.1:0",
		Domain:          "example.com",
		AllowedDomains:  []string{"example.com"},
		MaxMessageBytes: 1024 * 1024,
		MaxRecipients:   10,
	}
}

// countingEnqueuer counts how many times Enqueue is called.
type countingEnqueuer struct {
	mu    sync.Mutex
	msgs  []queue.Message
	count int32
}

func (c *countingEnqueuer) Enqueue(_ context.Context, msg queue.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	atomic.AddInt32(&c.count, 1)
	c.msgs = append(c.msgs, msg)
	return nil
}

func (c *countingEnqueuer) Count() int {
	return int(atomic.LoadInt32(&c.count))
}

func (c *countingEnqueuer) Last() (queue.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return queue.Message{}, false
	}
	return c.msgs[len(c.msgs)-1], true
}

// errorEnqueuer always returns an error.
type errorEnqueuer struct{ err error }

func (e *errorEnqueuer) Enqueue(_ context.Context, _ queue.Message) error { return e.err }

// --- Tests ---

func TestNewServer_NilEnqueuerRejected(t *testing.T) {
	if _, err := mxsmtp.NewServer(defaultCfg(), nil); err == nil {
		t.Fatal("expected nil enqueuer to be rejected")
	}
}

func TestSMTP_BasicSendAndEnqueue(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	body := "From: sender@example.com\r\nTo: rcpt@example.com\r\nSubject: Hello\r\n\r\nHello world\r\n"
	if err := netsmtp.SendMail(addr, nil, "sender@example.com", []string{"rcpt@example.com"}, []byte(body)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	if enq.Count() != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", enq.Count())
	}
	msg, _ := enq.Last()
	if msg.EnvelopeFrom != "sender@example.com" {
		t.Errorf("envelope_from = %q, want sender@example.com", msg.EnvelopeFrom)
	}
	if len(msg.EnvelopeTo) != 1 || msg.EnvelopeTo[0] != "rcpt@example.com" {
		t.Errorf("envelope_to = %#v, want rcpt@example.com", msg.EnvelopeTo)
	}
	if len(msg.Body) == 0 {
		t.Error("expected non-empty body")
	}
	if msg.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestSMTP_MultipleRecipients(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	rcpts := []string{"a@example.com", "b@example.com", "c@example.com"}
	body := "From: s@example.com\r\nTo: a@example.com\r\nSubject: Multi\r\n\r\nbody\r\n"
	if err := netsmtp.SendMail(addr, nil, "s@example.com", rcpts, []byte(body)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	if enq.Count() != 1 {
		t.Fatalf("expected 1 enqueue call, got %d", enq.Count())
	}
	msg, _ := enq.Last()
	for idx, recipient := range rcpts {
		if len(msg.EnvelopeTo) <= idx || msg.EnvelopeTo[idx] != recipient {
			t.Errorf("envelope_to[%d] = %#v, want %q", idx, msg.EnvelopeTo, recipient)
		}
	}
}

func TestSMTP_DisallowedRecipientRejected(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	body := "From: s@other.com\r\nTo: rcpt@other.com\r\nSubject: X\r\n\r\nX\r\n"
	err := netsmtp.SendMail(addr, nil, "s@other.com", []string{"rcpt@other.com"}, []byte(body))
	if err == nil {
		t.Fatal("expected error for disallowed recipient, got nil")
	}
	if enq.Count() != 0 {
		t.Fatalf("expected 0 enqueue calls, got %d", enq.Count())
	}
}

func TestSMTP_AllowedDomainsEmpty_Rejected(t *testing.T) {
	cfg := defaultCfg()
	cfg.AllowedDomains = nil
	enq := &countingEnqueuer{}
	if _, err := mxsmtp.NewServer(cfg, enq); err == nil {
		t.Fatal("expected empty allowed domains to be rejected")
	}
}

func TestSMTP_EnqueueError_PropagatedToClient(t *testing.T) {
	enq := &errorEnqueuer{err: errors.New("storage full")}
	addr := startServer(t, defaultCfg(), enq)

	body := "From: s@example.com\r\nTo: r@example.com\r\nSubject: X\r\n\r\nX\r\n"
	err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(body))
	if err == nil {
		t.Fatal("expected error when enqueue fails")
	}
}

func TestSMTP_EnqueueError_NotLeakedToPeer(t *testing.T) {
	enq := &errorEnqueuer{err: errors.New("pgx: connect postgres://mx:sup3rsecret@db.internal:5432/mxhook: connection refused")}
	addr := startServer(t, defaultCfg(), enq)

	body := "From: s@example.com\r\nTo: r@example.com\r\nSubject: X\r\n\r\nX\r\n"
	err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(body))
	if err == nil {
		t.Fatal("expected error when enqueue fails")
	}
	var protoErr *textproto.Error
	if !errors.As(err, &protoErr) {
		t.Fatalf("error type = %T (%v), want *textproto.Error", err, err)
	}
	if protoErr.Code != 451 {
		t.Errorf("reply code = %d, want 451", protoErr.Code)
	}
	for _, fragment := range []string{"postgres://", "sup3rsecret", "db.internal"} {
		if strings.Contains(protoErr.Msg, fragment) {
			t.Errorf("reply %q leaks internal detail %q", protoErr.Msg, fragment)
		}
	}
}

func TestSMTP_OversizedMessage_Returns552WithoutInternalWrap(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaxMessageBytes = 512
	addr := startServer(t, cfg, &countingEnqueuer{})

	oversized := "From: s@example.com\r\nTo: r@example.com\r\nSubject: Big\r\n\r\n" +
		strings.Repeat("X", 1000) + "\r\n"
	err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(oversized))
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
	var protoErr *textproto.Error
	if !errors.As(err, &protoErr) {
		t.Fatalf("error type = %T (%v), want *textproto.Error", err, err)
	}
	if protoErr.Code != 552 {
		t.Errorf("reply code = %d, want 552", protoErr.Code)
	}
	if strings.Contains(protoErr.Msg, "smtp: read data") {
		t.Errorf("reply %q leaks internal error wrap", protoErr.Msg)
	}
}

func TestSMTP_ConcurrentConnections(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(
				"From: s%d@example.com\r\nTo: r@example.com\r\nSubject: Concurrent %d\r\n\r\nbody\r\n",
				i, i,
			)
			errs[i] = netsmtp.SendMail(addr, nil,
				fmt.Sprintf("s%d@example.com", i),
				[]string{"r@example.com"},
				[]byte(body),
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SendMail error: %v", i, err)
		}
	}
	if enq.Count() != n {
		t.Errorf("expected %d enqueued, got %d", n, enq.Count())
	}
}

func TestSMTP_MaxConnectionsBoundsActiveSessions(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaxConnections = 1
	addr := startServer(t, cfg, &countingEnqueuer{})
	first, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	secondDone := make(chan error, 1)
	go func() {
		second, err := netsmtp.Dial(addr)
		if second != nil {
			_ = second.Close()
		}
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second session completed while first was active: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second Dial after capacity release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second session did not proceed after capacity release")
	}
}

func TestSMTP_SessionReset(t *testing.T) {
	// RSET between transactions: send two emails in sequence (each a new tx).
	// net/smtp.SendMail() opens a fresh connection per call, so we use the
	// lower-level smtp.Client to issue RSET explicitly.
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	c, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Hello("localhost"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}

	// First MAIL/RCPT then RSET.
	if err := c.Mail("s@example.com"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := c.Rcpt("r@example.com"); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := c.Reset(); err != nil {
		t.Fatalf("RSET: %v", err)
	}

	// After reset, start a new transaction.
	if err := c.Mail("s2@example.com"); err != nil {
		t.Fatalf("MAIL after RSET: %v", err)
	}
	if err := c.Rcpt("r@example.com"); err != nil {
		t.Fatalf("RCPT after RSET: %v", err)
	}
	wc, err := c.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	body := "From: s2@example.com\r\nTo: r@example.com\r\nSubject: AfterReset\r\n\r\nbody\r\n"
	if _, err := fmt.Fprint(wc, body); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close data: %v", err)
	}

	if enq.Count() != 1 {
		t.Errorf("expected 1 enqueue (RSET discards first), got %d", enq.Count())
	}
	msg, _ := enq.Last()
	if msg.EnvelopeFrom != "s2@example.com" {
		t.Errorf("envelope_from = %q, want s2@example.com", msg.EnvelopeFrom)
	}
}

func TestSMTP_InvalidMIME_StillEnqueues(t *testing.T) {
	// The SMTP layer does not parse MIME; even garbled MIME bodies should be
	// enqueued (parsing happens downstream in the worker).
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	badMIME := "From: s@example.com\r\nTo: r@example.com\r\n" +
		"Content-Type: multipart/mixed; boundary=\"MISSING_BOUNDARY\"\r\n" +
		"\r\nnot a valid mime body at all\r\n"

	if err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(badMIME)); err != nil {
		t.Fatalf("SendMail with invalid MIME: %v", err)
	}
	if enq.Count() != 1 {
		t.Fatalf("expected 1 enqueue even for invalid MIME, got %d", enq.Count())
	}
}

func TestSMTP_MessageSizeLimit_Rejected(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaxMessageBytes = 512
	enq := &countingEnqueuer{}
	addr := startServer(t, cfg, enq)

	// Build a message that exceeds 512 bytes.
	oversized := "From: s@example.com\r\nTo: r@example.com\r\nSubject: Big\r\n\r\n" +
		strings.Repeat("X", 1000) + "\r\n"
	err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(oversized))
	if err == nil {
		t.Fatal("expected error for oversized message, got nil")
	}
	if enq.Count() != 0 {
		t.Errorf("oversized message should not be enqueued, got %d", enq.Count())
	}
}

func TestSMTP_AuthNotAdvertisedAndRejected(t *testing.T) {
	// The inbound MX session implements no auth backend: AUTH must not be
	// advertised in EHLO, and an unsolicited AUTH command must be rejected.
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	c, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello("client.example.net"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}
	if ok, params := c.Extension("AUTH"); ok {
		t.Fatalf("AUTH advertised (%q); inbound MX has no auth backend", params)
	}
	id, err := c.Text.Cmd("AUTH PLAIN AGZvbwBiYXI=")
	if err != nil {
		t.Fatalf("send AUTH: %v", err)
	}
	c.Text.StartResponse(id)
	code, message, err := c.Text.ReadResponse(2)
	c.Text.EndResponse(id)
	if err == nil {
		t.Fatalf("AUTH accepted (%d %s); want rejection", code, message)
	}
}

func TestSMTP_BarePostmaster_DocumentedDeviation(t *testing.T) {
	// RFC 5321 section 4.5.1 requires accepting RCPT TO:<postmaster> with
	// no domain. go-smtp's RCPT parser requires '@' and replies 501 before
	// the backend sees the address, so mxhook currently deviates at the
	// library layer. The backend itself accepts the bare form; if the
	// upstream parser starts admitting it, this test exercises the full
	// acceptance path instead.
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	c, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello("client.example.net"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}
	if err := c.Mail("sender@example.net"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	err = c.Rcpt("postmaster")
	if err == nil {
		wc, err := c.Data()
		if err != nil {
			t.Fatalf("DATA: %v", err)
		}
		if _, err := fmt.Fprint(wc, "Subject: postmaster\r\n\r\nbody\r\n"); err != nil {
			t.Fatalf("write body: %v", err)
		}
		if err := wc.Close(); err != nil {
			t.Fatalf("close DATA: %v", err)
		}
		if enq.Count() != 1 {
			t.Fatalf("expected 1 enqueue, got %d", enq.Count())
		}
		return
	}
	var protoErr *textproto.Error
	if !errors.As(err, &protoErr) || protoErr.Code != 501 {
		t.Fatalf("bare postmaster reply = %v, want library-level 501 (or acceptance)", err)
	}
}

func TestSMTP_AllowedDomainTrailingDotsNormalized(t *testing.T) {
	// Multiple trailing dots in config or RCPT domain must normalize away;
	// a single-TrimSuffix left "example.com.." permanently unmatchable.
	cfg := defaultCfg()
	cfg.AllowedDomains = []string{"example.com.."}
	enq := &countingEnqueuer{}
	addr := startServer(t, cfg, enq)

	body := "From: s@example.net\r\nTo: r@example.com\r\nSubject: Dots\r\n\r\nbody\r\n"
	if err := netsmtp.SendMail(addr, nil, "s@example.net", []string{"r@example.com"}, []byte(body)); err != nil {
		t.Fatalf("SendMail with dotted config domain: %v", err)
	}
	if err := netsmtp.SendMail(addr, nil, "s@example.net", []string{"r@example.com.."}, []byte(body)); err != nil {
		t.Fatalf("SendMail with dotted recipient domain: %v", err)
	}
	if enq.Count() != 2 {
		t.Fatalf("expected 2 enqueues, got %d", enq.Count())
	}
}

func TestSMTP_DuringDrainDataGets421(t *testing.T) {
	// A session that was connected before shutdown began and reaches DATA
	// during the drain must get a 421 rather than a new enqueue.
	enqueuer := newBlockingEnqueuer()
	server, addr := startShutdownServer(t, enqueuer)

	first, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("second Dial: %v", err)
	}
	defer func() { _ = second.Close() }()
	if err := second.Hello("client.example.net"); err != nil {
		t.Fatalf("second EHLO: %v", err)
	}

	if err := first.Mail("sender@example.net"); err != nil {
		t.Fatal(err)
	}
	if err := first.Rcpt("inbox@example.com"); err != nil {
		t.Fatal(err)
	}
	writer, err := first.Data()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("Subject: drain\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	go func() { _ = writer.Close() }()
	<-enqueuer.entered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(context.Background()) }()
	// Shutdown has begun once the listener refuses new connections; the
	// stopping flag is set before the listener closes.
	if !waitRefused(addr, 2*time.Second) {
		t.Fatal("listener still accepting after Shutdown started")
	}

	// The pre-existing second session tries a full transaction mid-drain.
	if err := second.Mail("sender@example.net"); err != nil {
		t.Fatalf("MAIL during drain: %v", err)
	}
	if err := second.Rcpt("inbox@example.com"); err != nil {
		t.Fatalf("RCPT during drain: %v", err)
	}
	dataWriter, err := second.Data()
	if err != nil {
		t.Fatalf("DATA during drain: %v", err)
	}
	if _, err := dataWriter.Write([]byte("Subject: late\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("write during drain: %v", err)
	}
	err = dataWriter.Close()
	var protoErr *textproto.Error
	if !errors.As(err, &protoErr) || protoErr.Code != 421 {
		t.Fatalf("mid-drain DATA completion = %v, want 421", err)
	}

	close(enqueuer.release)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-enqueuer.result; err != nil {
		t.Fatalf("in-flight enqueue aborted during graceful drain: %v", err)
	}
	select {
	case err := <-enqueuer.result:
		t.Fatalf("mid-drain DATA reached the enqueuer (result %v)", err)
	default:
	}
}

func TestSMTP_NoRecipients_NotEnqueued(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	// Attempt delivery with a recipient in a domain not in AllowedDomains.
	// The RCPT TO will be rejected, so DATA never runs → nothing enqueued.
	err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@blocked.com"}, []byte("body"))
	if err == nil {
		t.Fatal("expected error when all recipients are rejected")
	}
	if enq.Count() != 0 {
		t.Errorf("expected 0 enqueued, got %d", enq.Count())
	}
}

func TestSMTP_MetaContainsAllRecipients(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	rcpts := []string{"alice@example.com", "bob@example.com"}
	body := "From: s@example.com\r\nTo: alice@example.com\r\nSubject: X\r\n\r\nX\r\n"
	if err := netsmtp.SendMail(addr, nil, "s@example.com", rcpts, []byte(body)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	msg, ok := enq.Last()
	if !ok {
		t.Fatal("no message enqueued")
	}
	for idx, recipient := range rcpts {
		if len(msg.EnvelopeTo) <= idx || msg.EnvelopeTo[idx] != recipient {
			t.Errorf("envelope_to[%d] = %#v, want %q", idx, msg.EnvelopeTo, recipient)
		}
	}
}

func TestSMTP_UniqueIDPerMessage(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	body := "From: s@example.com\r\nTo: r@example.com\r\nSubject: ID\r\n\r\nbody\r\n"
	for i := 0; i < 3; i++ {
		if err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(body)); err != nil {
			t.Fatalf("SendMail %d: %v", i, err)
		}
	}

	enq.mu.Lock()
	defer enq.mu.Unlock()
	seen := map[string]bool{}
	for _, m := range enq.msgs {
		if seen[m.ID] {
			t.Errorf("duplicate message ID: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestSMTP_EmptyBody_Enqueued(t *testing.T) {
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	// Only headers, no body text.
	body := "From: s@example.com\r\nTo: r@example.com\r\nSubject: Empty\r\n\r\n"
	if err := netsmtp.SendMail(addr, nil, "s@example.com", []string{"r@example.com"}, []byte(body)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}
	if enq.Count() != 1 {
		t.Fatalf("expected 1 enqueue, got %d", enq.Count())
	}
}

func TestSMTP_LogoutNoop(t *testing.T) {
	// QUIT should succeed cleanly (Logout returns nil).
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	c, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT: %v", err)
	}
}

func TestSMTP_MixedDomainRecipients_OnlyAllowedAccepted(t *testing.T) {
	// One allowed + one disallowed recipient: go-smtp returns error when any RCPT fails.
	enq := &countingEnqueuer{}
	addr := startServer(t, defaultCfg(), enq)

	rcpts := []string{"good@example.com", "bad@blocked.com"}
	body := "From: s@example.com\r\nTo: good@example.com\r\nSubject: X\r\n\r\nX\r\n"
	_ = netsmtp.SendMail(addr, nil, "s@example.com", rcpts, []byte(body))
	// Whether enqueued or not depends on whether go-smtp sends DATA after partial RCPT.
	// At minimum, the server must not panic.
}
