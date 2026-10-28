package smtp_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
	mxsmtp "github.com/mxhook/mxhook/smtp"
)

type blockingEnqueuer struct {
	entered chan struct{}
	release chan struct{}
	result  chan error
	once    sync.Once
}

func newBlockingEnqueuer() *blockingEnqueuer {
	return &blockingEnqueuer{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		result:  make(chan error, 1),
	}
}

func (b *blockingEnqueuer) Enqueue(ctx context.Context, _ queue.Message) error {
	b.once.Do(func() { close(b.entered) })
	var err error
	select {
	case <-b.release:
	case <-ctx.Done():
		err = ctx.Err()
	}
	b.result <- err
	return err
}

func TestSMTPShutdownWaitsForActiveDurableEnqueue(t *testing.T) {
	enqueuer := newBlockingEnqueuer()
	server, addr := startShutdownServer(t, enqueuer)
	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail("sender@example.net"); err != nil {
		t.Fatal(err)
	}
	if err := client.Rcpt("inbox@example.com"); err != nil {
		t.Fatal(err)
	}
	writer, err := client.Data()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("Subject: shutdown\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	dataDone := make(chan error, 1)
	go func() { dataDone <- writer.Close() }()
	<-enqueuer.entered
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before enqueue completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(enqueuer.release)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// The enqueue result is recorded before dataWG releases, so it is
	// guaranteed available once Shutdown returned.
	if err := <-enqueuer.result; err != nil {
		t.Fatalf("enqueue aborted during graceful shutdown: %v", err)
	}
	// Drain the writer goroutine without asserting: the final 250 races the
	// post-drain connection close, so its error is not meaningful here.
	<-dataDone
}

func TestSMTPShutdownDeadlineCancelsActiveEnqueue(t *testing.T) {
	enqueuer := newBlockingEnqueuer()
	server, addr := startShutdownServer(t, enqueuer)
	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail("sender@example.net"); err != nil {
		t.Fatal(err)
	}
	if err := client.Rcpt("inbox@example.com"); err != nil {
		t.Fatal(err)
	}
	writer, err := client.Data()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("Subject: deadline\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	go func() { _ = writer.Close() }()
	<-enqueuer.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
}

// stubbornEnqueuer blocks until released regardless of context
// cancellation, keeping the drain WG held across shutdown attempts.
type stubbornEnqueuer struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *stubbornEnqueuer) Enqueue(context.Context, queue.Message) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func TestSMTPShutdownDeadlineErrorNotSwallowedByClosedListener(t *testing.T) {
	enqueuer := &stubbornEnqueuer{entered: make(chan struct{}), release: make(chan struct{})}
	server, addr := startShutdownServer(t, enqueuer)
	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail("sender@example.net"); err != nil {
		t.Fatal(err)
	}
	if err := client.Rcpt("inbox@example.com"); err != nil {
		t.Fatal(err)
	}
	writer, err := client.Data()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("Subject: swallow\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	go func() { _ = writer.Close() }()
	<-enqueuer.entered
	// First closer takes the listener down and parks on the held drain WG.
	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close() }()
	if !waitRefused(addr, 2*time.Second) {
		t.Fatal("listener still accepting after Close")
	}
	// Shutdown with an already-cancelled context: the listener close now
	// fails with net.ErrClosed, which must not swallow the context error
	// out of the joined result.
	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(expiredCtx) }()
	// Let Shutdown reach its select while the drain WG is still held so the
	// context branch is the only ready case.
	time.Sleep(200 * time.Millisecond)
	close(enqueuer.release)
	if err := <-shutdownDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown error = %v, want context.Canceled (closed listener must not swallow it)", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func waitRefused(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestSMTPShutdownForceClosesBlockedDataRead(t *testing.T) {
	config := defaultCfg()
	config.ReadTimeout = 10 * time.Second
	addr := reserveAddress(t)
	config.Addr = addr
	server, err := mxsmtp.NewServer(config, &countingEnqueuer{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.ListenAndServe() }()
	if !waitPort(addr, 2*time.Second) {
		t.Fatalf("server did not start on %s", addr)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	expectSMTPReply(t, reader, "220")
	for _, command := range []struct{ line, code string }{
		{"EHLO client.example.net", "250"},
		{"MAIL FROM:<sender@example.net>", "250"},
		{"RCPT TO:<inbox@example.com>", "250"},
		{"DATA", "354"},
	} {
		if _, err := fmt.Fprintf(conn, "%s\r\n", command.line); err != nil {
			t.Fatalf("write %q: %v", command.line, err)
		}
		expectSMTPReply(t, reader, command.code)
	}
	// Partial body without the terminating dot: the session blocks in its
	// body read until the connection dies or the read deadline expires.
	if _, err := fmt.Fprint(conn, "Subject: stalled\r\n"); err != nil {
		t.Fatalf("write partial body: %v", err)
	}
	// Let the session enter the Data handler before shutting down.
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = server.Shutdown(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Shutdown took %v; blocked DATA read stalled drain past deadline", elapsed)
	}
}

func expectSMTPReply(t *testing.T, reader *bufio.Reader, code string) {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if !strings.HasPrefix(line, code) {
			t.Fatalf("reply = %q, want code %s", line, code)
		}
		if len(line) > 3 && line[3] == ' ' {
			return
		}
	}
}

func startShutdownServer(t *testing.T, enqueuer queue.Enqueuer) (*mxsmtp.Server, string) {
	t.Helper()
	config := defaultCfg()
	addr := reserveAddress(t)
	config.Addr = addr
	server, err := mxsmtp.NewServer(config, enqueuer)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.ListenAndServe() }()
	if !waitPort(addr, 2*time.Second) {
		t.Fatalf("server did not start on %s", addr)
	}
	return server, addr
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}
	return address
}
