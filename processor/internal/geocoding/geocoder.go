package geocoding

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/breaker"
	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Config holds all geocoder settings.
type Config struct {
	Provider      string   // "none", "nominatim", "photon", "google"
	ProviderURL   string   // nominatim/photon URL
	GeocodingKeys []string // google API keys
	CacheDetail   int      // decimal places for cache key rounding (default 3)
	CachePath     string   // pogreb database path
	ForwardOnly   bool     // if true, skip reverse geocoding
	AddressFormat string   // template for addr field, e.g. "{{{streetName}}} {{streetNumber}}"
	// IncludeCountry controls whether ocfmt renders the country name into
	// FormattedAddress. Defaults to false — the country tends to be noise
	// for single-country operator audiences.
	IncludeCountry bool
	Timeout        int // HTTP timeout in ms (default 5000)

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
	addrTmpl *AddressTemplate           // compiled once from config.AddressFormat
	breaker  *breaker.Breaker[*Address] // circuit breaker + concurrency limiter

	// Stats counters for periodic logging
	statCalls         atomic.Int64
	statTotalMs       atomic.Int64
	statErrors        atomic.Int64
	statHits          atomic.Int64
	statCircuitBreaks atomic.Int64
}

// errNilAddress marks a provider returning (nil, nil) — treated as a failure
// so it counts toward the circuit breaker.
var errNilAddress = errors.New("geocode: provider returned nil address")

// New creates a new Geocoder from the given config.
func New(config Config) (*Geocoder, error) {
	// Defaults
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
		provider = NewNominatim(config.ProviderURL, timeout, config.IncludeCountry)
	case "photon":
		provider = NewPhoton(config.ProviderURL, timeout, config.IncludeCountry)
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

	addrTmpl, err := CompileAddressTemplate(config.AddressFormat)
	if err != nil {
		log.Warnf("Geocoder: %s — falling back to formattedAddress", err)
	}

	g := &Geocoder{
		provider: provider,
		cache:    cache,
		config:   config,
		addrTmpl: addrTmpl,
	}
	g.breaker = breaker.New[*Address](breaker.Config{
		Name:             "geocoder-" + config.Provider,
		FailureThreshold: config.FailureThreshold,
		Cooldown:         time.Duration(config.CooldownMs) * time.Millisecond,
		Concurrency:      config.Concurrency,
		OnHealthChange: func(healthy bool) {
			if healthy {
				metrics.GeocodeCircuitHealthy.Set(1)
			} else {
				metrics.GeocodeCircuitHealthy.Set(0)
			}
		},
	})
	metrics.GeocodeCircuitHealthy.Set(1) // start healthy (gobreaker fires OnStateChange only on transition)
	return g, nil
}

// unknownAddress returns a minimal Address for error/skip cases.
func unknownAddress() *Address {
	return &Address{Addr: "Unknown", Flag: ""}
}

// GetAddress performs a reverse geocode lookup with caching, circuit breaker,
// and concurrency control. Returns an Address with all fields populated, or
// a minimal "Unknown" address on error. Never returns nil.
func (g *Geocoder) GetAddress(lat, lon float64) *Address {
	return g.GetAddressForLanguage(lat, lon, "")
}

// GetAddressForLanguage performs a reverse geocode lookup in the requested
// language when the configured provider supports language-specific results.
// The language code is part of the cache key so localized addresses never
// bleed across users with different locales.
func (g *Geocoder) GetAddressForLanguage(lat, lon float64, language string) *Address {
	if g.config.ForwardOnly {
		return unknownAddress()
	}
	language = normalizeLanguage(language)

	// Cache lookup
	var cacheKey string
	if g.cache != nil && g.config.CacheDetail > 0 {
		cacheKey = CacheKeyForLanguage(lat, lon, g.config.CacheDetail, language)
		if addr, ok := g.cache.Get(cacheKey); ok {
			g.statHits.Add(1)
			metrics.GeocodeTotal.WithLabelValues("cache_hit").Inc()
			return addr
		}
	}

	// Reverse geocode behind the circuit breaker (which also bounds
	// concurrency). The breaker trips on consecutive provider failures.
	addr, err := g.breaker.Do(func() (*Address, error) {
		metrics.GeocodeInFlight.Inc()
		defer metrics.GeocodeInFlight.Dec()

		start := time.Now()
		a, rerr := g.provider.Reverse(lat, lon, language)
		duration := time.Since(start)
		metrics.GeocodeDuration.Observe(duration.Seconds())

		if rerr != nil || a == nil {
			g.statErrors.Add(1)
			metrics.GeocodeTotal.WithLabelValues("error").Inc()
			// Log only a genuine provider error; a (nil, nil) "no address found"
			// was silent before the refactor — keep it silent to avoid flooding
			// the warn log in high-volume unresolvable areas. It still trips the
			// breaker (parity with the old recordError path) via errNilAddress.
			if rerr != nil {
				log.Warnf("Geocode %f,%f failed: %s", lat, lon, rerr)
			} else {
				rerr = errNilAddress
			}
			return nil, rerr
		}

		g.statCalls.Add(1)
		g.statTotalMs.Add(duration.Milliseconds())
		metrics.GeocodeTotal.WithLabelValues("success").Inc()
		return a, nil
	})

	if errors.Is(err, breaker.ErrOpen) {
		g.statCircuitBreaks.Add(1)
		metrics.GeocodeTotal.WithLabelValues("circuit_break").Inc()
		return unknownAddress()
	}
	if err != nil {
		// Provider error — already counted and logged inside the breaker fn.
		return unknownAddress()
	}

	// Add flag and formatted address (template rendered via raymond so
	// operators get {{#if}}/{{#unless}}/helpers for conditional layout).
	addr.Flag = CountryFlag(addr.CountryCode)
	addr.Addr = g.addrTmpl.Render(*addr)

	log.Debugf("Geocode %.4f,%.4f → street=%q number=%q city=%q addr=%q",
		lat, lon, addr.StreetName, addr.StreetNumber, addr.City, addr.Addr)

	// Escape special characters
	EscapeAddress(addr)

	// Store in cache
	if g.cache != nil && cacheKey != "" {
		g.cache.Set(cacheKey, addr)
	}

	log.Debugf("Geocode %f,%f → %s", lat, lon, addr.Addr)
	return addr
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

// Forward performs a forward geocode (address search). Results are not cached.
//
// Failures and empty result sets are logged at WARN. Forward geocoding is
// user-initiated and low-frequency (the !location command and the
// /api/geocode/forward endpoint), and the bot surfaces only a generic "could
// not understand the location" to the user — so this log is the only place the
// real cause (unreachable URL, HTTP status, unfinished import, out-of-region
// query) is visible to an operator.
func (g *Geocoder) Forward(query string) ([]ForwardResult, error) {
	results, err := g.provider.Forward(query)
	if err != nil {
		log.Warnf("Forward geocode %q failed: %s", query, err)
		return results, err
	}
	if len(results) == 0 {
		log.Warnf("Forward geocode %q returned no results — provider reachable but no match; check the import has finished and the query falls within the imported area", query)
	}
	return results, nil
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

// CacheStats returns a point-in-time snapshot of the geocoder's cache health.
// Returns a zero-value CacheStats when no cache is configured.
func (g *Geocoder) CacheStats() CacheStats {
	if g.cache == nil {
		return CacheStats{}
	}
	return g.cache.Stats()
}

// Cache returns the geocoder's two-layer cache so other geocoding features
// (e.g. intersection lookups) can share the same pogreb DB. Returns nil when
// no cache is configured.
func (g *Geocoder) Cache() *Cache {
	return g.cache
}

// ClearCache drops all entries from the in-memory cache layer.
// Returns the number of entries cleared. Safe to call when no cache is configured.
func (g *Geocoder) ClearCache() int {
	if g.cache == nil {
		return 0
	}
	return g.cache.ClearMemory()
}

// Close shuts down the geocoder and releases resources.
func (g *Geocoder) Close() error {
	if g.cache != nil {
		return g.cache.Close()
	}
	return nil
}
