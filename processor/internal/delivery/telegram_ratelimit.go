package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

const (
	defaultTelegramGlobalRatePerSecond = 29
	defaultTelegramGlobalBurst         = 5
)

// TelegramRateLimitConfig controls proactive Telegram message-send limiting.
// Zero values use the safe defaults so programmatic/test callers that predate
// the config remain compatible. Production TOML validation rejects explicit
// non-positive values before this configuration reaches the sender.
type TelegramRateLimitConfig struct {
	GlobalRatePerSecond int
	GlobalBurst         int
}

// TelegramSendRateLimiter applies one global token bucket to outbound Telegram
// message sends for a bot token. It intentionally does not rate-limit reads,
// edits, or deletes: Telegram documents the ~30/sec free limit as a
// message/broadcast limit.
type TelegramSendRateLimiter struct {
	global *tokenBucket
}

func defaultTelegramSendRateLimiter() *TelegramSendRateLimiter {
	limiter, err := NewTelegramSendRateLimiter(TelegramRateLimitConfig{})
	if err != nil {
		panic(fmt.Sprintf("invalid built-in Telegram rate-limit defaults: %v", err))
	}
	return limiter
}

// NewTelegramSendRateLimiter constructs a proactive global send limiter that
// can be shared by every outbound message path using one Telegram bot token.
func NewTelegramSendRateLimiter(cfg TelegramRateLimitConfig) (*TelegramSendRateLimiter, error) {
	cfg, err := normalizeTelegramRateLimitConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &TelegramSendRateLimiter{
		global: newTokenBucket(float64(cfg.GlobalBurst), float64(cfg.GlobalRatePerSecond)),
	}, nil
}

func normalizeTelegramRateLimitConfig(cfg TelegramRateLimitConfig) (TelegramRateLimitConfig, error) {
	if cfg.GlobalRatePerSecond == 0 {
		cfg.GlobalRatePerSecond = defaultTelegramGlobalRatePerSecond
	}
	if cfg.GlobalBurst == 0 {
		cfg.GlobalBurst = defaultTelegramGlobalBurst
	}
	if cfg.GlobalRatePerSecond < 0 {
		return cfg, fmt.Errorf("telegram global rate per second must be positive (got %d)", cfg.GlobalRatePerSecond)
	}
	if cfg.GlobalBurst < 0 {
		return cfg, fmt.Errorf("telegram global burst must be positive (got %d)", cfg.GlobalBurst)
	}
	return cfg, nil
}

// Wait consumes one global send token, blocking until it is available. It is
// used by Telegram paths that do not have the delivery sender's HTTP
// concurrency semaphore.
func (r *TelegramSendRateLimiter) Wait(ctx context.Context) error {
	release, err := r.WaitForPermit(ctx, func(context.Context) (func(), error) {
		return func() {}, nil
	})
	if release != nil {
		release()
	}
	return err
}

// WaitForPermit waits for global send quota, then acquires gate and re-checks
// the quota before consuming a token. TelegramSender uses gate for its HTTP
// concurrency semaphore.
//
// Tokens are deliberately not consumed before gate is acquired: otherwise a
// slow in-flight request could let goroutines pre-consume future tokens while
// queued on the semaphore, then burst onto the wire later. If another
// goroutine takes the token while this call waits for gate, gate is released
// immediately and the limiter retries. Limiter sleeps never hold gate or the
// token-bucket mutex.
func (r *TelegramSendRateLimiter) WaitForPermit(
	ctx context.Context,
	gate func(context.Context) (func(), error),
) (func(), error) {
	if gate == nil {
		gate = func(context.Context) (func(), error) { return func() {}, nil }
	}

	var waited time.Duration
	delayed := false
	defer func() {
		if delayed {
			metrics.DeliveryRateLimitWait.WithLabelValues("telegram").Observe(waited.Seconds())
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if waitFor := r.global.timeUntilAvailable(); waitFor > 0 {
			delayed = true
			elapsed, err := waitTelegramRateDelay(ctx, waitFor)
			waited += elapsed
			if err != nil {
				return nil, err
			}
			continue
		}

		releaseGate, err := gate(ctx)
		if err != nil {
			return nil, err
		}
		if releaseGate == nil {
			releaseGate = func() {}
		}
		if err := ctx.Err(); err != nil {
			releaseGate()
			return nil, err
		}

		// Availability may have changed while waiting for the HTTP slot.
		// Consume only while gate is held; otherwise release it and retry.
		if r.global.tryConsume() {
			return releaseGate, nil
		}
		releaseGate()
		delayed = true

		// tokenBucket.timeUntilAvailable truncates to milliseconds and can
		// therefore report zero for a sub-millisecond deficit. Avoid spinning
		// on the semaphore until that fractional token refills.
		elapsed, err := waitTelegramRateDelay(ctx, time.Millisecond)
		waited += elapsed
		if err != nil {
			return nil, err
		}
	}
}

func waitTelegramRateDelay(ctx context.Context, delay time.Duration) (time.Duration, error) {
	started := time.Now()
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return time.Since(started), ctx.Err()
	case <-timer.C:
		return time.Since(started), nil
	}
}
