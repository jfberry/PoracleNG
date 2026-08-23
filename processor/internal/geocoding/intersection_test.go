package geocoding

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newIntersectionWithBase builds an Intersection pointing at a test server
// by overriding the base URL. Cache is optional.
func newIntersectionWithServer(t *testing.T, handler http.HandlerFunc, cache *Cache) *Intersection {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	i := NewIntersection(IntersectionConfig{
		Usernames:   []string{"demo"},
		Cache:       cache,
		CacheDetail: 4,
		TimeoutMs:   2000,
	})
	i.baseURL = srv.URL
	return i
}

func TestIntersection_ParsesStreets(t *testing.T) {
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"intersection":{"street1":"Main St","street2":"2nd Ave"}}`))
	}, nil)

	if got := i.GetIntersection(40.0, -73.0); got != "Main St & 2nd Ave" {
		t.Errorf("got %q, want %q", got, "Main St & 2nd Ave")
	}
}

func TestIntersection_EmptyOnNoStreets(t *testing.T) {
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"intersection":{}}`))
	}, nil)

	if got := i.GetIntersection(40.0, -73.0); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestIntersection_EmptyOnGeoNamesError(t *testing.T) {
	// GeoNames signals over-quota with HTTP 200 + a status object.
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":{"message":"the hourly limit of 1000 credits has been exceeded","value":19}}`))
	}, nil)

	if got := i.GetIntersection(40.0, -73.0); got != "" {
		t.Errorf("got %q, want empty string on GeoNames error", got)
	}
}

func TestIntersection_EmptyOnHTTP500(t *testing.T) {
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, nil)

	if got := i.GetIntersection(40.0, -73.0); got != "" {
		t.Errorf("got %q, want empty string on HTTP 500", got)
	}
}

// A successful lookup is cached so the second call makes no HTTP request.
func TestIntersection_CachesSuccess(t *testing.T) {
	cache := newTestCache(t)
	var calls atomic.Int32
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"intersection":{"street1":"A","street2":"B"}}`))
	}, cache)

	if got := i.GetIntersection(1.23456, 2.34567); got != "A & B" {
		t.Fatalf("first call got %q", got)
	}
	if got := i.GetIntersection(1.23456, 2.34567); got != "A & B" {
		t.Fatalf("second call got %q", got)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 HTTP call (second served from cache), got %d", n)
	}
}

// A stable "no intersection" result is negative-cached: no repeat HTTP call.
func TestIntersection_NegativeCaches(t *testing.T) {
	cache := newTestCache(t)
	var calls atomic.Int32
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"intersection":{}}`))
	}, cache)

	i.GetIntersection(5.5, 6.6)
	i.GetIntersection(5.5, 6.6)
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 HTTP call (empty result negative-cached), got %d", n)
	}
}

// A transient failure (HTTP 500) must NOT be cached — it should retry.
func TestIntersection_DoesNotCacheFailure(t *testing.T) {
	cache := newTestCache(t)
	var calls atomic.Int32
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}, cache)

	i.GetIntersection(7.7, 8.8)
	i.GetIntersection(7.7, 8.8)
	if n := calls.Load(); n != 2 {
		t.Errorf("expected 2 HTTP calls (failures not cached), got %d", n)
	}
}

func TestIntersection_NoUsernamesReturnsEmpty(t *testing.T) {
	i := NewIntersection(IntersectionConfig{Usernames: nil})
	if got := i.GetIntersection(1, 2); got != "" {
		t.Errorf("got %q, want empty with no usernames", got)
	}
}

// After FailureThreshold consecutive failures the circuit opens and further
// lookups return immediately without hitting GeoNames (until cooldown).
func TestIntersection_CircuitBreakerOpensAfterFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	i := NewIntersection(IntersectionConfig{
		Usernames:        []string{"demo"},
		TimeoutMs:        2000,
		FailureThreshold: 3,
		CooldownMs:       60000, // long cooldown so the circuit stays open in-test
	})
	i.baseURL = srv.URL

	// First 3 calls hit the server and fail, tripping the breaker.
	for n := range 3 {
		if got := i.GetIntersection(float64(n), 0); got != "" {
			t.Fatalf("call %d: got %q, want empty", n, got)
		}
	}
	// Subsequent calls are short-circuited — no further HTTP.
	for n := range 5 {
		i.GetIntersection(float64(100+n), 0)
	}
	if c := calls.Load(); c != 3 {
		t.Errorf("expected 3 HTTP calls before the circuit opened, got %d", c)
	}
}

// A success after failures resets the breaker.
func TestIntersection_CircuitBreakerResetsOnSuccess(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"intersection":{"street1":"A","street2":"B"}}`))
	}))
	t.Cleanup(srv.Close)

	i := NewIntersection(IntersectionConfig{
		Usernames:        []string{"demo"},
		TimeoutMs:        2000,
		FailureThreshold: 5,
	})
	i.baseURL = srv.URL

	// 2 failures (below threshold), then a success resets the counter.
	i.GetIntersection(1, 0)
	i.GetIntersection(2, 0)
	fail.Store(false)
	if got := i.GetIntersection(3, 0); got != "A & B" {
		t.Fatalf("recovery call got %q", got)
	}
	// Breaker reset: 4 more failures shouldn't open it (counter started over).
	fail.Store(true)
	before := calls.Load()
	for n := range 4 {
		i.GetIntersection(float64(10+n), 0)
	}
	if c := calls.Load() - before; c != 4 {
		t.Errorf("expected 4 HTTP calls after reset (circuit still closed), got %d", c)
	}
}

func TestIntersection_RespectsTimeout(t *testing.T) {
	i := newIntersectionWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"intersection":{"street1":"A","street2":"B"}}`))
	}, nil)
	i.client.Timeout = 50 * time.Millisecond

	if got := i.GetIntersection(1, 2); got != "" {
		t.Errorf("got %q, want empty on timeout", got)
	}
}
