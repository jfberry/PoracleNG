package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/breaker"
)

// defaultIntersectionBaseURL is the GeoNames OSM intersection endpoint. The
// OSM variant (vs the US-only findNearestIntersectionJSON used by PoracleJS)
// gives worldwide coverage. HTTPS is served from the secure.geonames.org host —
// api.geonames.org does not answer over TLS.
const defaultIntersectionBaseURL = "https://secure.geonames.org/findNearestIntersectionOSMJSON"

// IntersectionConfig configures intersection lookups.
type IntersectionConfig struct {
	// Usernames is the pool of GeoNames usernames; one is picked at random
	// per uncached lookup to spread credit usage. Empty disables lookups.
	Usernames []string
	// Cache, when non-nil, is shared with the reverse geocoder so intersection
	// results live in the same pogreb DB (under a separate key namespace).
	// When nil, lookups run uncached.
	Cache *Cache
	// CacheDetail is the lat/lon rounding (decimal places) for cache keys.
	// Defaults to 3 when <= 0.
	CacheDetail int
	// TimeoutMs is the per-request HTTP timeout. Defaults to 5000.
	TimeoutMs int
	// Concurrency bounds simultaneous outbound GeoNames requests so a cold
	// cache can't tie up the whole webhook worker pool. Defaults to 5. This is
	// independent of the reverse geocoder's limiter — GeoNames is a separate
	// service with its own credit pool.
	Concurrency int
	// FailureThreshold is the number of consecutive failures before the circuit
	// opens (stops calling GeoNames). Defaults to 5.
	FailureThreshold int
	// CooldownMs is how long the circuit stays open before a half-open probe.
	// Defaults to 30000.
	CooldownMs int
}

// Intersection fetches the nearest street intersection for a coordinate from
// GeoNames, with optional shared-cache backing. Reuses a single HTTP client
// and routes every lookup through a shared circuit breaker (which also bounds
// concurrency), so a GeoNames outage can't park webhook workers on doomed
// requests.
type Intersection struct {
	usernames   []string
	cache       *Cache
	cacheDetail int
	client      *http.Client
	breaker     *breaker.Breaker[string]
	baseURL     string // overridable in tests
}

// NewIntersection builds an Intersection from config. Returns a usable (no-op
// on empty usernames) instance.
func NewIntersection(cfg IntersectionConfig) *Intersection {
	detail := cfg.CacheDetail
	if detail <= 0 {
		detail = 3
	}
	timeout := cfg.TimeoutMs
	if timeout <= 0 {
		timeout = 5000
	}
	conc := cfg.Concurrency
	if conc <= 0 {
		conc = 5
	}
	return &Intersection{
		usernames:   cfg.Usernames,
		cache:       cfg.Cache,
		cacheDetail: detail,
		client:      &http.Client{Timeout: time.Duration(timeout) * time.Millisecond},
		breaker: breaker.New[string](breaker.Config{
			Name:             "geonames-intersection",
			FailureThreshold: cfg.FailureThreshold,
			Cooldown:         time.Duration(cfg.CooldownMs) * time.Millisecond,
			Concurrency:      conc,
		}),
		baseURL: defaultIntersectionBaseURL,
	}
}

// geonamesResponse models the fields we read from the GeoNames reply. Over-quota
// and other API-level errors arrive as HTTP 200 with a populated Status object.
type geonamesResponse struct {
	Intersection struct {
		Street1 string `json:"street1"`
		Street2 string `json:"street2"`
	} `json:"intersection"`
	Status *struct {
		Message string `json:"message"`
		Value   int    `json:"value"`
	} `json:"status"`
}

// GetIntersection returns "<street1> & <street2>" for the nearest intersection,
// or "" when none is found, lookups are disabled, or the request fails. A
// successful result (including a stable "no intersection here") is cached;
// transient failures and circuit-open skips are not, so they retry later.
func (i *Intersection) GetIntersection(lat, lon float64) string {
	if len(i.usernames) == 0 {
		return ""
	}

	var cacheKey string
	if i.cache != nil {
		cacheKey = IntersectionCacheKey(lat, lon, i.cacheDetail)
		if v, ok := i.cache.GetIntersection(cacheKey); ok {
			return v
		}
	}

	result, err := i.breaker.Do(func() (string, error) {
		return i.fetch(lat, lon)
	})
	if err != nil {
		// Transient failure or open circuit — don't cache, so a later sighting
		// retries once the service recovers.
		return ""
	}
	if i.cache != nil {
		i.cache.SetIntersection(cacheKey, result)
	}
	return result
}

// fetch performs one GeoNames request. A nil error means a usable answer
// (including "no intersection nearby", returned as "" — a cacheable fact); a
// non-nil error is a transient failure that counts toward tripping the breaker.
func (i *Intersection) fetch(lat, lon float64) (string, error) {
	username := i.usernames[rand.IntN(len(i.usernames))]

	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lng", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("username", username)
	reqURL := i.baseURL + "?" + q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), i.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		log.Debugf("GeoNames intersection: build request: %v", err)
		return "", err
	}

	resp, err := i.client.Do(req)
	if err != nil {
		log.Warnf("GeoNames intersection request failed for %f,%f: %v", lat, lon, err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warnf("GeoNames intersection: HTTP %d for %f,%f", resp.StatusCode, lat, lon)
		return "", fmt.Errorf("geonames: http %d", resp.StatusCode)
	}

	var result geonamesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Warnf("GeoNames intersection: decode response: %v", err)
		return "", err
	}

	// API-level error (e.g. credit exhaustion) — surface it and trip the breaker.
	if result.Status != nil {
		log.Warnf("GeoNames intersection: API error %d for %f,%f: %s", result.Status.Value, lat, lon, result.Status.Message)
		return "", fmt.Errorf("geonames: api error %d: %s", result.Status.Value, result.Status.Message)
	}

	if result.Intersection.Street1 != "" && result.Intersection.Street2 != "" {
		return fmt.Sprintf("%s & %s", result.Intersection.Street1, result.Intersection.Street2), nil
	}

	// Successful call, genuinely no intersection nearby — cacheable.
	return "", nil
}
