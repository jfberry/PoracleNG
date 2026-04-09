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

	// Render queue metrics
	RenderQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_render_queue_depth",
		Help: "Current number of items in the render queue",
	})

	RenderQueueCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_render_queue_capacity",
		Help: "Configured render queue capacity",
	})

	RenderDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_render_duration_seconds",
		Help:    "Time to process a render job (tile + render + deliver)",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})

	RenderTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_render_total",
		Help: "Total render jobs processed",
	}, []string{"status"})

	RenderTileSkipped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_render_tile_skipped_total",
		Help: "Tiles skipped due to render queue pressure",
	})

	// Delivery metrics
	DeliveryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_delivery_total",
		Help: "Total messages delivered",
	}, []string{"platform", "status"})

	DeliveryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_delivery_duration_seconds",
		Help:    "Time to deliver a message",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
	}, []string{"platform"})

	DeliveryQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_queue_depth",
		Help: "Current items in delivery queue",
	})

	DeliveryTrackerSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_tracker_size",
		Help: "Messages tracked for clean/edit",
	})

	DeliveryCleanTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_clean_total",
		Help: "Messages successfully deleted (clean)",
	})

	DeliveryRateLimited = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_delivery_rate_limited_total",
		Help: "Rate-limited delivery attempts",
	}, []string{"platform"})

	// Per-platform queue depths
	DeliveryDiscordQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_discord_queue_depth",
		Help: "Current discord delivery queue depth",
	})
	DeliveryWebhookQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_webhook_queue_depth",
		Help: "Current discord webhook delivery queue depth",
	})
	DeliveryTelegramQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_delivery_telegram_queue_depth",
		Help: "Current telegram delivery queue depth",
	})

	// Per-platform in-flight (concurrency saturation)
	DeliveryInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poracle_delivery_in_flight",
		Help: "Current in-flight delivery sends per platform",
	}, []string{"platform"})

	// Discord API rate limit wait time
	DeliveryRateLimitWait = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_delivery_rate_limit_wait_seconds",
		Help:    "Time spent waiting for Discord rate limits",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"platform"})

	// Message tracker evictions (TTL expiry)
	DeliveryTrackerEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poracle_delivery_tracker_evictions_total",
		Help: "Messages evicted from tracker on TTL expiry",
	})

	// Template rendering (separate from full render pipeline)
	TemplateDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_template_render_seconds",
		Help:    "DTS template rendering latency",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
	}, []string{"type"})

	TemplateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_template_render_total",
		Help: "DTS template renders by type and status",
	}, []string{"type", "status"})

	// Shlink URL shortening
	ShlinkDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_shlink_seconds",
		Help:    "Shlink URL shortening request latency",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})

	ShlinkTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_shlink_total",
		Help: "Shlink shortening outcomes",
	}, []string{"result"}) // ok, error, cache_hit, disabled

	// Uicons
	UiconsRefreshTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "poracle_uicons_refresh_total",
		Help: "Uicons index refresh outcomes",
	}, []string{"result"}) // ok, error, not_found

	UiconsRefreshDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_uicons_refresh_seconds",
		Help:    "Uicons index fetch latency",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5},
	})

	// Circuit breaker state (1=closed/healthy, 0=open/tripped)
	TileCircuitHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_tile_circuit_healthy",
		Help: "Tileserver circuit breaker state (1=closed, 0=open)",
	})

	GeocodeCircuitHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_geocode_circuit_healthy",
		Help: "Geocoding circuit breaker state (1=closed, 0=open)",
	})

	// State reload breakdown
	StateDBQueryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poracle_processor_state_db_query_seconds",
		Help:    "Time for state reload DB queries",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	})

	StateLastReloadSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "poracle_processor_state_last_reload_success_timestamp",
		Help: "Unix timestamp of last successful state reload",
	})

	// Enrichment timing
	EnrichmentDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "poracle_processor_enrichment_seconds",
		Help:    "Enrichment computation time by webhook type",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
	}, []string{"type"})
)
