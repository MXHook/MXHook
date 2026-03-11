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
	metrics.WebhookDeliveries.WithLabelValues("success").Inc()
	metrics.WebhookDeliveries.WithLabelValues("failure").Inc()
	// Just ensure no panic
}
