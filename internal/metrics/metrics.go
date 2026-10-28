package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler exposes metrics in Prometheus text format.
func Handler() http.Handler { return promhttp.Handler() }

// Snapshot contains durable backlog counts sampled when metrics are scraped.
type Snapshot struct {
	QueueDepth  int64
	OutboxDepth int64
	DeadDepth   int64
}

// Snapshotter reads durable backlog counts from the configured state store.
type Snapshotter interface {
	MetricsSnapshot(ctx context.Context) (Snapshot, error)
}

// HandlerWithSnapshot refreshes durable gauges before serving Prometheus data.
func HandlerWithSnapshot(snapshotter Snapshotter) http.Handler {
	base := promhttp.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		snapshot, err := snapshotter.MetricsSnapshot(ctx)
		if err != nil {
			MetricsSnapshotHealthy.Set(0)
		} else {
			QueueDepth.Set(float64(snapshot.QueueDepth))
			OutboxDepth.Set(float64(snapshot.OutboxDepth))
			DeadDepth.Set(float64(snapshot.DeadDepth))
			MetricsSnapshotHealthy.Set(1)
		}
		base.ServeHTTP(w, r)
	})
}

// Delivery outcome label values recorded on WebhookDeliveries. Alert rules
// in deploy/prometheus/mxhook-alerts.yml match these literals; keep them in
// sync when changing values here.
const (
	OutcomeSuccess = "success"
	OutcomeRetry   = "retry"
	OutcomeDead    = "dead"
	// OutcomeLeaseLost marks a finish write rejected because another worker
	// owns the row. The attempt's work already happened, so the row will be
	// delivered again: this counter is the duplicate-delivery signal.
	OutcomeLeaseLost = "lease_lost"
)

var (
	// MessagesReceived counts ingress messages durably enqueued.
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_messages_received_total",
		Help: "Total ingress emails durably enqueued.",
	})

	// ParseErrors counts failed parse attempts.
	ParseErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_parse_errors_total",
		Help: "Total email parse failures.",
	})

	// WebhookDeliveries tracks delivery outcomes by label.
	WebhookDeliveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mxhook_webhook_deliveries_total",
		Help: "Total webhook delivery attempts by outcome.",
	}, []string{"outcome"}) // success, retry, dead

	// WebhookRetries counts retry attempts.
	WebhookRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_webhook_retries_total",
		Help: "Total webhook delivery retry attempts.",
	})

	// DeliveryLatency measures end-to-end ingestion-to-delivery latency.
	DeliveryLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mxhook_delivery_latency_seconds",
		Help:    "End-to-end latency from SMTP accept to webhook delivery.",
		Buckets: prometheus.DefBuckets,
	})

	// ParseLatency measures email parse duration.
	ParseLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mxhook_parse_latency_seconds",
		Help:    "Time taken to parse an email.",
		Buckets: prometheus.DefBuckets,
	})

	// QueueDepth tracks current queue depth (gauge).
	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mxhook_queue_depth",
		Help: "Current number of messages in the queue.",
	})

	// OutboxDepth tracks webhook deliveries waiting or in progress.
	OutboxDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mxhook_outbox_depth",
		Help: "Current webhook deliveries pending or in progress.",
	})

	// DeadDepth tracks terminal ingress and webhook delivery failures.
	DeadDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mxhook_dead_depth",
		Help: "Current terminal ingress and webhook delivery failures.",
	})

	// MetricsSnapshotHealthy reports whether the durable gauge refresh succeeded.
	MetricsSnapshotHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mxhook_metrics_snapshot_healthy",
		Help: "Whether the most recent durable metrics snapshot succeeded.",
	})
)
