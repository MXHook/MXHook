package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mxhook/mxhook/internal/metrics"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

type EventOutbox interface {
	Commit(ctx context.Context, event *types.EmailEvent, rules []router.Rule) error
}

// ruleCacheTTL bounds how long a loaded rule set is reused. Loading decrypts
// every route secret, which is wasted work once per message; a short TTL keeps
// route edits taking effect promptly.
const ruleCacheTTL = 5 * time.Second

type IntakeProcessor struct {
	parser Parser
	raw    RawStore
	rules  RuleLoader
	outbox EventOutbox

	cacheMu     sync.Mutex
	cachedRules []router.Rule
	cachedAt    time.Time
	now         func() time.Time
}

func NewIntakeProcessor(parser Parser, raw RawStore, rules RuleLoader, outbox EventOutbox) (*IntakeProcessor, error) {
	if parser == nil || raw == nil || rules == nil || outbox == nil {
		return nil, errors.New("runtime: parser, raw store, rule loader, and event outbox are required")
	}
	return &IntakeProcessor{parser: parser, raw: raw, rules: rules, outbox: outbox, now: time.Now}, nil
}

// InvalidateRules drops the cached rule set so the next message reloads it.
func (p *IntakeProcessor) InvalidateRules() {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cachedRules, p.cachedAt = nil, time.Time{}
}

func (p *IntakeProcessor) loadRules() ([]router.Rule, error) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if p.cachedRules != nil && p.now().Sub(p.cachedAt) < ruleCacheTTL {
		return p.cachedRules, nil
	}
	rules, err := p.rules.Load()
	if err != nil {
		return nil, fmt.Errorf("runtime: load rules: %w", err)
	}
	p.cachedRules, p.cachedAt = rules, p.now()
	return rules, nil
}

func (p *IntakeProcessor) Process(ctx context.Context, message queue.Message) error {
	rawPath, err := p.raw.Store(ctx, message.ID, message.Body)
	if err != nil {
		return fmt.Errorf("runtime: store raw: %w", err)
	}
	parseStarted := time.Now()
	event, err := p.parser.Parse(message.ID, message.EnvelopeFrom, message.EnvelopeTo, bytes.NewReader(message.Body))
	metrics.ParseLatency.Observe(time.Since(parseStarted).Seconds())
	if err != nil {
		metrics.ParseErrors.Inc()
		return Permanent(fmt.Errorf("runtime: parse: %w", err))
	}
	event.ReceivedAt = message.ReceivedAt
	event.TraceID = message.TraceID
	event.RawPath = rawPath
	rules, err := p.loadRules()
	if err != nil {
		return fmt.Errorf("runtime: load rules: %w", err)
	}
	matcher := router.NewMatcher(rules)
	matched := make([]router.Rule, 0, len(message.EnvelopeTo))
	seen := make(map[string]struct{})
	for _, recipient := range message.EnvelopeTo {
		rule, ok := matcher.Match(recipient)
		if !ok {
			continue
		}
		if rule.ID == "" {
			return Permanent(errors.New("runtime: matched route has no stable ID"))
		}
		if _, exists := seen[rule.ID]; exists {
			continue
		}
		seen[rule.ID] = struct{}{}
		matched = append(matched, rule)
	}
	if len(matched) == 0 {
		return Permanent(errors.New("runtime: no route matched accepted recipients"))
	}
	if err := p.outbox.Commit(ctx, event, matched); err != nil {
		return fmt.Errorf("runtime: commit event outbox: %w", err)
	}
	return nil
}
