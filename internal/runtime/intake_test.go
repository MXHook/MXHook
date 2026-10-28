package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/parser"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

type captureRawStore struct {
	path string
	body []byte
}

func (s *captureRawStore) Store(_ context.Context, _ string, body []byte) (string, error) {
	s.body = append([]byte(nil), body...)
	return s.path, nil
}

type staticRules []router.Rule

func (r staticRules) Load() ([]router.Rule, error) { return r, nil }

type countingRules struct {
	rules []router.Rule
	loads int
}

func (r *countingRules) Load() ([]router.Rule, error) {
	r.loads++
	return r.rules, nil
}

type captureOutbox struct {
	event *types.EmailEvent
	rules []router.Rule
	err   error
}

func (o *captureOutbox) Commit(_ context.Context, event *types.EmailEvent, rules []router.Rule) error {
	o.event = event
	o.rules = append([]router.Rule(nil), rules...)
	return o.err
}

func TestIntakeProcessorCachesRulesAndHonoursInvalidation(t *testing.T) {
	rules := &countingRules{rules: []router.Rule{{
		ID: "route-1", RecipientPattern: "*@example.com", Target: router.TargetWebhook,
		WebhookURL: "https://hooks.example.net/inbox", WebhookSecret: "secret",
	}}}
	processor, err := NewIntakeProcessor(parser.New(), &captureRawStore{path: "raw/m.eml"}, rules, &captureOutbox{})
	if err != nil {
		t.Fatalf("NewIntakeProcessor: %v", err)
	}
	clock := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return clock }
	deliver := func(id string) {
		t.Helper()
		message := queue.NewMessage(id, "smtp", "", "sender@example.net",
			[]string{"one@example.com"}, clock, []byte("Subject: s\r\n\r\nbody"))
		if err := processor.Process(context.Background(), message); err != nil {
			t.Fatalf("Process %s: %v", id, err)
		}
	}
	deliver("m-1")
	deliver("m-2")
	if rules.loads != 1 {
		t.Fatalf("loads = %d, want 1: rules must be cached across messages", rules.loads)
	}
	processor.InvalidateRules()
	deliver("m-3")
	if rules.loads != 2 {
		t.Fatalf("loads = %d, want 2: invalidation must force a reload", rules.loads)
	}
	clock = clock.Add(ruleCacheTTL + time.Second)
	deliver("m-4")
	if rules.loads != 3 {
		t.Fatalf("loads = %d, want 3: cache must expire after its TTL", rules.loads)
	}
}

func TestIntakeProcessorCommitsEventAndDeduplicatedRouteSnapshots(t *testing.T) {
	raw := &captureRawStore{path: "raw/message.eml"}
	outbox := &captureOutbox{}
	processor, err := NewIntakeProcessor(parser.New(), raw, staticRules{{
		ID: "route-1", RecipientPattern: "*@example.com", Target: router.TargetWebhook,
		WebhookURL: "https://hooks.example.net/inbox", WebhookSecret: "secret",
	}}, outbox)
	if err != nil {
		t.Fatalf("NewIntakeProcessor: %v", err)
	}
	receivedAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	message := queue.NewMessage(
		"message-1", "smtp", "", "sender@example.net",
		[]string{"one@example.com", "two@example.com"}, receivedAt,
		[]byte("Subject: production\r\nMessage-ID: <one@example.net>\r\n\r\nbody"),
	)
	if err := processor.Process(context.Background(), message); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(outbox.rules) != 1 {
		t.Fatalf("outbox routes = %d, want 1", len(outbox.rules))
	}
	if outbox.event == nil || outbox.event.Subject != "production" || outbox.event.RawPath != raw.path ||
		!outbox.event.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("outbox event = %#v", outbox.event)
	}
	if string(raw.body) != string(message.Body) {
		t.Fatalf("stored raw = %q", raw.body)
	}
}

func TestIntakeProcessorRoutesMixedCaseRecipient(t *testing.T) {
	outbox := &captureOutbox{}
	processor, err := NewIntakeProcessor(parser.New(), &captureRawStore{path: "raw/mixed.eml"}, staticRules{{
		ID: "route-1", RecipientPattern: "*@example.com", Target: router.TargetWebhook,
		WebhookURL: "https://hooks.example.net/inbox", WebhookSecret: "secret",
	}}, outbox)
	if err != nil {
		t.Fatalf("NewIntakeProcessor: %v", err)
	}
	message := queue.NewMessage(
		"mixed-case", "smtp", "", "sender@example.net", []string{"User@EXAMPLE.COM"},
		time.Unix(1, 0), []byte("Subject: mixed\r\n\r\nbody"),
	)
	if err := processor.Process(context.Background(), message); err != nil {
		t.Fatalf("Process error = %v, want mixed-case recipient routed", err)
	}
	if len(outbox.rules) != 1 || outbox.rules[0].ID != "route-1" {
		t.Fatalf("outbox routes = %#v, want route-1", outbox.rules)
	}
}

func TestIntakeProcessorClassifiesInvalidMessageAndMissingRouteAsPermanent(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    []byte
		routes  staticRules
		message string
	}{
		{name: "invalid message", body: []byte("not a valid header"), message: "parse"},
		{name: "missing route", body: []byte("Subject: ok\r\n\r\nbody"), message: "no route"},
	} {
		t.Run(test.name, func(t *testing.T) {
			processor, err := NewIntakeProcessor(parser.New(), &captureRawStore{}, test.routes, &captureOutbox{})
			if err != nil {
				t.Fatalf("NewIntakeProcessor: %v", err)
			}
			message := queue.NewMessage(
				"bad", "test", "", "sender@example.net", []string{"inbox@example.com"},
				time.Unix(1, 0), test.body,
			)
			err = processor.Process(context.Background(), message)
			if !IsPermanent(err) || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Process error = %v, want permanent %s error", err, test.message)
			}
		})
	}
}

func TestIntakeProcessorKeepsIngressLeaseWhenOutboxCommitFails(t *testing.T) {
	outbox := &captureOutbox{err: errors.New("database unavailable")}
	processor, err := NewIntakeProcessor(parser.New(), &captureRawStore{}, staticRules{{
		ID: "route", RecipientPattern: "*", Target: router.TargetWebhook,
		WebhookURL: "https://example.com", WebhookSecret: "secret",
	}}, outbox)
	if err != nil {
		t.Fatal(err)
	}
	message := queue.NewMessage("retry", "test", "", "", []string{"inbox@example.com"}, time.Now(), []byte("Subject: retry\r\n\r\nbody"))
	if err := processor.Process(context.Background(), message); err == nil || IsPermanent(err) {
		t.Fatalf("outbox failure classification = %v", err)
	}
}
