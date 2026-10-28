package state

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

func TestSQLiteOutboxIsIdempotentEncryptedAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	key := bytes.Repeat([]byte{7}, 32)
	store := openTestStore(t, path, key, time.Second)
	rule := testRule()
	if err := store.EnsureRoute(context.Background(), rule); err != nil {
		t.Fatalf("EnsureRoute: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].WebhookSecret != rule.WebhookSecret {
		t.Fatalf("loaded routes = %#v", loaded)
	}
	var ciphertext []byte
	if err := store.db.QueryRow("SELECT webhook_secret FROM mxhook_routes WHERE id = ?", rule.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("read encrypted secret: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(rule.WebhookSecret)) {
		t.Fatal("webhook secret stored as plaintext")
	}

	event := testEvent()
	if err := store.Commit(context.Background(), event, loaded); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := store.Commit(context.Background(), event, loaded); err != nil {
		t.Fatalf("idempotent Commit: %v", err)
	}
	assertCount(t, store, "mxhook_events", 1)
	assertCount(t, store, "mxhook_deliveries", 1)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store = openTestStore(t, path, key, time.Second)
	t.Cleanup(func() { _ = store.Close() })
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery after reopen: %v", err)
	}
	if delivery.Event.ID != event.ID || delivery.Secret != rule.WebhookSecret || delivery.Attempt != 1 {
		t.Fatalf("delivery = %#v", delivery)
	}
	if err := store.CompleteDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("CompleteDelivery: %v", err)
	}
	assertCount(t, store, "mxhook_delivery_attempts", 1)
}

func TestSQLiteDeliveryLeaseExpiresAndFencesStaleWorker(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{9}, 32), 20*time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	clock := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	if err := store.Commit(context.Background(), testEvent(), []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	first, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("first ClaimDelivery: %v", err)
	}
	clock = clock.Add(30 * time.Millisecond)
	second, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("second ClaimDelivery: %v", err)
	}
	if first.Receipt == second.Receipt || second.Attempt != 2 {
		t.Fatalf("redelivery receipts/attempts: first=%#v second=%#v", first, second)
	}
	if err := store.CompleteDelivery(context.Background(), first); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale completion error = %v, want ErrLeaseLost", err)
	}
	if err := store.DeadDelivery(context.Background(), second, "permanent"); err != nil {
		t.Fatalf("DeadDelivery: %v", err)
	}
}

func TestSQLiteRetryScheduleSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	key := bytes.Repeat([]byte{3}, 32)
	store := openTestStore(t, path, key, time.Second)
	if err := store.Commit(context.Background(), testEvent(), []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	due := time.Now().Add(40 * time.Millisecond)
	if err := store.RetryDelivery(context.Background(), delivery, "temporary", due); err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	store = openTestStore(t, path, key, time.Second)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	retried, err := store.ClaimDelivery(ctx)
	if err != nil {
		t.Fatalf("ClaimDelivery after restart: %v", err)
	}
	if claimedAt := time.Now(); claimedAt.Before(due) {
		t.Fatalf("retry claimed %s before schedule", due.Sub(claimedAt))
	}
	if retried.Attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", retried.Attempt)
	}
}

func TestSQLiteReplayResetsAttemptBudgetAndPreservesAudit(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{4}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	event := testEvent()
	if err := store.Commit(context.Background(), event, []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	if err := store.DeadDelivery(context.Background(), delivery, "permanent"); err != nil {
		t.Fatalf("DeadDelivery: %v", err)
	}
	if _, err := store.ScheduleReplay(context.Background(), event.ID); err != nil {
		t.Fatalf("ScheduleReplay: %v", err)
	}
	replayed, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery after replay: %v", err)
	}
	if replayed.Attempt != 1 {
		t.Fatalf("replayed attempt = %d, want 1: replay must reset the attempt budget", replayed.Attempt)
	}
	if replayed.Generation != 1 {
		t.Fatalf("replayed generation = %d, want 1", replayed.Generation)
	}
	if err := store.DeadDelivery(context.Background(), replayed, "permanent again"); err != nil {
		t.Fatalf("DeadDelivery after replay: %v", err)
	}
	// Attempt 1 of generation 0 and attempt 1 of generation 1 must both be audited.
	assertCount(t, store, "mxhook_delivery_attempts", 2)
}

func TestSQLiteIngressFailureRemediation(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{6}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	message := queue.NewMessage("ingress-1", "trace-1", "", "sender@example.net",
		[]string{"inbox@example.com"}, time.Unix(1, 0), []byte("Subject: x\r\n\r\nbody"))
	if err := store.RecordIngressFailure(context.Background(), message, "parse failed"); err != nil {
		t.Fatalf("RecordIngressFailure: %v", err)
	}
	if err := store.DeleteIngressFailure(context.Background(), message.ID); err != nil {
		t.Fatalf("DeleteIngressFailure: %v", err)
	}
	assertCount(t, store, "mxhook_ingress_failures", 0)
	if err := store.DeleteIngressFailure(context.Background(), message.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("second DeleteIngressFailure error = %v, want api.ErrNotFound", err)
	}
	if err := store.RecordIngressFailure(context.Background(), message, "parse failed"); err != nil {
		t.Fatalf("RecordIngressFailure again: %v", err)
	}
	if err := store.ClearIngressFailure(context.Background(), message.ID); err != nil {
		t.Fatalf("ClearIngressFailure: %v", err)
	}
	assertCount(t, store, "mxhook_ingress_failures", 0)
	if err := store.ClearIngressFailure(context.Background(), message.ID); err != nil {
		t.Fatalf("ClearIngressFailure on absent row: %v", err)
	}
}

// failingRowsAffected mimics a driver whose RowsAffected reports a fault,
// which must not be read as "no work available".
type failingRowsAffected struct{ sql.Result }

func (failingRowsAffected) RowsAffected() (int64, error) {
	return 0, errors.New("driver: rows affected unavailable")
}

func TestRequireOneSurfacesRowsAffectedFailure(t *testing.T) {
	err := requireOne(failingRowsAffected{})
	if err == nil {
		t.Fatal("requireOne swallowed a RowsAffected failure")
	}
	if errors.Is(err, ErrLeaseLost) {
		t.Fatal("driver fault reported as a lost lease")
	}
}

func TestClaimResultInspectionDistinguishesFaultFromEmptyQueue(t *testing.T) {
	// A driver fault must surface as an error; only a genuine zero-row update
	// means "another worker won the row".
	fault, err := claimAffected(failingRowsAffected{})
	if err == nil {
		t.Fatal("claimAffected swallowed a RowsAffected failure: worker would idle silently")
	}
	if fault {
		t.Fatal("faulted result reported as a successful claim")
	}
	claimed, err := claimAffected(rowsAffectedResult(1))
	if err != nil || !claimed {
		t.Fatalf("claimAffected(1) = %v, %v; want true, nil", claimed, err)
	}
	lost, err := claimAffected(rowsAffectedResult(0))
	if err != nil || lost {
		t.Fatalf("claimAffected(0) = %v, %v; want false, nil", lost, err)
	}
}

type rowsAffectedResult int64

func (r rowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedResult) RowsAffected() (int64, error) { return int64(r), nil }

func TestMissingSchemaIsDetectedByErrorCode(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var version int
	queryErr := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM mxhook_state_migrations").Scan(&version)
	if queryErr == nil {
		t.Fatal("querying a nonexistent table must fail")
	}
	// Matched on the driver's error code, not on message text.
	if !isMissingTable(queryErr) {
		t.Fatalf("isMissingTable(%v) = false, want true", queryErr)
	}
	if isMissingTable(errors.New("no such table: some other failure")) {
		t.Fatal("isMissingTable matched plain error text rather than a driver code")
	}

	// End to end: opening an unmigrated database reports a version mismatch.
	if _, err := Open(Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"), EncryptionKey: bytes.Repeat([]byte{2}, 32),
		LeaseDuration: time.Second, PollInterval: 5 * time.Millisecond, RunMigrations: false,
	}); err == nil {
		t.Fatal("Open on an unmigrated database must fail")
	} else if !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("error = %v, want a schema version mismatch", err)
	}
}

func TestMessageStateReflectsDeliveryOutcome(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{11}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	event := testEvent()
	if err := store.Commit(context.Background(), event, []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	message, err := store.GetMessage(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if message.State != "pending" {
		t.Fatalf("state = %q, want pending while delivery is outstanding", message.State)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	if err := store.DeadDelivery(context.Background(), delivery, "permanent"); err != nil {
		t.Fatalf("DeadDelivery: %v", err)
	}
	if message, err = store.GetMessage(context.Background(), event.ID); err != nil {
		t.Fatalf("GetMessage after dead: %v", err)
	}
	if message.State != "dead" {
		t.Fatalf("state = %q, want dead once every delivery is dead", message.State)
	}
}

func TestListDeadDeliveriesUnionsIngressFailuresAndPages(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{12}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	event := testEvent()
	if err := store.Commit(context.Background(), event, []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	if err := store.DeadDelivery(context.Background(), delivery, "endpoint rejected"); err != nil {
		t.Fatalf("DeadDelivery: %v", err)
	}
	message := queue.NewMessage("ingress-dead", "trace", "", "sender@example.net",
		[]string{"inbox@example.com"}, time.Unix(2, 0), []byte("Subject: x\r\n\r\nbody"))
	if err := store.RecordIngressFailure(context.Background(), message, "unparsable"); err != nil {
		t.Fatalf("RecordIngressFailure: %v", err)
	}
	entries, err := store.ListDeadDeliveries(context.Background(), api.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListDeadDeliveries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want both dead delivery and ingress failure", len(entries))
	}
	paged, err := store.ListDeadDeliveries(context.Background(), api.Page{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("paged ListDeadDeliveries: %v", err)
	}
	if len(paged) != 1 || paged[0].ID == entries[0].ID {
		t.Fatalf("paging did not advance: first=%#v paged=%#v", entries[0], paged)
	}
}

func TestListMessagesPagingAndRouteDomainDeletion(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{13}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	first := testEvent()
	second := *first
	second.ID = "message-2"
	second.ReceivedAt = first.ReceivedAt.Add(time.Second)
	for _, event := range []*types.EmailEvent{first, &second} {
		if err := store.Commit(context.Background(), event, nil); err != nil {
			t.Fatalf("Commit %s: %v", event.ID, err)
		}
	}
	messages, err := store.ListMessages(context.Background(), api.Page{Limit: 1})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != second.ID {
		t.Fatalf("first page = %#v, want newest message only", messages)
	}
	offset, err := store.ListMessages(context.Background(), api.Page{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListMessages offset: %v", err)
	}
	if len(offset) != 1 || offset[0].ID != first.ID {
		t.Fatalf("second page = %#v, want the older message", offset)
	}

	domain, err := store.CreateDomain(context.Background(), "example.org")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if err := store.DeleteDomain(context.Background(), domain.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if err := store.DeleteDomain(context.Background(), domain.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("repeat DeleteDomain = %v, want api.ErrNotFound", err)
	}
	route, err := store.CreateRoute(context.Background(), api.Route{
		Domain: "example.org", RecipientPattern: "*@example.org",
		WebhookURL: "https://example.org/hook", WebhookSecret: "s",
	})
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	if err := store.DeleteRoute(context.Background(), route.ID); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if err := store.DeleteRoute(context.Background(), route.ID); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("repeat DeleteRoute = %v, want api.ErrNotFound", err)
	}
}

func TestFormatTimeLexicographicOrderMatchesChronology(t *testing.T) {
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	earlier := base.Add(100 * time.Millisecond)
	later := base.Add(150 * time.Millisecond)
	a, b := formatTime(earlier), formatTime(later)
	if a >= b {
		t.Fatalf("formatTime order skewed: %q >= %q", a, b)
	}
	for _, value := range []time.Time{base, earlier, later} {
		parsed, err := parseDatabaseTime(formatTime(value))
		if err != nil {
			t.Fatalf("parseDatabaseTime(%q): %v", formatTime(value), err)
		}
		if !parsed.Equal(value) {
			t.Fatalf("round-trip = %v, want %v", parsed, value)
		}
	}
}

func TestSQLiteClaimDueDeliveryAtSubSecondBoundary(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{5}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	committed := base.Add(100 * time.Millisecond)
	store.now = func() time.Time { return committed }
	if err := store.Commit(context.Background(), testEvent(), []router.Rule{testRule()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	store.now = func() time.Time { return base.Add(120 * time.Millisecond) }
	delivery, err := store.claimOne(context.Background())
	if err != nil {
		t.Fatalf("claimOne: %v", err)
	}
	if delivery == nil {
		t.Fatal("delivery due 20ms ago not claimed: sub-second timestamp comparison skew")
	}
}

func openTestStore(t *testing.T, path string, key []byte, lease time.Duration) *Store {
	t.Helper()
	store, err := Open(Config{
		Driver: "sqlite", DSN: path, EncryptionKey: key,
		LeaseDuration: lease, PollInterval: 5 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func testRule() router.Rule {
	return router.Rule{
		ID: "route-1", Domain: "example.com", RecipientPattern: "*@example.com",
		Target: router.TargetWebhook, WebhookURL: "https://example.com/hook", WebhookSecret: "super-secret",
	}
}

func testEvent() *types.EmailEvent {
	return &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1,
		ID:            "message-1", TraceID: "trace-1", ReceivedAt: time.Now().UTC(),
		EnvelopeFrom: "sender@example.net", EnvelopeTo: []string{"receiver@example.com"},
		Headers: map[string][]string{}, Subject: "hello", RawPath: "/raw/message-1.eml",
	}
}

func assertCount(t *testing.T, store *Store, table string, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
