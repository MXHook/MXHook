package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MessagesReceived counts SMTP-accepted messages.
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_messages_received_total",
		Help: "Total emails accepted by SMTP receiver.",
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
	}, []string{"outcome"}) // success, failure, dlq

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
)
