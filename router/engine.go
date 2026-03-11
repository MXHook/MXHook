package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RuleLoader loads routing rules.
type RuleLoader interface {
	Load() ([]Rule, error)
}

// Engine provides Postgres-backed rules with hot-reload.
type Engine struct {
	pool   *pgxpool.Pool // nil for in-memory
	mu     sync.RWMutex
	rules  []Rule
	ticker *time.Ticker
	stop   chan struct{}
}

// NewEngineInMemory creates an Engine backed by a static rule set. Useful for tests.
func NewEngineInMemory(rules []Rule) *Engine {
	return &Engine{rules: rules}
}

// NewEngine creates a Postgres-backed Engine that reloads rules every reloadInterval.
func NewEngine(ctx context.Context, pool *pgxpool.Pool, reloadInterval time.Duration) (*Engine, error) {
	e := &Engine{
		pool:   pool,
		stop:   make(chan struct{}),
		ticker: time.NewTicker(reloadInterval),
	}
	if err := e.reload(ctx); err != nil {
		return nil, fmt.Errorf("rule engine: initial load: %w", err)
	}
	go e.hotReload(ctx)
	return e, nil
}

// Load returns the current rule set.
func (e *Engine) Load() ([]Rule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out, nil
}

// Close stops the hot-reload goroutine.
func (e *Engine) Close() {
	if e.ticker != nil {
		e.ticker.Stop()
	}
	if e.stop != nil {
		close(e.stop)
	}
}

func (e *Engine) hotReload(ctx context.Context) {
	for {
		select {
		case <-e.ticker.C:
			_ = e.reload(ctx)
		case <-e.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) reload(ctx context.Context) error {
	rows, err := e.pool.Query(ctx, `
		SELECT id, domain, recipient_pattern, target, webhook_url, webhook_secret, is_catch_all, priority
		FROM mxhook_rules
		ORDER BY priority DESC
	`)
	if err != nil {
		return fmt.Errorf("rule engine: query: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(
			&r.ID, &r.Domain, &r.RecipientPattern, &r.Target,
			&r.WebhookURL, &r.WebhookSecret, &r.IsCatchAll, &r.Priority,
		); err != nil {
			return fmt.Errorf("rule engine: scan: %w", err)
		}
		rules = append(rules, r)
	}

	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
	return nil
}
