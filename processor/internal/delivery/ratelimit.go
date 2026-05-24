package delivery

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// DiscordRateLimiter tracks per-destination rate limits and a global 50 req/sec token bucket.
type DiscordRateLimiter struct {
	mu      sync.Mutex
	targets map[string]*targetLimit
	global  *tokenBucket
	waiting atomic.Int64 // number of goroutines currently blocked in Wait()

	// 429 ring-buffer: 60 one-minute slots covering 60 minutes.
	// slot index = (minute % 60); each slot holds the count for that minute.
	// slotMinute tracks which wall-clock minute each slot was last written.
	counter429      [60]int32
	counter429Mins  [60]int64 // Unix minute when the slot was last written
	nowFunc         func() time.Time // injectable for tests; nil → time.Now
}

type targetLimit struct {
	remaining int
	limit     int // X-RateLimit-Limit (total bucket size)
	resetAt   time.Time
}

// RouteState is a point-in-time snapshot of one Discord route's rate-limit state.
type RouteState struct {
	Route     string    // route key as stored internally
	Remaining int       // X-RateLimit-Remaining
	Limit     int       // X-RateLimit-Limit
	ResetAt   time.Time // when the bucket replenishes
}

// DiscordRateSnapshot is the full point-in-time read of Discord rate-limit state.
type DiscordRateSnapshot struct {
	Routes         []RouteState // only routes where Remaining < Limit (partially consumed)
	GlobalTokens   int          // current tokens remaining in the global 50/sec bucket
	GlobalCapacity int          // configured capacity (typically 50)
	Recent429Count int          // number of 429s observed in the last 5 minutes
}

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	lastRefill time.Time
	rate       float64 // tokens per second
}

// NewDiscordRateLimiter creates a rate limiter with a global bucket of 50 tokens/sec.
func NewDiscordRateLimiter() *DiscordRateLimiter {
	return &DiscordRateLimiter{
		targets: make(map[string]*targetLimit),
		global:  newTokenBucket(50, 50),
	}
}

func newTokenBucket(maxTokens, rate float64) *tokenBucket {
	return &tokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		lastRefill: time.Now(),
		rate:       rate,
	}
}

// refill adds tokens based on elapsed time since last refill.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now
}

// tryConsume attempts to consume one token. Returns true if successful.
func (tb *tokenBucket) tryConsume() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// timeUntilAvailable returns how long until at least one token is available.
func (tb *tokenBucket) timeUntilAvailable() time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= 1 {
		return 0
	}
	deficit := 1.0 - tb.tokens
	return time.Duration(deficit/tb.rate*1000) * time.Millisecond
}

// Cleanup removes stale rate limit entries whose reset time has passed.
func (rl *DiscordRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for key, limit := range rl.targets {
		if limit.resetAt.Before(now) {
			delete(rl.targets, key)
		}
	}
}

// Wait blocks until the target is not rate-limited and a global token is available.
// It is safe to call from a single goroutine per destination (fair queue pattern).
func (rl *DiscordRateLimiter) Wait(target string) {
	// Clean up stale entries if the map has grown large.
	rl.mu.Lock()
	if len(rl.targets) > 1000 {
		now := time.Now()
		for key, limit := range rl.targets {
			if limit.resetAt.Before(now) {
				delete(rl.targets, key)
			}
		}
	}

	// Check per-target limit.
	tl, exists := rl.targets[target]
	if exists && tl.remaining <= 0 {
		sleepUntil := tl.resetAt
		rl.mu.Unlock()
		if d := time.Until(sleepUntil); d > 0 {
			if d > 5*time.Second {
				log.Infof("discord: rate limit wait %.1fs for %s", d.Seconds(), target)
			} else {
				log.Debugf("discord: rate limit wait %.1fs for %s", d.Seconds(), target)
			}
			rl.waiting.Add(1)
			time.Sleep(d)
			rl.waiting.Add(-1)
			metrics.DeliveryRateLimitWait.WithLabelValues("discord").Observe(d.Seconds())
		}
	} else {
		rl.mu.Unlock()
	}

	// Check global bucket.
	for !rl.global.tryConsume() {
		wait := rl.global.timeUntilAvailable()
		if wait <= 0 {
			wait = time.Millisecond
		}
		time.Sleep(wait)
	}
}

// WaitingCount returns the number of goroutines currently blocked waiting for rate limits.
func (rl *DiscordRateLimiter) WaitingCount() int64 {
	return rl.waiting.Load()
}

// Update parses Discord rate limit response headers and updates the target's state.
//
// Headers used:
//   - X-RateLimit-Remaining: requests remaining in the current window
//   - X-RateLimit-Limit: total bucket size for the route
//   - X-RateLimit-Reset-After: seconds (float) until the window resets
//   - X-RateLimit-Global: if "true", the limit applies to the global bucket
func (rl *DiscordRateLimiter) Update(target string, headers http.Header) {
	remainingStr := headers.Get("X-RateLimit-Remaining")
	resetAfterStr := headers.Get("X-RateLimit-Reset-After")
	isGlobal := strings.EqualFold(headers.Get("X-RateLimit-Global"), "true")

	if isGlobal && resetAfterStr != "" {
		if secs, err := strconv.ParseFloat(resetAfterStr, 64); err == nil {
			d := time.Duration(secs*1000) * time.Millisecond
			rl.global.mu.Lock()
			rl.global.tokens = 0
			rl.global.lastRefill = time.Now().Add(d)
			rl.global.mu.Unlock()
		}
		return
	}

	if remainingStr == "" {
		return
	}

	remaining, err := strconv.Atoi(remainingStr)
	if err != nil {
		return
	}

	var limit int
	if limitStr := headers.Get("X-RateLimit-Limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}

	var resetAt time.Time
	if resetAfterStr != "" {
		if secs, err := strconv.ParseFloat(resetAfterStr, 64); err == nil {
			resetAt = time.Now().Add(time.Duration(secs*1000) * time.Millisecond)
		}
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	tl, exists := rl.targets[target]
	if !exists {
		tl = &targetLimit{}
		rl.targets[target] = tl
	}
	tl.remaining = remaining
	if limit > 0 {
		tl.limit = limit
	}
	tl.resetAt = resetAt
}

// now returns the current time, using the injected nowFunc when set (tests only).
func (rl *DiscordRateLimiter) now() time.Time {
	if rl.nowFunc != nil {
		return rl.nowFunc()
	}
	return time.Now()
}

// Record429 records a 429 response in the rolling 5-minute window counter.
// Safe to call from any goroutine.
func (rl *DiscordRateLimiter) Record429() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	// Use a 60-slot ring keyed by Unix minute, giving 60 minutes of history.
	// We only report the last 5 minutes, but extra history is free and allows
	// future widening without a schema change.
	currentMin := now.Unix() / 60
	slot := int(currentMin % 60)
	if rl.counter429Mins[slot] != currentMin {
		// Slot belongs to a different (older) minute — reset it.
		rl.counter429[slot] = 0
		rl.counter429Mins[slot] = currentMin
	}
	rl.counter429[slot]++
}

// recent429Count sums the 429 slots covering the last 5 minutes.
// Must be called with rl.mu held.
func (rl *DiscordRateLimiter) recent429Count() int {
	now := rl.now()
	currentMin := now.Unix() / 60
	count := 0
	for i := range 5 {
		targetMin := currentMin - int64(i)
		slot := int(targetMin % 60)
		if rl.counter429Mins[slot] == targetMin {
			count += int(rl.counter429[slot])
		}
	}
	return count
}

// Snapshot returns the current rate-limit state. Safe to call from any goroutine.
// The returned Routes slice is value-typed — no internal references escape.
func (rl *DiscordRateLimiter) Snapshot() DiscordRateSnapshot {
	rl.mu.Lock()
	var routes []RouteState
	for key, tl := range rl.targets {
		// Omit fully-quota'd routes (have limit info, no capacity consumed).
		if tl.limit > 0 && tl.remaining >= tl.limit {
			continue
		}
		// Omit routes with positive remaining but no limit info — not useful to surface.
		if tl.limit == 0 && tl.remaining > 0 {
			continue
		}
		// Everything else: partially-consumed (remaining < limit) or exhausted
		// without limit info (both 0). Include.
		routes = append(routes, RouteState{
			Route:     key,
			Remaining: tl.remaining,
			Limit:     tl.limit,
			ResetAt:   tl.resetAt,
		})
	}
	count429 := rl.recent429Count()
	rl.mu.Unlock()

	// Read global bucket without holding rl.mu (it has its own lock).
	rl.global.mu.Lock()
	rl.global.refill()
	globalTokens := int(rl.global.tokens)
	globalCap := int(rl.global.maxTokens)
	rl.global.mu.Unlock()

	return DiscordRateSnapshot{
		Routes:         routes,
		GlobalTokens:   globalTokens,
		GlobalCapacity: globalCap,
		Recent429Count: count429,
	}
}

// ParseRetryAfter converts a Retry-After value to a Duration using Dexter's heuristic:
// if the value is > 1000, treat it as milliseconds; otherwise treat it as seconds.
// A small random jitter (0-500ms) is added.
func ParseRetryAfter(retryAfter float64) time.Duration {
	var d time.Duration
	if retryAfter > 1000 {
		d = time.Duration(retryAfter) * time.Millisecond
	} else {
		d = time.Duration(retryAfter*1000) * time.Millisecond
	}
	jitter := time.Duration(rand.IntN(500)) * time.Millisecond
	return d + jitter
}
