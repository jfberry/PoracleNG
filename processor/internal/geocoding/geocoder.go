package geocoding

import (
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Config holds all geocoder settings.
type Config struct {
	Provider      string   // "none", "nominatim", "google"
	ProviderURL   string   // nominatim URL
	GeocodingKeys []string // google API keys
	CacheDetail   int      // decimal places for cache key rounding (default 3)
	CachePath     string   // pogreb database path
	ForwardOnly   bool     // if true, skip reverse geocoding
	AddressFormat string   // template for addr field, e.g. "{{{streetName}}} {{streetNumber}}"
	Timeout       int      // HTTP timeout in ms (default 5000)

	// Circuit breaker
	FailureThreshold int // consecutive errors before circuit opens (default 5)
	CooldownMs       int // ms to keep circuit open (default 30000)

	// Concurrency limiter
	Concurrency int // max concurrent geocode requests (default 5)
}

// Geocoder is the main entry point for geocoding operations. It wraps a
// Provider with a two-layer cache, circuit breaker, concurrency limiter,
// and address formatting.
type Geocoder struct {
	provider Provider
	cache    *Cache
	config   Config
	sem      chan struct{} // concurrency limiter

	// Circuit breaker state
	consecutiveErrors int
	circuitOpenSince  time.Time
	mu                sync.Mutex

	// Stats counters for periodic logging
	statCalls         atomic.Int64
	statTotalMs       atomic.Int64
	statErrors        atomic.Int64
	statHits          atomic.Int64
	statCircuitBreaks atomic.Int64
}

// New creates a new Geocoder from the given config.
func New(config Config) (*Geocoder, error) {
	// Defaults
	if config.CacheDetail == 0 {
		config.CacheDetail = 3
	}
	if config.Timeout == 0 {
		config.Timeout = 5000
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.CooldownMs == 0 {
		config.CooldownMs = 30000
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 5
	}

	timeout := time.Duration(config.Timeout) * time.Millisecond

	var provider Provider
	switch config.Provider {
	case "nominatim":
		provider = NewNominatim(config.ProviderURL, timeout)
	case "google":
		provider = NewGoogle(config.GeocodingKeys, timeout)
	default:
		return nil, nil
	}

	var cache *Cache
	if config.CachePath != "" && config.CacheDetail > 0 {
		var err error
		cache, err = NewCache(config.CachePath, 24*time.Hour, 50000)
		if err != nil {
			log.Warnf("Geocoder cache init failed (continuing without disk cache): %s", err)
			// Fall back to memory-only: create without disk
		}
	}

	return &Geocoder{
		provider: provider,
		cache:    cache,
		config:   config,
		sem:      make(chan struct{}, config.Concurrency),
	}, nil
}

// unknownAddress returns a minimal Address for error/skip cases.
func unknownAddress() *Address {
	return &Address{Addr: "Unknown", Flag: ""}
}

// GetAddress performs a reverse geocode lookup with caching, circuit breaker,
// and concurrency control. Returns an Address with all fields populated, or
// a minimal "Unknown" address on error. Never returns nil.
func (g *Geocoder) GetAddress(lat, lon float64) *Address {
	if g.config.ForwardOnly {
		return unknownAddress()
	}

	// Cache lookup
	var cacheKey string
	if g.cache != nil && g.config.CacheDetail > 0 {
		cacheKey = CacheKey(lat, lon, g.config.CacheDetail)
		if addr, ok := g.cache.Get(cacheKey); ok {
			g.statHits.Add(1)
			metrics.GeocodeTotal.WithLabelValues("cache_hit").Inc()
			return addr
		}
	}

	// Circuit breaker check
	g.mu.Lock()
	if g.consecutiveErrors >= g.config.FailureThreshold {
		elapsed := time.Since(g.circuitOpenSince)
		if elapsed < time.Duration(g.config.CooldownMs)*time.Millisecond {
			g.mu.Unlock()
			g.statCircuitBreaks.Add(1)
			metrics.GeocodeTotal.WithLabelValues("circuit_break").Inc()
			return unknownAddress()
		}
		// Half-open: allow one probe request
	}
	g.mu.Unlock()

	// Acquire concurrency slot
	g.sem <- struct{}{}
	defer func() { <-g.sem }()

	metrics.GeocodeInFlight.Inc()
	defer metrics.GeocodeInFlight.Dec()

	start := time.Now()
	addr, err := g.provider.Reverse(lat, lon)
	duration := time.Since(start)

	metrics.GeocodeDuration.Observe(duration.Seconds())

	if err != nil || addr == nil {
		g.statErrors.Add(1)
		metrics.GeocodeTotal.WithLabelValues("error").Inc()
		g.recordError()
		if err != nil {
			log.Warnf("Geocode %f,%f failed: %s", lat, lon, err)
		}
		return unknownAddress()
	}

	// Success — reset circuit breaker
	g.mu.Lock()
	g.consecutiveErrors = 0
	g.mu.Unlock()

	g.statCalls.Add(1)
	g.statTotalMs.Add(duration.Milliseconds())
	metrics.GeocodeTotal.WithLabelValues("success").Inc()

	// Add flag and formatted address
	addr.Flag = CountryFlag(addr.CountryCode)
	addr.Addr = FormatAddress(g.config.AddressFormat, *addr)

	log.Debugf("Geocode %.4f,%.4f → street=%q number=%q city=%q addr=%q (%dms)",
		lat, lon, addr.StreetName, addr.StreetNumber, addr.City, addr.Addr, duration.Milliseconds())

	// Escape special characters
	EscapeAddress(addr)

	// Store in cache
	if g.cache != nil && cacheKey != "" {
		g.cache.Set(cacheKey, addr)
	}

	log.Debugf("Geocode %f,%f → %s (%dms)", lat, lon, addr.Addr, duration.Milliseconds())
	return addr
}

// Forward performs a forward geocode (address search). Results are not cached.
func (g *Geocoder) Forward(query string) ([]ForwardResult, error) {
	return g.provider.Forward(query)
}

// GetStats returns the current geocoder statistics.
func (g *Geocoder) GetStats() Stats {
	return Stats{
		Calls:         g.statCalls.Load(),
		TotalMs:       g.statTotalMs.Load(),
		Errors:        g.statErrors.Load(),
		Hits:          g.statHits.Load(),
		CircuitBreaks: g.statCircuitBreaks.Load(),
	}
}

// ResetStats resets all statistics to zero.
func (g *Geocoder) ResetStats() {
	g.statCalls.Store(0)
	g.statTotalMs.Store(0)
	g.statErrors.Store(0)
	g.statHits.Store(0)
	g.statCircuitBreaks.Store(0)
}

// Close shuts down the geocoder and releases resources.
func (g *Geocoder) Close() error {
	if g.cache != nil {
		return g.cache.Close()
	}
	return nil
}

// recordError increments the consecutive error counter and opens the circuit
// if the threshold is reached.
func (g *Geocoder) recordError() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.consecutiveErrors++
	if g.consecutiveErrors >= g.config.FailureThreshold {
		g.circuitOpenSince = time.Now()
	}
}
