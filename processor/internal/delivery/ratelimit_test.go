package delivery

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimiterWaitNoLimit(t *testing.T) {
	rl := NewDiscordRateLimiter()

	start := time.Now()
	rl.Wait("user123")
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("Wait with no limit took %v, expected near-instant", elapsed)
	}
}

func TestRateLimiterWaitBlocked(t *testing.T) {
	rl := NewDiscordRateLimiter()

	// Manually set a target as exhausted with a short reset window.
	blockDuration := 100 * time.Millisecond
	rl.mu.Lock()
	rl.targets["user456"] = &targetLimit{
		remaining: 0,
		resetAt:   time.Now().Add(blockDuration),
	}
	rl.mu.Unlock()

	start := time.Now()
	rl.Wait("user456")
	elapsed := time.Since(start)

	if elapsed < 80*time.Millisecond {
		t.Errorf("Wait should have slept ~%v but only waited %v", blockDuration, elapsed)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

func TestRateLimiterUpdate(t *testing.T) {
	rl := NewDiscordRateLimiter()

	headers := http.Header{}
	headers.Set("X-RateLimit-Remaining", "3")
	headers.Set("X-RateLimit-Reset-After", "1.5")

	rl.Update("chan789", headers)

	rl.mu.Lock()
	tl, exists := rl.targets["chan789"]
	rl.mu.Unlock()

	if !exists {
		t.Fatal("target should exist after Update")
	}
	if tl.remaining != 3 {
		t.Errorf("remaining = %d, want 3", tl.remaining)
	}
	if tl.resetAt.IsZero() {
		t.Error("resetAt should be set")
	}
	// resetAt should be ~1.5 seconds from now
	untilReset := time.Until(tl.resetAt)
	if untilReset < 1*time.Second || untilReset > 2*time.Second {
		t.Errorf("resetAt is %v from now, expected ~1.5s", untilReset)
	}
}

func TestRateLimiterUpdateGlobal(t *testing.T) {
	rl := NewDiscordRateLimiter()

	headers := http.Header{}
	headers.Set("X-RateLimit-Global", "true")
	headers.Set("X-RateLimit-Reset-After", "0.5")

	rl.Update("any", headers)

	// Global bucket should be drained; tryConsume should fail immediately.
	rl.global.mu.Lock()
	tokens := rl.global.tokens
	rl.global.mu.Unlock()

	if tokens != 0 {
		t.Errorf("global tokens = %v, want 0 after global rate limit", tokens)
	}
}

func TestRateLimiterUpdateNoHeaders(t *testing.T) {
	rl := NewDiscordRateLimiter()

	// Empty headers should not create a target entry.
	rl.Update("ghost", http.Header{})

	rl.mu.Lock()
	_, exists := rl.targets["ghost"]
	rl.mu.Unlock()

	if exists {
		t.Error("target should not exist when no rate limit headers present")
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	d := ParseRetryAfter(2.5)
	// Should be ~2500ms + jitter (0-500ms), so between 2500ms and 3000ms.
	if d < 2500*time.Millisecond || d > 3100*time.Millisecond {
		t.Errorf("ParseRetryAfter(2.5) = %v, expected 2500-3000ms", d)
	}
}

func TestParseRetryAfterMilliseconds(t *testing.T) {
	d := ParseRetryAfter(5000)
	// > 1000 → treated as milliseconds: 5000ms + jitter (0-500ms)
	if d < 5000*time.Millisecond || d > 5600*time.Millisecond {
		t.Errorf("ParseRetryAfter(5000) = %v, expected 5000-5500ms", d)
	}
}

func TestParseRetryAfterBoundary(t *testing.T) {
	// Exactly 1000 should be treated as seconds (not > 1000).
	d := ParseRetryAfter(1000)
	// 1000 seconds = 1_000_000ms + jitter
	if d < 999*time.Second || d > 1001*time.Second {
		t.Errorf("ParseRetryAfter(1000) = %v, expected ~1000s", d)
	}
}

func TestTokenBucketConsume(t *testing.T) {
	tb := newTokenBucket(5, 10)

	// Should be able to consume 5 tokens immediately.
	for i := 0; i < 5; i++ {
		if !tb.tryConsume() {
			t.Errorf("tryConsume() #%d should succeed", i+1)
		}
	}

	// 6th should fail.
	if tb.tryConsume() {
		t.Error("tryConsume() should fail when bucket is empty")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := newTokenBucket(5, 100) // 100 tokens/sec for fast test

	// Drain the bucket.
	for i := 0; i < 5; i++ {
		tb.tryConsume()
	}
	if tb.tryConsume() {
		t.Fatal("bucket should be empty")
	}

	// Wait for refill (at 100/sec, ~10ms should give us at least 1 token).
	time.Sleep(20 * time.Millisecond)

	if !tb.tryConsume() {
		t.Error("tryConsume() should succeed after refill period")
	}
}

func TestTokenBucketTimeUntilAvailable(t *testing.T) {
	tb := newTokenBucket(2, 10)

	// With tokens available, should return 0.
	d := tb.timeUntilAvailable()
	if d != 0 {
		t.Errorf("timeUntilAvailable() = %v, want 0 when tokens available", d)
	}

	// Drain.
	tb.tryConsume()
	tb.tryConsume()

	d = tb.timeUntilAvailable()
	// At 10 tokens/sec, need ~100ms for 1 token.
	if d <= 0 || d > 200*time.Millisecond {
		t.Errorf("timeUntilAvailable() = %v, expected ~100ms", d)
	}
}
