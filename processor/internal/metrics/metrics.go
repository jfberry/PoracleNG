package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WebhooksReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_webhooks_received_total",
		Help: "Inbound webhooks received by type",
	}, []string{"type"})

	WebhookProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_processor_webhook_processing_seconds",
		Help:    "Time to process a webhook",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"type"})

	MatchedUsers = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_matched_users_total",
		Help: "Number of matched user-alert pairs produced",
	}, []string{"type"})

	MatchedEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_matched_events_total",
		Help: "Events that matched at least one user",
	}, []string{"type"})

	DuplicatesSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_duplicates_skipped_total",
		Help: "Duplicate events dropped",
	}, []string{"type"})

	WorkerPoolInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_worker_pool_in_use",
		Help: "Current number of occupied worker pool slots",
	})

	WorkerPoolCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_worker_pool_capacity",
		Help: "Total worker pool capacity",
	})

	SenderQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_sender_queue_depth",
		Help: "Current items queued in sender batch",
	})

	SenderBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_sender_batch_size",
		Help:    "Number of items per outbound flush",
		Buckets: []float64{1, 5, 10, 20, 50, 100, 200},
	})

	SenderFlushDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_sender_flush_seconds",
		Help:    "Time to POST a batch to alerter",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	})

	SenderBatches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_sender_batches_total",
		Help: "Outbound batch sends",
	}, []string{"status"})

	StateReloads = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_state_reloads_total",
		Help: "State reload count",
	}, []string{"status"})

	StateReloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_state_reload_seconds",
		Help:    "Time to reload state from DB",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
)
