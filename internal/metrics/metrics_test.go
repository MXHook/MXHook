package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/mxhook/mxhook/internal/metrics"
)

func TestMetrics_CountersIncrement(t *testing.T) {
	metrics.MessagesReceived.Add(1)
	if v := testutil.ToFloat64(metrics.MessagesReceived); v < 1 {
		t.Errorf("MessagesReceived: got %v, want >= 1", v)
	}

	metrics.ParseErrors.Add(1)
	if v := testutil.ToFloat64(metrics.ParseErrors); v < 1 {
		t.Errorf("ParseErrors: got %v, want >= 1", v)
	}
}

func TestMetrics_WebhookDeliveries(t *testing.T) {
	for _, outcome := range []string{metrics.OutcomeSuccess, metrics.OutcomeRetry, metrics.OutcomeDead} {
		counter, err := metrics.WebhookDeliveries.GetMetricWithLabelValues(outcome)
		if err != nil {
			t.Fatalf("WebhookDeliveries outcome %q: %v", outcome, err)
		}
		before := testutil.ToFloat64(counter)
		counter.Inc()
		if got := testutil.ToFloat64(counter); got != before+1 {
			t.Errorf("WebhookDeliveries outcome %q: got %v, want %v", outcome, got, before+1)
		}
	}
}

// TestMetrics_DeliveryOutcomeContract pins the outcome label values the
// delivery worker emits. Alert rules in deploy/prometheus/mxhook-alerts.yml
// match these literals; changing a value here without updating the rules
// silently disables alerting.
func TestMetrics_DeliveryOutcomeContract(t *testing.T) {
	outcomes := []struct{ name, got, want string }{
		{"OutcomeSuccess", metrics.OutcomeSuccess, "success"},
		{"OutcomeRetry", metrics.OutcomeRetry, "retry"},
		{"OutcomeDead", metrics.OutcomeDead, "dead"},
	}
	for _, outcome := range outcomes {
		if outcome.got != outcome.want {
			t.Errorf("%s = %q, want %q", outcome.name, outcome.got, outcome.want)
		}
	}
}
