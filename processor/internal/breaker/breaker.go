// Package breaker wraps github.com/sony/gobreaker with this project's
// conventions: a consecutive-failure circuit breaker (default 5 failures /
// 30s cooldown / single half-open probe), an optional concurrency limiter,
// and a health-gauge hook. It is the shared replacement for the per-component
// hand-rolled breakers that previously lived in geocoder, staticmap, and
// intersection — one tested implementation instead of three copies.
package breaker

import (
	"errors"
	"time"

	"github.com/sony/gobreaker/v2"
)

// Defaults match the previous hand-rolled breakers so behaviour is unchanged
// across the migration.
const (
	DefaultFailureThreshold = 5
	DefaultCooldown         = 30 * time.Second
)

// ErrOpen is returned by Do when the breaker rejects a call without running it
// (the circuit is open, or the half-open probe budget is spent). Callers use
// errors.Is(err, ErrOpen) to count "circuit break" events separately from real
// call failures.
var ErrOpen = errors.New("circuit breaker open")

// Config configures a Breaker.
type Config struct {
	// Name identifies the breaker in logs/metrics.
	Name string
	// FailureThreshold is the number of consecutive failures that opens the
	// circuit. <= 0 uses DefaultFailureThreshold.
	FailureThreshold int
	// Cooldown is how long the circuit stays open before allowing a single
	// half-open probe. <= 0 uses DefaultCooldown.
	Cooldown time.Duration
	// Concurrency bounds simultaneous in-flight calls. <= 0 means unlimited.
	Concurrency int
	// OnHealthChange fires on state transitions: false when the circuit opens,
	// true when it closes. Drives a health gauge. Nil disables.
	OnHealthChange func(healthy bool)
}

// Breaker guards calls to an external dependency that returns a T.
type Breaker[T any] struct {
	cb  *gobreaker.CircuitBreaker[T]
	sem chan struct{}
}

// New builds a Breaker from cfg.
func New[T any](cfg Config) *Breaker[T] {
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = DefaultFailureThreshold
	}
	cooldown := cfg.Cooldown
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}

	st := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: 1, // single half-open probe, matching the old breakers
		Timeout:     cooldown,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= uint32(threshold)
		},
	}
	if cfg.OnHealthChange != nil {
		onHealth := cfg.OnHealthChange
		st.OnStateChange = func(_ string, _, to gobreaker.State) {
			switch to {
			case gobreaker.StateClosed:
				onHealth(true)
			case gobreaker.StateOpen:
				onHealth(false)
			}
		}
	}

	var sem chan struct{}
	if cfg.Concurrency > 0 {
		sem = make(chan struct{}, cfg.Concurrency)
	}

	return &Breaker[T]{
		cb:  gobreaker.NewCircuitBreaker[T](st),
		sem: sem,
	}
}

// Do runs fn if the breaker permits it, honouring the concurrency limit. When
// the circuit is open (or the half-open probe budget is spent) fn is NOT run
// and Do returns the zero T and ErrOpen. Otherwise it returns fn's result; a
// non-nil error from fn counts toward tripping the breaker unless IsSuccessful
// classifies it as a success.
//
// The concurrency limiter sits inside the breaker gate, so a rejected call
// never acquires a slot.
func (b *Breaker[T]) Do(fn func() (T, error)) (T, error) {
	res, err := b.cb.Execute(func() (T, error) {
		if b.sem != nil {
			b.sem <- struct{}{}
			defer func() { <-b.sem }()
		}
		return fn()
	})
	if err != nil && (errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)) {
		var zero T
		return zero, ErrOpen
	}
	return res, err
}

// State returns the current breaker state ("closed", "half-open", or "open")
// for diagnostics.
func (b *Breaker[T]) State() string {
	return b.cb.State().String()
}

// IsOpen reports whether the circuit is fully open, so callers can cheaply
// skip expensive request preparation (marshaling, key selection) that Do would
// then throw away. Half-open returns false so the single probe still proceeds.
func (b *Breaker[T]) IsOpen() bool {
	return b.cb.State() == gobreaker.StateOpen
}

// Gate is a circuit breaker for calls whose result the caller handles itself
// (typically via a closure), so only success/failure matters to the breaker.
// It's the right choice when one breaker guards several call sites with
// different return types — e.g. a tile server that both pregenerates a URL and
// fetches bytes. Build with NewGate.
type Gate struct {
	b *Breaker[struct{}]
}

// NewGate builds a Gate from cfg.
func NewGate(cfg Config) *Gate {
	return &Gate{b: New[struct{}](cfg)}
}

// Do runs fn if the breaker permits it (honouring the concurrency limit) and
// returns fn's error. When the circuit is open it returns ErrOpen without
// running fn. A non-nil error from fn counts toward tripping the breaker
// unless IsSuccessful classifies it as a success.
func (g *Gate) Do(fn func() error) error {
	_, err := g.b.Do(func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// State returns the current breaker state for diagnostics.
func (g *Gate) State() string {
	return g.b.State()
}

// IsOpen reports whether the circuit is fully open (see Breaker.IsOpen).
func (g *Gate) IsOpen() bool {
	return g.b.IsOpen()
}
