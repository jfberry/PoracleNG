package ratelimit

import (
	"sync"
	"time"
)

// Config holds rate limiting configuration.
type Config struct {
	TimingPeriod        int            // window seconds (default 240)
	DMLimit             int            // messages per window for DM users (default 20)
	ChannelLimit        int            // messages per window for channels (default 40)
	MaxLimitsBeforeStop int            // violations in 24h before disable (default 10)
	Overrides           map[string]int // per-destination limit overrides
}

// RateResult is returned by Check.
type RateResult struct {
	Allowed      bool // message is under the limit
	JustBreached bool // this call is the first to exceed the limit (send notification)
	Banned       bool // user has exceeded max violations in 24h (disable user)
	Limit        int  // the applicable limit
	ResetSeconds int  // seconds until the current window expires
}

type counter struct {
	count    int
	windowAt time.Time // when this window started
}

type violation struct {
	count    int
	windowAt time.Time // when 24h violation tracking started
}

// Limiter tracks per-destination message counts and violations.
type Limiter struct {
	mu         sync.Mutex
	counters   map[string]*counter
	violations map[string]*violation
	cfg        Config
	done       chan struct{}
}

// New creates a new rate limiter. Pass a zero Config for defaults.
func New(cfg Config) *Limiter {
	if cfg.TimingPeriod <= 0 {
		cfg.TimingPeriod = 240
	}
	if cfg.DMLimit <= 0 {
		cfg.DMLimit = 20
	}
	if cfg.ChannelLimit <= 0 {
		cfg.ChannelLimit = 40
	}
	if cfg.MaxLimitsBeforeStop <= 0 {
		cfg.MaxLimitsBeforeStop = 10
	}
	if cfg.Overrides == nil {
		cfg.Overrides = make(map[string]int)
	}

	l := &Limiter{
		counters:   make(map[string]*counter),
		violations: make(map[string]*violation),
		cfg:        cfg,
		done:       make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Close stops the cleanup goroutine.
func (l *Limiter) Close() {
	close(l.done)
}

// Check increments the message counter for the given destination and returns
// whether the message should be sent.
func (l *Limiter) Check(destinationID, destinationType string) RateResult {
	limit := l.limitFor(destinationID, destinationType)
	windowDuration := time.Duration(l.cfg.TimingPeriod) * time.Second
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	c := l.counters[destinationID]
	if c == nil || now.Sub(c.windowAt) >= windowDuration {
		// Start new window
		c = &counter{count: 0, windowAt: now}
		l.counters[destinationID] = c
	}

	c.count++
	resetSeconds := max(int(windowDuration.Seconds()-now.Sub(c.windowAt).Seconds()), 1)

	result := RateResult{
		Limit:        limit,
		ResetSeconds: resetSeconds,
	}

	if c.count <= limit {
		result.Allowed = true
		return result
	}

	// Over the limit — only the first message past the limit triggers JustBreached.
	// This ensures exactly one notification per window and one violation increment
	// per window, preventing notification spam while still tracking repeated offences.
	if c.count == limit+1 {
		result.JustBreached = true
		result.Banned = l.incrementViolation(destinationID, now)
	}

	return result
}

// limitFor returns the applicable message limit for a destination.
func (l *Limiter) limitFor(destinationID, destinationType string) int {
	if override, ok := l.cfg.Overrides[destinationID]; ok {
		return override
	}
	if isUserType(destinationType) {
		return l.cfg.DMLimit
	}
	return l.cfg.ChannelLimit
}

// incrementViolation tracks 24h violations. Returns true if user should be banned.
// Must be called with l.mu held.
func (l *Limiter) incrementViolation(destinationID string, now time.Time) bool {
	v := l.violations[destinationID]
	if v == nil || now.Sub(v.windowAt) >= 24*time.Hour {
		v = &violation{count: 0, windowAt: now}
		l.violations[destinationID] = v
	}
	v.count++
	return v.count >= l.cfg.MaxLimitsBeforeStop
}

// isUserType returns true for destination types that should use the DM limit.
// All other types (discord:channel, telegram:channel, telegram:group, webhook)
// use the channel limit — they are multi-user destinations where higher
// throughput is expected.
func isUserType(t string) bool {
	return t == "discord:user" || t == "telegram:user"
}

// cleanupLoop removes expired counters and violations periodically.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.done:
			return
		}
	}
}

func (l *Limiter) cleanup() {
	now := time.Now()
	windowDuration := time.Duration(l.cfg.TimingPeriod) * time.Second

	l.mu.Lock()
	defer l.mu.Unlock()

	for id, c := range l.counters {
		if now.Sub(c.windowAt) >= windowDuration {
			delete(l.counters, id)
		}
	}
	for id, v := range l.violations {
		if now.Sub(v.windowAt) >= 24*time.Hour {
			delete(l.violations, id)
		}
	}
}
