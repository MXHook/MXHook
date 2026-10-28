package maildir

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/mxhook/mxhook/internal/metrics"
	"github.com/mxhook/mxhook/queue"
)

const testMessage = "Delivered-To: inbox@mxhook.exe.xyz\r\n" +
	"To: visible@example.com\r\n" +
	"Return-Path: <sender@example.net>\r\n" +
	"Subject: hello\r\n\r\nbody\r\n"

type recordingEnqueuer struct {
	mu       sync.Mutex
	messages []queue.Message
	err      error
}

func (r *recordingEnqueuer) Enqueue(_ context.Context, msg queue.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, msg)
	return r.err
}

func (r *recordingEnqueuer) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *recordingEnqueuer) snapshot() []queue.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]queue.Message(nil), r.messages...)
}

func newTestMaildir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"new", "cur", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	return root
}

// writeMessage delivers like a Maildir MTA: write the whole file under tmp/,
// then rename it into dir. Writing straight into new/ lets a running
// ingester claim the file before its contents land and read a partial
// message.
func writeMessage(t *testing.T, root, dir, name, body string) string {
	t.Helper()
	staged := filepath.Join(root, "tmp", "delivery-"+name)
	if err := os.WriteFile(staged, []byte(body), 0o600); err != nil {
		t.Fatalf("stage message: %v", err)
	}
	path := filepath.Join(root, dir, name)
	if err := os.Rename(staged, path); err != nil {
		t.Fatalf("deliver message: %v", err)
	}
	return path
}

func entries(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	got, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read dir %s: %v", path, err)
	}
	return got
}

func TestProcessOnceClaimsEnqueuesAndRemovesMessage(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "unique-name", testMessage)
	q := &recordingEnqueuer{}

	ingester, err := New(Config{
		Path:        root,
		IDNamespace: "test-mailbox",
	}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}

	got := q.snapshot()
	if len(got) != 1 {
		t.Fatalf("messages = %d, want 1", len(got))
	}
	if string(got[0].Body) != testMessage {
		t.Error("raw message bytes changed")
	}
	if got[0].EnvelopeFrom != "sender@example.net" {
		t.Errorf("envelope_from = %q", got[0].EnvelopeFrom)
	}
	if len(got[0].EnvelopeTo) != 1 || got[0].EnvelopeTo[0] != "inbox@mxhook.exe.xyz" {
		t.Errorf("envelope_to = %#v", got[0].EnvelopeTo)
	}
	if got[0].Source != "maildir" {
		t.Errorf("source = %q", got[0].Source)
	}
	if len(entries(t, filepath.Join(root, "new"))) != 0 {
		t.Error("successful message remains in new")
	}
	if len(entries(t, filepath.Join(root, "cur"))) != 0 {
		t.Error("successful claimed message remains in cur")
	}
}

func TestQueueFailureRetainsClaimAndRetryUsesStableID(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "opaque.123", testMessage)
	q := &recordingEnqueuer{err: errors.New("queue unavailable")}
	ingester, err := New(Config{Path: root, IDNamespace: "stable"}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if processed, err := ingester.ProcessOnce(context.Background()); err == nil || processed != 0 {
		t.Fatalf("first ProcessOnce = (%d, %v), want (0, error)", processed, err)
	}
	if len(entries(t, filepath.Join(root, "new"))) != 0 {
		t.Error("failed message was not claimed out of new")
	}
	if len(entries(t, filepath.Join(root, "cur"))) != 1 {
		t.Fatal("failed message not retained in cur")
	}

	q.setError(nil)
	processed, err := ingester.ProcessOnce(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("retry ProcessOnce = (%d, %v), want (1, nil)", processed, err)
	}
	got := q.snapshot()
	if len(got) != 2 {
		t.Fatalf("attempts = %d, want 2", len(got))
	}
	if got[0].ID != got[1].ID {
		t.Errorf("retry ID changed: %q != %q", got[0].ID, got[1].ID)
	}
	if len(entries(t, filepath.Join(root, "cur"))) != 0 {
		t.Error("successful retry remains in cur")
	}
}

func TestEnvelopeUsesFirstDeliveredToAndNullReturnPath(t *testing.T) {
	root := newTestMaildir(t)
	body := "Delivered-To: bcc@mxhook.exe.xyz\r\n" +
		"Delivered-To: older@example.net\r\n" +
		"To: somebody-else@example.com\r\n" +
		"Return-Path: <>\r\n\r\nbody"
	writeMessage(t, root, "new", "delivery", body)
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if _, err := ingester.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	msg := q.snapshot()[0]
	if len(msg.EnvelopeTo) != 1 || msg.EnvelopeTo[0] != "bcc@mxhook.exe.xyz" {
		t.Errorf("recipient = %#v", msg.EnvelopeTo)
	}
	if msg.EnvelopeFrom != "" {
		t.Errorf("null sender = %q", msg.EnvelopeFrom)
	}
}

func TestMissingRecipientMovesMessageToQuarantine(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "no-recipient", "Subject: no envelope\r\n\r\nbody")
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err == nil || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v), want (0, error)", processed, err)
	}
	if len(q.snapshot()) != 0 {
		t.Error("message without envelope recipient was enqueued")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Error("message without recipient not quarantined")
	}
}

func TestDefaultRecipientSupportsGenericMaildir(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "generic", "Subject: generic\r\n\r\nbody")
	q := &recordingEnqueuer{}
	ingester, err := New(Config{
		Path:             root,
		DefaultRecipient: "fallback@example.com",
	}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if _, err := ingester.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if got := q.snapshot()[0].EnvelopeTo[0]; got != "fallback@example.com" {
		t.Errorf("recipient = %q", got)
	}
}

func TestOversizedMessageIsQuarantined(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "large", testMessage)
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root, MaxMessageBytes: 10}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if processed, err := ingester.ProcessOnce(context.Background()); err == nil || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v), want (0, error)", processed, err)
	}
	if len(q.snapshot()) != 0 {
		t.Error("oversized message was enqueued")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Error("oversized message not quarantined")
	}
}

func TestNewRefusesMissingStructureAndUnownedCur(t *testing.T) {
	root := t.TempDir()
	if _, err := New(Config{Path: root}, &recordingEnqueuer{}); err == nil {
		t.Fatal("New accepted root without new/cur/tmp")
	}

	root = newTestMaildir(t)
	writeMessage(t, root, "cur", "existing:2,S", testMessage)
	if _, err := New(Config{Path: root}, &recordingEnqueuer{}); err == nil {
		t.Fatal("New adopted nonempty cur without opt-in")
	}
	adopted, err := New(Config{Path: root, AdoptExisting: true}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("New with AdoptExisting: %v", err)
	}
	if err := adopted.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewEnforcesExclusiveProcessLock(t *testing.T) {
	root := newTestMaildir(t)
	first, err := New(Config{Path: root}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := New(Config{Path: root}, &recordingEnqueuer{}); err == nil {
		t.Fatal("second New acquired an already-held Maildir lock")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := New(Config{Path: root}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("New after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestReadLimitedRejectsFileReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "claimed")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	expected, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat original: %v", err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatalf("rename original: %v", err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	if _, err := readLimited(path, expected, 1024); err == nil {
		t.Fatal("readLimited accepted a replaced claimed file")
	}
}

func TestSymlinkMessageIsQuarantinedWithoutReadingTarget(t *testing.T) {
	root := newTestMaildir(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte(testMessage), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "new", "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if _, err := ingester.ProcessOnce(context.Background()); err == nil {
		t.Fatal("ProcessOnce accepted a symlink")
	}
	if len(q.snapshot()) != 0 {
		t.Fatal("symlink target was enqueued")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Fatal("symlink was not quarantined")
	}
}

func TestRunPollsForNewMessages(t *testing.T) {
	root := newTestMaildir(t)
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root, PollInterval: 10 * time.Millisecond}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ingester.Run(ctx) }()

	writeMessage(t, root, "new", "arrived-later", testMessage)
	// Wait on the condition, not a fixed duration: the deadline only bounds
	// a genuine hang on a slow runner.
	deadline := time.Now().Add(10 * time.Second)
	for len(q.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(q.snapshot()); got != 1 {
		t.Fatalf("Run enqueued %d messages after delivery, want 1", got)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

// cancelingEnqueuer simulates shutdown arriving mid-scan: the first enqueue
// cancels the context and fails with a real (non-cancellation) error.
type cancelingEnqueuer struct {
	cancel context.CancelFunc
}

func (c *cancelingEnqueuer) Enqueue(context.Context, queue.Message) error {
	c.cancel()
	return errors.New("enqueue backend exploded")
}

func TestRunLogsRealErrorsAlongsideShutdownCancellation(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "aaa", testMessage)
	writeMessage(t, root, "new", "bbb", testMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ingester, err := New(Config{Path: root}, &cancelingEnqueuer{cancel: cancel})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	if err := ingester.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
	if !strings.Contains(logs.String(), "enqueue backend exploded") {
		t.Error("real scan error suppressed by shutdown cancellation gate")
	}
}

func TestRunReturnsWhenIngesterClosed(t *testing.T) {
	root := newTestMaildir(t)
	ingester, err := New(Config{Path: root, PollInterval: 10 * time.Millisecond}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- ingester.Run(context.Background()) }()
	if err := ingester.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errIngesterClosed) {
			t.Fatalf("Run = %v, want errIngesterClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run kept ticking after Close")
	}
}

func TestMessagesReceivedCountsOncePerRetainedClaim(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "counted-once", testMessage)
	q := &recordingEnqueuer{err: errors.New("transient queue outage")}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()
	before := testutil.ToFloat64(metrics.MessagesReceived)

	// Claim the message into cur/ while the queue is down.
	if _, err := ingester.ProcessOnce(context.Background()); err == nil {
		t.Fatal("ProcessOnce succeeded during queue outage")
	}
	q.setError(nil)

	// Queue recovers but the claimed file cannot be removed: the enqueue
	// succeeds and the claim is retained for retry.
	curDir := filepath.Join(root, "cur")
	if err := os.Chmod(curDir, 0o500); err != nil { //nolint:gosec // G302: directory mode; the file threshold does not apply to os.Chmod on a directory
		t.Fatalf("chmod cur: %v", err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			if err := os.Chmod(curDir, 0o700); err != nil { //nolint:gosec // G302: directory mode; the file threshold does not apply to os.Chmod on a directory
				t.Fatalf("restore cur perms: %v", err)
			}
		}
	}
	defer restore()
	if _, err := ingester.ProcessOnce(context.Background()); err == nil {
		t.Fatal("ProcessOnce succeeded with undeletable claim")
	}

	// The retry dedupes in the queue and finally removes the claim.
	restore()
	if processed, err := ingester.ProcessOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("retry ProcessOnce = (%d, %v), want (1, nil)", processed, err)
	}

	if got := testutil.ToFloat64(metrics.MessagesReceived) - before; got != 1 {
		t.Errorf("MessagesReceived delta = %v, want 1 (retained-claim retry over-counted)", got)
	}
}

func TestRenameNoReplacePreservesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("source bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(source, target); err == nil {
		t.Fatal("renameNoReplace overwrote an existing target")
	}
	if got, _ := os.ReadFile(source); string(got) != "source bytes" { //nolint:gosec // G304: test reads a path under t.TempDir()
		t.Fatalf("source changed: %q", got)
	}
	if got, _ := os.ReadFile(target); string(got) != "target bytes" { //nolint:gosec // G304: test reads a path under t.TempDir()
		t.Fatalf("target changed: %q", got)
	}
}

func TestNewRejectsSymlinkedOwnershipMarkerAndQuarantine(t *testing.T) {
	t.Run("ownership marker", func(t *testing.T) {
		root := newTestMaildir(t)
		outside := filepath.Join(t.TempDir(), "marker")
		if err := os.WriteFile(outside, []byte(ownerMarkerContent), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ownerMarkerName)); err != nil {
			t.Fatal(err)
		}
		if _, err := New(Config{Path: root}, &recordingEnqueuer{}); err == nil {
			t.Fatal("New followed a symlinked ownership marker")
		}
	})

	t.Run("quarantine directory", func(t *testing.T) {
		root := newTestMaildir(t)
		if err := os.WriteFile(filepath.Join(root, ownerMarkerName), []byte(ownerMarkerContent), 0o600); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "mxhook-failed")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(Config{Path: root}, &recordingEnqueuer{}); err == nil {
			t.Fatal("New accepted a symlinked quarantine directory")
		}
	})
}

func TestMalformedEnvelopeAddressIsQuarantined(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "malformed", "Delivered-To: one@example.com, two@example.com\r\n\r\nbody")
	queue := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, queue)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()
	if _, err := ingester.ProcessOnce(context.Background()); err == nil {
		t.Fatal("ProcessOnce accepted multiple Delivered-To mailboxes")
	}
	if len(queue.snapshot()) != 0 || len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Fatal("malformed envelope was not quarantined")
	}
}

func TestEnqueueIDCollisionQuarantinesInsteadOfRetrying(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "collide", testMessage)
	q := &recordingEnqueuer{err: fmt.Errorf("sqlite queue: %w", queue.ErrIDCollision)}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err == nil || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v), want (0, error)", processed, err)
	}
	if got := len(entries(t, filepath.Join(root, "cur"))); got != 0 {
		t.Errorf("ID-collision message retained in cur for retry (%d entries)", got)
	}
	if got := len(entries(t, filepath.Join(root, "mxhook-failed"))); got != 1 {
		t.Errorf("quarantined = %d, want 1", got)
	}
}

func TestQuarantineNameCollisionQuarantinesBothMessages(t *testing.T) {
	root := newTestMaildir(t)
	ingester, err := New(Config{Path: root}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	for attempt := range 2 {
		writeMessage(t, root, "new", "dup", "Subject: no envelope\r\n\r\nbody")
		if _, err := ingester.ProcessOnce(context.Background()); err == nil {
			t.Fatalf("attempt %d: ProcessOnce accepted message without recipient", attempt)
		}
	}
	if got := len(entries(t, filepath.Join(root, "mxhook-failed"))); got != 2 {
		t.Errorf("quarantined = %d, want 2", got)
	}
	if got := len(entries(t, filepath.Join(root, "cur"))); got != 0 {
		t.Errorf("collision left %d message(s) wedged in cur", got)
	}
}

func TestNewRejectsInvalidDefaultRecipient(t *testing.T) {
	root := newTestMaildir(t)
	if _, err := New(Config{Path: root, DefaultRecipient: "not an address"}, &recordingEnqueuer{}); err == nil {
		t.Fatal("New accepted invalid default recipient")
	}
}

func TestZeroByteMessageIsQuarantined(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "empty", "")
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err == nil || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v), want (0, error)", processed, err)
	}
	if len(q.snapshot()) != 0 {
		t.Error("zero-byte message was enqueued")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Error("zero-byte message not quarantined")
	}
}

func TestDirectoryInNewIsQuarantined(t *testing.T) {
	root := newTestMaildir(t)
	if err := os.Mkdir(filepath.Join(root, "new", "stray-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	if _, err := ingester.ProcessOnce(context.Background()); err == nil {
		t.Fatal("ProcessOnce accepted a directory in new/")
	}
	if len(q.snapshot()) != 0 {
		t.Error("directory was enqueued")
	}
	if len(entries(t, filepath.Join(root, "new"))) != 0 {
		t.Error("directory left in new/")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 1 {
		t.Error("directory not quarantined")
	}
}

func TestAdoptExistingProcessesPreexistingCur(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "cur", "legacy:2,S", testMessage)
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root, AdoptExisting: true}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("ProcessOnce = (%d, %v), want (1, nil)", processed, err)
	}
	got := q.snapshot()
	if len(got) != 1 {
		t.Fatalf("messages = %d, want 1", len(got))
	}
	if got[0].SourceID != "legacy" {
		t.Errorf("source_id = %q, want maildir flags stripped", got[0].SourceID)
	}
	if len(entries(t, filepath.Join(root, "cur"))) != 0 {
		t.Error("adopted message remains in cur")
	}
}

func TestDotfilesAreIgnored(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", ".hidden-new", testMessage)
	writeMessage(t, root, "cur", ".hidden-cur", testMessage)
	q := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root, AdoptExisting: true}, q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()

	processed, err := ingester.ProcessOnce(context.Background())
	if err != nil || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v), want (0, nil)", processed, err)
	}
	if len(q.snapshot()) != 0 {
		t.Error("dotfile was enqueued")
	}
	if len(entries(t, filepath.Join(root, "new"))) != 1 {
		t.Error("dotfile removed from new/")
	}
	if len(entries(t, filepath.Join(root, "cur"))) != 1 {
		t.Error("dotfile removed from cur/")
	}
	if len(entries(t, filepath.Join(root, "mxhook-failed"))) != 0 {
		t.Error("dotfile quarantined")
	}
}

func TestCanceledProcessOnceLeavesNewUntouched(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "untouched", testMessage)
	ingester, err := New(Config{Path: root}, &recordingEnqueuer{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ingester.ProcessOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProcessOnce error = %v", err)
	}
	if len(entries(t, filepath.Join(root, "new"))) != 1 || len(entries(t, filepath.Join(root, "cur"))) != 0 {
		t.Fatal("canceled ProcessOnce mutated Maildir")
	}
}

func TestConcurrentProcessOnceEnqueuesOnce(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "single", testMessage)
	queue := &recordingEnqueuer{}
	ingester, err := New(Config{Path: root}, queue)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = ingester.Close() }()
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			_, _ = ingester.ProcessOnce(context.Background())
		}()
	}
	wait.Wait()
	if got := len(queue.snapshot()); got != 1 {
		t.Fatalf("concurrent scans enqueued %d messages, want 1", got)
	}
}
