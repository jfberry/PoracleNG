package metrics

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// IntervalCounters tracks counts between periodic log resets.
var (
	IntervalWebhooks atomic.Int64
	IntervalMatched  atomic.Int64
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

	// Rate limiting metrics
	RateLimitDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_processor_rate_limit_dropped_total",
		Help: "Messages dropped by rate limiter",
	})

	RateLimitBreaches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_processor_rate_limit_breaches_total",
		Help: "Number of rate limit breach events (user notified)",
	})

	RateLimitDisabled = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_processor_rate_limit_disabled_total",
		Help: "Users disabled for exceeding rate limit violation threshold",
	})

	// Tileserver metrics
	TileDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_tile_seconds",
		Help:    "Tileserver request latency",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	TileTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_tile_total",
		Help: "Tile generation outcomes",
	}, []string{"result"})
	TileInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_tile_in_flight",
		Help: "Concurrent tile requests",
	})
	TileQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_tile_queue_depth",
		Help: "Pending tile requests in async queue",
	})

	// Geocoding metrics
	GeocodeDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_geocode_seconds",
		Help:    "Geocoding request latency",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	GeocodeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_geocode_total",
		Help: "Geocoding outcomes",
	}, []string{"result"})
	GeocodeInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_geocode_in_flight",
		Help: "Concurrent geocode requests",
	})

	// AccuWeather metrics
	AccuWeatherRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_accuweather_requests_total",
		Help: "AccuWeather API requests by type and status",
	}, []string{"type", "status"})

	AccuWeatherQuotaRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poracle_processor_accuweather_quota_remaining",
		Help: "Remaining AccuWeather API quota (from RateLimit-Remaining header)",
	}, []string{"key_index"})

	AccuWeatherQuotaExhausted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_processor_accuweather_quota_exhausted_total",
		Help: "Number of times all AccuWeather API keys had no remaining quota",
	})

	AccuWeatherKeyUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poracle_processor_accuweather_key_usage_today",
		Help: "Number of API calls made today per key",
	}, []string{"key_index"})

	// State size metrics
	StateHumans = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_state_humans",
		Help: "Number of humans loaded in state",
	})
	StateTrackingRules = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poracle_processor_state_tracking_rules",
		Help: "Number of tracking rules loaded in state",
	}, []string{"type"})
	StateGeofences = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_state_geofences",
		Help: "Number of geofences loaded in state",
	})

	// Webhook batch size
	WebhookBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_webhook_batch_size",
		Help:    "Number of items per inbound webhook POST from Golbat",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
	})

	// Matching duration (just the Match() call, excluding parse/enrich/send)
	MatchingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_processor_matching_seconds",
		Help:    "Time to match a webhook against tracking rules",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
	}, []string{"type"})

	// Build info
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poracle_processor_build_info",
		Help: "Build information",
	}, []string{"version", "commit", "date"})

	// API metrics
	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_processor_api_request_seconds",
		Help:    "API request duration",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"method", "endpoint"})
	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_processor_api_requests_total",
		Help: "API requests by method, endpoint, and status",
	}, []string{"method", "endpoint", "status"})
)
