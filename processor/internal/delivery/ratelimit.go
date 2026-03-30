package delivery

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// DiscordRateLimiter tracks per-destination rate limits and a global 50 req/sec token bucket.
type DiscordRateLimiter struct {
	mu      sync.Mutex
	targets map[string]*targetLimit
	global  *tokenBucket
}

type targetLimit struct {
	remaining int
	resetAt   time.Time
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
			time.Sleep(d)
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

// Update parses Discord rate limit response headers and updates the target's state.
//
// Headers used:
//   - X-RateLimit-Remaining: requests remaining in the current window
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
	tl.resetAt = resetAt
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
