package ratelimit

import (
	"sync"
	"time"
)

// Config holds rate limiting configuration.
type Config struct {
	TimingPeriod        int            // window seconds (default 240)
	DMLimit             int            // alert messages per window for DM users (default 20)
	ChannelLimit        int            // alert messages per window for channels (default 40)
	DMSummaryLimit      int            // summary dispatches per window for DM users (default 10)
	ChannelSummaryLimit int            // summary dispatches per window for channels (default 40)
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
// summaryCounters is a separate bucket so the summary cap (one
// dispatch per call) doesn't compete with the alert cap (one message
// per call).
type Limiter struct {
	mu              sync.Mutex
	counters        map[string]*counter
	summaryCounters map[string]*counter
	violations      map[string]*violation
	cfg             Config
	done            chan struct{}
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
	// Summary buckets mirror the alert-bucket DM/Channel split. The
	// caps exist as a backstop against pathological cases like a user
	// opting all their tracked rewards into summary mode on a frequent
	// schedule; in normal use a few digests a window is plenty.
	if cfg.DMSummaryLimit <= 0 {
		cfg.DMSummaryLimit = 10
	}
	if cfg.ChannelSummaryLimit <= 0 {
		cfg.ChannelSummaryLimit = 40
	}
	if cfg.MaxLimitsBeforeStop <= 0 {
		cfg.MaxLimitsBeforeStop = 10
	}
	if cfg.Overrides == nil {
		cfg.Overrides = make(map[string]int)
	}

	l := &Limiter{
		counters:        make(map[string]*counter),
		summaryCounters: make(map[string]*counter),
		violations:      make(map[string]*violation),
		cfg:             cfg,
		done:            make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Close stops the cleanup goroutine.
func (l *Limiter) Close() {
	close(l.done)
}

// IsBlocked reports whether the destination is currently over its limit
// within the live window. It is non-mutating: no counter increment, no
// violation tracking, no notification side effects. Use this at match
// time as a cheap pre-filter to skip render work for paused destinations.
// The authoritative count and breach detection happens in Check, which
// is called at delivery time.
func (l *Limiter) IsBlocked(destinationID, destinationType string) bool {
	limit := l.limitFor(destinationID, destinationType)
	windowDuration := time.Duration(l.cfg.TimingPeriod) * time.Second
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	c := l.counters[destinationID]
	if c == nil {
		return false
	}
	if now.Sub(c.windowAt) >= windowDuration {
		return false
	}
	return c.count >= limit
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

// CheckSummary increments the summary-dispatch counter for the given
// destination (1 per fire — chunking doesn't multiply the cost). The
// summary bucket is separate from the alert bucket so the two cap
// independently: a destination near its alert limit can still receive
// scheduled summaries, and a user opting many rules into summary mode
// can't blow past the alert cap with digest messages. DM and channel
// destinations have separate summary limits, mirroring the alert
// bucket's DM/Channel split (channels generally tolerate more
// throughput than individual users).
//
// Banned is intentionally not set here — opting into summary mode
// shouldn't escalate to auto-disable. The breach hook fires once per
// window to tell the destination their digest was dropped.
func (l *Limiter) CheckSummary(destinationID, destinationType string) RateResult {
	limit := l.summaryLimitFor(destinationType)
	windowDuration := time.Duration(l.cfg.TimingPeriod) * time.Second
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	c := l.summaryCounters[destinationID]
	if c == nil || now.Sub(c.windowAt) >= windowDuration {
		c = &counter{count: 0, windowAt: now}
		l.summaryCounters[destinationID] = c
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

	if c.count == limit+1 {
		result.JustBreached = true
	}
	return result
}

// summaryLimitFor returns the applicable summary limit for a
// destination type. Mirrors limitFor but for the summary bucket;
// note that per-destination Overrides apply only to the alert bucket
// (operators wanting per-user summary overrides can request a
// follow-up — no current use case).
func (l *Limiter) summaryLimitFor(destinationType string) int {
	if isUserType(destinationType) {
		return l.cfg.DMSummaryLimit
	}
	return l.cfg.ChannelSummaryLimit
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
	for id, c := range l.summaryCounters {
		if now.Sub(c.windowAt) >= windowDuration {
			delete(l.summaryCounters, id)
		}
	}
	for id, v := range l.violations {
		if now.Sub(v.windowAt) >= 24*time.Hour {
			delete(l.violations, id)
		}
	}
}
