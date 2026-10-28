package state

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	// Registers the "pgx" database/sql driver used for Postgres state.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
	"modernc.org/sqlite"
)

//go:embed schema_sqlite.sql
var sqliteSchema string

//go:embed schema_postgres.sql
var postgresSchema string

const schemaVersion = 1

// Config.Driver values.
const (
	driverSQLite   = "sqlite"
	driverPostgres = "postgres"
)

var ErrLeaseLost = errors.New("state: delivery lease lost")

type Config struct {
	Driver        string
	DSN           string
	EncryptionKey []byte
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RunMigrations bool
}

type Store struct {
	db            *sql.DB
	driver        string
	aead          cipher.AEAD
	leaseDuration time.Duration
	pollInterval  time.Duration
	now           func() time.Time

	queueDepth         QueueDepthSource
	queueDepthLogMu    sync.Mutex
	queueDepthLoggedAt time.Time
}

func Open(cfg Config) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = driverSQLite
	}
	if cfg.DSN == "" {
		return nil, errors.New("state: database URL is required")
	}
	aead, err := newCipher(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = 30 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.LeaseDuration <= 0 || cfg.PollInterval <= 0 {
		return nil, errors.New("state: lease duration and poll interval must be positive")
	}

	var sqlDriver, dsn string
	switch driver {
	case driverSQLite:
		absolute, err := filepath.Abs(cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("state: resolve SQLite path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			return nil, fmt.Errorf("state: create SQLite directory: %w", err)
		}
		sqlDriver = "sqlite"
		dsn = (&url.URL{Scheme: "file", Path: absolute}).String() +
			"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	case driverPostgres:
		sqlDriver = "pgx"
		dsn = cfg.DSN
	default:
		return nil, fmt.Errorf("state: unsupported database driver %q", cfg.Driver)
	}
	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("state: open database: %w", err)
	}
	if driver == driverSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, fmt.Errorf("state: ping database: %w", err)
	}
	if driver == driverSQLite {
		absolute, _ := filepath.Abs(cfg.DSN)
		if err := os.Chmod(absolute, 0o600); err != nil {
			_ = db.Close() // best-effort cleanup on error path; original error returned
			return nil, fmt.Errorf("state: secure SQLite database: %w", err)
		}
	}
	store := &Store{
		db: db, driver: driver, aead: aead,
		leaseDuration: cfg.LeaseDuration, pollInterval: cfg.PollInterval, now: time.Now,
	}
	if err := store.checkFutureSchema(context.Background()); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, err
	}
	if cfg.RunMigrations {
		if err := store.Migrate(context.Background()); err != nil {
			_ = db.Close() // best-effort cleanup on error path; original error returned
			return nil, err
		}
	} else if err := store.verifySchema(context.Background()); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("state: close database: %w", err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("state: ping database: %w", err)
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	schema := sqliteSchema
	if s.driver == driverPostgres {
		schema = postgresSchema
	}
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("state: apply schema: %w", err)
	}
	return s.verifySchema(ctx)
}

func (s *Store) checkFutureSchema(ctx context.Context) error {
	var version int
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM mxhook_state_migrations").Scan(&version)
	if err != nil {
		if isMissingTable(err) {
			return nil
		}
		return fmt.Errorf("state: inspect schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("state: database schema version %d is newer than supported version %d", version, schemaVersion)
	}
	return nil
}

// isMissingTable reports whether err is the backend's "relation does not
// exist" fault, matched on the driver's error code rather than message text,
// which is localized and version-dependent.
func isMissingTable(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		// SQLITE_ERROR: the generic statement-preparation fault, which is
		// what a missing table produces.
		return sqliteErr.Code() == 1
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// undefined_table
		return pgErr.Code == "42P01"
	}
	return false
}

func (s *Store) verifySchema(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM mxhook_state_migrations").Scan(&version); err != nil {
		if isMissingTable(err) {
			return fmt.Errorf("state: database schema version 0, need %d", schemaVersion)
		}
		return fmt.Errorf("state: read schema version: %w", err)
	}
	if version != schemaVersion {
		return fmt.Errorf("state: database schema version %d, need %d", version, schemaVersion)
	}
	return nil
}

func (s *Store) EnsureRoute(ctx context.Context, rule router.Rule) error {
	if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.RecipientPattern) == "" {
		return errors.New("state: route ID and recipient pattern are required")
	}
	if rule.Target == router.TargetWebhook && (rule.WebhookURL == "" || rule.WebhookSecret == "") {
		return errors.New("state: webhook route URL and secret are required")
	}
	encrypted, err := seal(s.aead, rule.WebhookSecret, rule.ID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	query := `INSERT INTO mxhook_routes
		(id, domain, recipient_pattern, target, webhook_url, webhook_secret, is_catch_all, priority, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		domain = excluded.domain, recipient_pattern = excluded.recipient_pattern,
		target = excluded.target, webhook_url = excluded.webhook_url,
		webhook_secret = excluded.webhook_secret, is_catch_all = excluded.is_catch_all,
		priority = excluded.priority, enabled = excluded.enabled, updated_at = excluded.updated_at`
	_, err = s.db.ExecContext(ctx, s.bind(query), rule.ID, rule.Domain, rule.RecipientPattern, string(rule.Target),
		rule.WebhookURL, encrypted, rule.IsCatchAll, rule.Priority, true, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("state: ensure route: %w", err)
	}
	return nil
}

func (s *Store) Load() ([]router.Rule, error) {
	// RuleLoader.Load carries no context; route reloads are short and bounded
	// by the database driver timeouts.
	rows, err := s.db.QueryContext(context.Background(), s.bind(`SELECT id, domain, recipient_pattern, target, webhook_url,
		webhook_secret, is_catch_all, priority FROM mxhook_routes
		WHERE enabled = ? ORDER BY priority DESC, created_at`), true)
	if err != nil {
		return nil, fmt.Errorf("state: load routes: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	var rules []router.Rule
	for rows.Next() {
		var rule router.Rule
		var encrypted []byte
		if err := rows.Scan(&rule.ID, &rule.Domain, &rule.RecipientPattern, &rule.Target,
			&rule.WebhookURL, &encrypted, &rule.IsCatchAll, &rule.Priority); err != nil {
			return nil, fmt.Errorf("state: scan route: %w", err)
		}
		rule.WebhookSecret, err = openSecret(s.aead, encrypted, rule.ID)
		if err != nil {
			return nil, fmt.Errorf("state: route %s: %w", rule.ID, err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: load routes: %w", err)
	}
	return rules, nil
}

func (s *Store) Commit(ctx context.Context, event *types.EmailEvent, rules []router.Rule) error {
	if event == nil || event.ID == "" {
		return errors.New("state: event ID is required")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("state: encode event: %w", err)
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; Commit error is the one reported
	if _, err := tx.ExecContext(ctx, s.bind(`INSERT INTO mxhook_events
		(id, trace_id, received_at, raw_path, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`), event.ID, event.TraceID, formatTime(event.ReceivedAt), event.RawPath, payload, formatTime(now)); err != nil {
		return fmt.Errorf("state: persist event: %w", err)
	}
	for _, rule := range rules {
		if rule.Target != router.TargetWebhook {
			continue
		}
		encrypted, err := seal(s.aead, rule.WebhookSecret, rule.ID)
		if err != nil {
			return err
		}
		deliveryID := deterministicDeliveryID(event.ID, rule.ID)
		if _, err := tx.ExecContext(ctx, s.bind(`INSERT INTO mxhook_deliveries
			(id, message_id, route_id, webhook_url, webhook_secret, next_attempt_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(message_id, route_id) DO NOTHING`),
			deliveryID, event.ID, rule.ID, rule.WebhookURL, encrypted, formatTime(now), formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("state: persist delivery: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("state: commit event transaction: %w", err)
	}
	return nil
}

func (s *Store) RecordIngressFailure(ctx context.Context, message queue.Message, reason string) error {
	now := formatTime(s.now().UTC())
	_, err := s.db.ExecContext(ctx, s.bind(`INSERT INTO mxhook_ingress_failures
		(message_id, trace_id, reason, failed_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET reason = excluded.reason, failed_at = excluded.failed_at`),
		message.ID, message.TraceID, boundedError(reason), now)
	if err != nil {
		return fmt.Errorf("state: record ingress failure: %w", err)
	}
	return nil
}

// ClearIngressFailure removes a recorded ingress failure once its message is
// successfully processed; an absent row is not an error.
func (s *Store) ClearIngressFailure(ctx context.Context, messageID string) error {
	if _, err := s.db.ExecContext(ctx, s.bind(`DELETE FROM mxhook_ingress_failures WHERE message_id = ?`), messageID); err != nil {
		return fmt.Errorf("state: clear ingress failure: %w", err)
	}
	return nil
}

func deterministicDeliveryID(messageID, routeID string) string {
	sum := sha256.Sum256([]byte(messageID + "\x00" + routeID))
	return "delivery:" + hex.EncodeToString(sum[:])
}

func (s *Store) ClaimDelivery(ctx context.Context) (Delivery, error) {
	for {
		delivery, err := s.claimOne(ctx)
		if err != nil {
			return Delivery{}, err
		}
		if delivery != nil {
			return *delivery, nil
		}
		timer := time.NewTimer(s.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Delivery{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) claimOne(ctx context.Context) (*Delivery, error) {
	now := s.now().UTC()
	lockedUntil := now.Add(s.leaseDuration)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("state: begin delivery claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; Commit error is the one reported
	query := `SELECT d.id, d.message_id, d.route_id, d.webhook_url, d.webhook_secret,
		d.attempt_count, d.replay_generation, e.payload
		FROM mxhook_deliveries d JOIN mxhook_events e ON e.id = d.message_id
		WHERE ((d.state IN ('pending', 'retry') AND d.next_attempt_at <= ?)
		   OR (d.state = 'processing' AND d.locked_until <= ?))
		ORDER BY d.next_attempt_at, d.created_at LIMIT 1`
	if s.driver == driverPostgres {
		// Lock only the delivery row: locking the joined event row would make
		// concurrent workers skip sibling deliveries of the same message.
		query += " FOR UPDATE OF d SKIP LOCKED"
	}
	var delivery Delivery
	var encrypted, payload []byte
	if err := tx.QueryRowContext(ctx, s.bind(query), formatTime(now), formatTime(now)).Scan(
		&delivery.ID, &delivery.MessageID, &delivery.RouteID, &delivery.WebhookURL,
		&encrypted, &delivery.Attempt, &delivery.Generation, &payload,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: select delivery: %w", err)
	}
	delivery.Secret, err = openSecret(s.aead, encrypted, delivery.RouteID)
	if err != nil {
		return nil, fmt.Errorf("state: delivery %s: %w", delivery.ID, err)
	}
	if err := json.Unmarshal(payload, &delivery.Event); err != nil {
		return nil, fmt.Errorf("state: decode event %s: %w", delivery.MessageID, err)
	}
	// Minted only once a claimable row exists, so idle polls cost no entropy.
	receipt, err := queue.NewReceiptHandle()
	if err != nil {
		return nil, fmt.Errorf("state: create delivery receipt: %w", err)
	}
	result, err := tx.ExecContext(ctx, s.bind(`UPDATE mxhook_deliveries SET
		state = 'processing', attempt_count = attempt_count + 1,
		lease_token = ?, locked_until = ?, updated_at = ?
		WHERE id = ? AND ((state IN ('pending', 'retry') AND next_attempt_at <= ?)
		OR (state = 'processing' AND locked_until <= ?))`),
		receipt, formatTime(lockedUntil), formatTime(now), delivery.ID, formatTime(now), formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("state: claim delivery: %w", err)
	}
	claimed, err := claimAffected(result)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	delivery.Attempt++
	delivery.Receipt = receipt
	delivery.LockedUntil = lockedUntil
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("state: commit delivery claim: %w", err)
	}
	return &delivery, nil
}

func (s *Store) ExtendDelivery(ctx context.Context, delivery Delivery, extension time.Duration) (time.Time, error) {
	if extension <= 0 {
		return time.Time{}, errors.New("state: delivery lease extension must be positive")
	}
	now := s.now().UTC()
	until := now.Add(extension)
	// Fenced on the lease token alone, like finishDelivery. A lapsed but
	// unclaimed lease still belongs to whoever holds the token, and re-arming
	// its deadline restores the invariant the claim intended. Demanding
	// locked_until > now instead rejects the rightful owner whenever the claim
	// itself ran long, which strands the row in 'processing' and redelivers it.
	result, err := s.db.ExecContext(ctx, s.bind(`UPDATE mxhook_deliveries
		SET locked_until = ?, updated_at = ?
		WHERE id = ? AND lease_token = ? AND state = 'processing'`),
		formatTime(until), formatTime(now), delivery.ID, delivery.Receipt)
	if err != nil {
		return time.Time{}, fmt.Errorf("state: extend delivery: %w", err)
	}
	if err := requireOne(result); err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func (s *Store) CompleteDelivery(ctx context.Context, delivery Delivery) error {
	return s.finishDelivery(ctx, delivery, "succeeded", "", time.Time{})
}

func (s *Store) RetryDelivery(ctx context.Context, delivery Delivery, reason string, next time.Time) error {
	return s.finishDelivery(ctx, delivery, "retry", reason, next)
}

func (s *Store) DeadDelivery(ctx context.Context, delivery Delivery, reason string) error {
	return s.finishDelivery(ctx, delivery, "dead", reason, time.Time{})
}

func (s *Store) GetDelivery(ctx context.Context, id string) (DeliveryRecord, error) {
	var record DeliveryRecord
	var next, created, updated any
	err := s.db.QueryRowContext(ctx, s.bind(`SELECT id, message_id, route_id, webhook_url,
		state, attempt_count, replay_generation, next_attempt_at, last_error, created_at, updated_at
		FROM mxhook_deliveries WHERE id = ?`), id).Scan(
		&record.ID, &record.MessageID, &record.RouteID, &record.WebhookURL,
		&record.State, &record.AttemptCount, &record.ReplayGeneration,
		&next, &record.LastError, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryRecord{}, fmt.Errorf("state: delivery %q not found", id)
	}
	if err != nil {
		return DeliveryRecord{}, fmt.Errorf("state: get delivery: %w", err)
	}
	if record.NextAttemptAt, err = parseDatabaseTime(next); err != nil {
		return DeliveryRecord{}, err
	}
	if record.CreatedAt, err = parseDatabaseTime(created); err != nil {
		return DeliveryRecord{}, err
	}
	if record.UpdatedAt, err = parseDatabaseTime(updated); err != nil {
		return DeliveryRecord{}, err
	}
	return record, nil
}

func (s *Store) finishDelivery(ctx context.Context, delivery Delivery, stateName, reason string, next time.Time) error {
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin delivery completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; Commit error is the one reported
	outcome := stateName
	if _, err := tx.ExecContext(ctx, s.bind(`INSERT INTO mxhook_delivery_attempts
		(delivery_id, replay_generation, attempt_no, outcome, error, finished_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(delivery_id, replay_generation, attempt_no) DO NOTHING`),
		delivery.ID, delivery.Generation, delivery.Attempt, outcome, boundedError(reason), formatTime(now)); err != nil {
		return fmt.Errorf("state: record delivery attempt: %w", err)
	}
	if next.IsZero() {
		next = now
	}
	// Fenced on the lease token alone, deliberately without locked_until > now.
	// The token is the ownership proof: a reclaim overwrites it, so a receipt
	// from a worker that lost the row still matches zero rows and is rejected.
	// Requiring the lease to also be live would reject a late-but-uncontested
	// completion, stranding the row in 'processing' with a dead lease -- which
	// matches the reclaim predicate and redelivers work that already finished.
	result, err := tx.ExecContext(ctx, s.bind(`UPDATE mxhook_deliveries SET
		state = ?, next_attempt_at = ?, lease_token = NULL, locked_until = NULL,
		last_error = ?, updated_at = ?
		WHERE id = ? AND lease_token = ? AND state = 'processing'`),
		stateName, formatTime(next), boundedError(reason), formatTime(now), delivery.ID, delivery.Receipt)
	if err != nil {
		return fmt.Errorf("state: finish delivery: %w", err)
	}
	if err := requireOne(result); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("state: commit delivery completion: %w", err)
	}
	return nil
}

// claimAffected reports whether the claim UPDATE took the row. A RowsAffected
// fault is a driver error, not an empty queue: swallowing it would leave the
// worker polling silently forever.
func claimAffected(result sql.Result) (bool, error) {
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("state: inspect claim result: %w", err)
	}
	return rows == 1, nil
}

func requireOne(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("state: inspect affected rows: %w", err)
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

func boundedError(reason string) string {
	const maxReasonBytes = 4096
	if len(reason) > maxReasonBytes {
		return reason[:maxReasonBytes]
	}
	return reason
}

func (s *Store) bind(query string) string {
	if s.driver != driverPostgres {
		return query
	}
	var builder strings.Builder
	argument := 1
	for _, r := range query {
		if r == '?' {
			fmt.Fprintf(&builder, "$%d", argument)
			argument++
		} else {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// timeLayout is fixed-width so stored TEXT timestamps compare correctly with
// SQL string operators; RFC3339Nano trims trailing zeros, which skews
// lexicographic comparisons on sub-second values (".1Z" sorts after ".15Z").
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(value time.Time) string { return value.UTC().Format(timeLayout) }

func parseDatabaseTime(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return time.Time{}, fmt.Errorf("state: parse database time: %w", err)
		}
		return parsed.UTC(), nil
	case []byte:
		return parseDatabaseTime(string(typed))
	default:
		return time.Time{}, fmt.Errorf("state: unsupported database time type %T", value)
	}
}
