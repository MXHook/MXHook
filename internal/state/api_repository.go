package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

func (s *Store) CreateDomain(ctx context.Context, name string) (api.Domain, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.Contains(name, "@") {
		return api.Domain{}, fmt.Errorf("%w: valid domain name is required", api.ErrInvalid)
	}
	id, err := queue.NewReceiptHandle()
	if err != nil {
		return api.Domain{}, fmt.Errorf("state: create domain ID: %w", err)
	}
	domain := api.Domain{ID: "domain:" + id, Name: name, CreatedAt: s.now().UTC()}
	_, err = s.db.ExecContext(ctx, s.bind(`INSERT INTO mxhook_domains (id, name, created_at) VALUES (?, ?, ?)`),
		domain.ID, domain.Name, formatTime(domain.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return api.Domain{}, fmt.Errorf("%w: domain %q already exists", api.ErrConflict, name)
		}
		return api.Domain{}, fmt.Errorf("state: create domain: %w", err)
	}
	return domain, nil
}

func (s *Store) ListDomains(ctx context.Context) ([]api.Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM mxhook_domains ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("state: list domains: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	domains := make([]api.Domain, 0)
	for rows.Next() {
		var domain api.Domain
		var created any
		if err := rows.Scan(&domain.ID, &domain.Name, &created); err != nil {
			return nil, fmt.Errorf("state: scan domain: %w", err)
		}
		if domain.CreatedAt, err = parseDatabaseTime(created); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list domains rows: %w", err)
	}
	return domains, nil
}

// ActiveDomains returns the registered domain names for SMTP recipient
// acceptance. It selects only the name column so the ingress path does not pay
// for row decoding it does not need.
func (s *Store) ActiveDomains(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM mxhook_domains`)
	if err != nil {
		return nil, fmt.Errorf("state: list active domains: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("state: scan active domain: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: active domains rows: %w", err)
	}
	return names, nil
}

func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.bind(`DELETE FROM mxhook_domains WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("state: delete domain: %w", err)
	}
	return apiAffected(result)
}

func (s *Store) CreateRoute(ctx context.Context, route api.Route) (api.Route, error) {
	if strings.TrimSpace(route.WebhookSecret) == "" {
		return api.Route{}, fmt.Errorf("%w: webhook signing secret is required", api.ErrInvalid)
	}
	if strings.TrimSpace(route.RecipientPattern) == "" {
		return api.Route{}, fmt.Errorf("%w: recipient pattern is required", api.ErrInvalid)
	}
	if strings.TrimSpace(route.WebhookURL) == "" {
		return api.Route{}, fmt.Errorf("%w: webhook URL is required", api.ErrInvalid)
	}
	id, err := queue.NewReceiptHandle()
	if err != nil {
		return api.Route{}, fmt.Errorf("state: create route ID: %w", err)
	}
	route.ID = "route:" + id
	route.Domain = strings.ToLower(strings.TrimSpace(route.Domain))
	route.RecipientPattern = strings.ToLower(strings.TrimSpace(route.RecipientPattern))
	route.CreatedAt = s.now().UTC()
	if err := s.EnsureRoute(ctx, router.Rule{
		ID: route.ID, Domain: route.Domain, RecipientPattern: route.RecipientPattern,
		Target: router.TargetWebhook, WebhookURL: route.WebhookURL, WebhookSecret: route.WebhookSecret,
	}); err != nil {
		return api.Route{}, err
	}
	return route, nil
}

func (s *Store) ListRoutes(ctx context.Context) ([]api.Route, error) {
	rows, err := s.db.QueryContext(ctx, s.bind(`SELECT id, domain, recipient_pattern, webhook_url, created_at
		FROM mxhook_routes WHERE enabled = ? ORDER BY priority DESC, created_at`), true)
	if err != nil {
		return nil, fmt.Errorf("state: list routes: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	routes := make([]api.Route, 0)
	for rows.Next() {
		var route api.Route
		var created any
		if err := rows.Scan(&route.ID, &route.Domain, &route.RecipientPattern, &route.WebhookURL, &created); err != nil {
			return nil, fmt.Errorf("state: scan route: %w", err)
		}
		if route.CreatedAt, err = parseDatabaseTime(created); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list routes rows: %w", err)
	}
	return routes, nil
}

func (s *Store) DeleteRoute(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.bind(`UPDATE mxhook_routes SET enabled = ?, updated_at = ? WHERE id = ? AND enabled = ?`),
		false, formatTime(s.now().UTC()), id, true)
	if err != nil {
		return fmt.Errorf("state: disable route: %w", err)
	}
	return apiAffected(result)
}

func (s *Store) ListMessages(ctx context.Context, page api.Page) ([]api.Message, error) {
	rows, err := s.db.QueryContext(ctx, s.bind(`SELECT e.payload, `+messageStateExpr+`
		FROM mxhook_events e ORDER BY e.received_at DESC LIMIT ? OFFSET ?`), page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("state: list messages: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	messages := make([]api.Message, 0)
	for rows.Next() {
		var payload []byte
		var state string
		if err := rows.Scan(&payload, &state); err != nil {
			return nil, fmt.Errorf("state: scan message: %w", err)
		}
		message, err := apiMessage(payload, state)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list messages rows: %w", err)
	}
	return messages, nil
}

// messageStateExpr derives a message's state from its delivery rows rather
// than reporting every message as processed: no deliveries means nothing was
// routed, any outstanding delivery keeps it pending, and a message whose
// deliveries all ended terminally reports dead when none succeeded.
const messageStateExpr = `CASE
		WHEN NOT EXISTS (SELECT 1 FROM mxhook_deliveries d WHERE d.message_id = e.id) THEN 'received'
		WHEN EXISTS (SELECT 1 FROM mxhook_deliveries d WHERE d.message_id = e.id
			AND d.state IN ('pending', 'processing', 'retry')) THEN 'pending'
		WHEN EXISTS (SELECT 1 FROM mxhook_deliveries d WHERE d.message_id = e.id
			AND d.state = 'succeeded') THEN 'processed'
		ELSE 'dead'
	END`

func (s *Store) GetMessage(ctx context.Context, id string) (api.Message, error) {
	var payload []byte
	var state string
	if err := s.db.QueryRowContext(ctx, s.bind(`SELECT e.payload, `+messageStateExpr+`
		FROM mxhook_events e WHERE e.id = ?`), id).Scan(&payload, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return api.Message{}, api.ErrNotFound
		}
		return api.Message{}, fmt.Errorf("state: get message: %w", err)
	}
	return apiMessage(payload, state)
}

func apiMessage(payload []byte, state string) (api.Message, error) {
	var event types.EmailEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return api.Message{}, fmt.Errorf("state: decode message: %w", err)
	}
	return api.Message{
		ID: event.ID, MessageID: event.MessageID, From: event.EnvelopeFrom,
		To: event.EnvelopeTo, Subject: event.Subject, ReceivedAt: event.ReceivedAt, State: state,
	}, nil
}

func (s *Store) ListDeadDeliveries(ctx context.Context, page api.Page) ([]api.DLQEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.bind(`SELECT id, message_id, route_id, webhook_url,
		attempt_count, replay_generation, reason, failed_at FROM (
			SELECT id, message_id, route_id, webhook_url, attempt_count, replay_generation,
				last_error AS reason, updated_at AS failed_at
			FROM mxhook_deliveries WHERE state = 'dead'
			UNION ALL
			SELECT 'ingress:' || message_id, message_id, '', '', 0, 0, reason, failed_at
			FROM mxhook_ingress_failures
		) AS terminal_failures ORDER BY failed_at DESC LIMIT ? OFFSET ?`), page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("state: list dead deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	entries := make([]api.DLQEntry, 0)
	for rows.Next() {
		var entry api.DLQEntry
		var failed any
		if err := rows.Scan(&entry.ID, &entry.MessageID, &entry.RouteID, &entry.WebhookURL,
			&entry.AttemptCount, &entry.ReplayGeneration, &entry.Reason, &failed); err != nil {
			return nil, fmt.Errorf("state: scan dead delivery: %w", err)
		}
		if entry.FailedAt, err = parseDatabaseTime(failed); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list dead deliveries rows: %w", err)
	}
	return entries, nil
}

func (s *Store) DeleteIngressFailure(ctx context.Context, messageID string) error {
	result, err := s.db.ExecContext(ctx, s.bind(`DELETE FROM mxhook_ingress_failures WHERE message_id = ?`), messageID)
	if err != nil {
		return fmt.Errorf("state: delete ingress failure: %w", err)
	}
	return apiAffected(result)
}

func (s *Store) ScheduleReplay(ctx context.Context, messageID string) (int64, error) {
	now := formatTime(s.now().UTC())
	result, err := s.db.ExecContext(ctx, s.bind(`UPDATE mxhook_deliveries SET state = 'pending',
		replay_generation = replay_generation + 1, attempt_count = 0, next_attempt_at = ?,
		lease_token = NULL, locked_until = NULL, last_error = '', updated_at = ?
		WHERE message_id = ? AND state = 'dead'`),
		now, now, messageID)
	if err != nil {
		return 0, fmt.Errorf("state: schedule replay: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("state: inspect replay result: %w", err)
	}
	if count == 0 {
		return 0, api.ErrNotFound
	}
	return count, nil
}

// isUniqueViolation reports whether err is a uniqueness-constraint failure
// from either supported driver.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "UNIQUE constraint")
}

func apiAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("state: inspect affected rows: %w", err)
	}
	if count == 0 {
		return api.ErrNotFound
	}
	return nil
}

var _ api.Repository = (*Store)(nil)
