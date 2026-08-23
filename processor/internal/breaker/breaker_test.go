package breaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func TestBreaker_RunsAndReturns(t *testing.T) {
	b := New[int](Config{Name: "t"})
	got, err := b.Do(func() (int, error) { return 42, nil })
	if err != nil || got != 42 {
		t.Fatalf("Do = (%d, %v), want (42, nil)", got, err)
	}
}

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	b := New[int](Config{Name: "t", FailureThreshold: 3, Cooldown: time.Minute})

	for n := range 3 {
		_, err := b.Do(func() (int, error) { calls.Add(1); return 0, errBoom })
		if !errors.Is(err, errBoom) {
			t.Fatalf("call %d: err = %v, want errBoom", n, err)
		}
	}
	// Circuit now open: fn must NOT run, ErrOpen returned.
	_, err := b.Do(func() (int, error) { calls.Add(1); return 0, errBoom })
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("after threshold: err = %v, want ErrOpen", err)
	}
	if c := calls.Load(); c != 3 {
		t.Errorf("fn ran %d times, want 3 (open circuit skips fn)", c)
	}
}

func TestBreaker_SuccessResetsConsecutive(t *testing.T) {
	b := New[int](Config{Name: "t", FailureThreshold: 3, Cooldown: time.Minute})
	// 2 failures, a success, then 2 more failures — should NOT open (consecutive reset).
	b.Do(func() (int, error) { return 0, errBoom })
	b.Do(func() (int, error) { return 0, errBoom })
	b.Do(func() (int, error) { return 1, nil })
	var ran int
	for n := range 2 {
		_, err := b.Do(func() (int, error) { ran++; return 0, errBoom })
		if errors.Is(err, ErrOpen) {
			t.Fatalf("call %d unexpectedly ErrOpen — consecutive count not reset", n)
		}
	}
	if ran != 2 {
		t.Errorf("fn ran %d times, want 2", ran)
	}
}

func TestBreaker_HalfOpenRecovers(t *testing.T) {
	b := New[int](Config{Name: "t", FailureThreshold: 2, Cooldown: 40 * time.Millisecond})
	b.Do(func() (int, error) { return 0, errBoom })
	b.Do(func() (int, error) { return 0, errBoom })
	if _, err := b.Do(func() (int, error) { return 0, nil }); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen while open, got %v", err)
	}
	time.Sleep(60 * time.Millisecond) // let it move to half-open
	got, err := b.Do(func() (int, error) { return 7, nil })
	if err != nil || got != 7 {
		t.Fatalf("half-open probe = (%d, %v), want (7, nil)", got, err)
	}
	// Closed again.
	if got, err := b.Do(func() (int, error) { return 9, nil }); err != nil || got != 9 {
		t.Fatalf("after recovery = (%d, %v), want (9, nil)", got, err)
	}
}

func TestBreaker_OnHealthChange(t *testing.T) {
	var mu sync.Mutex
	var transitions []bool
	b := New[int](Config{
		Name: "t", FailureThreshold: 2, Cooldown: 40 * time.Millisecond,
		OnHealthChange: func(healthy bool) {
			mu.Lock()
			transitions = append(transitions, healthy)
			mu.Unlock()
		},
	})
	b.Do(func() (int, error) { return 0, errBoom })
	b.Do(func() (int, error) { return 0, errBoom }) // opens → healthy=false
	time.Sleep(60 * time.Millisecond)
	b.Do(func() (int, error) { return 1, nil }) // half-open probe succeeds → closed → healthy=true

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) < 2 || transitions[0] != false || transitions[len(transitions)-1] != true {
		t.Errorf("transitions = %v, want first=false (open) then ...true (closed)", transitions)
	}
}

func TestBreaker_ConcurrencyLimits(t *testing.T) {
	const limit = 2
	b := New[int](Config{Name: "t", Concurrency: limit})
	var inFlight, maxSeen atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			b.Do(func() (int, error) {
				n := inFlight.Add(1)
				for {
					m := maxSeen.Load()
					if n <= m || maxSeen.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				inFlight.Add(-1)
				return 0, nil
			})
		})
	}
	wg.Wait()
	if m := maxSeen.Load(); m > limit {
		t.Errorf("max concurrent fn = %d, want <= %d", m, limit)
	}
}

func TestGate_OpensAndReturnsErrOpen(t *testing.T) {
	var calls atomic.Int32
	g := NewGate(Config{Name: "g", FailureThreshold: 2, Cooldown: time.Minute})
	for n := range 2 {
		if err := g.Do(func() error { calls.Add(1); return errBoom }); !errors.Is(err, errBoom) {
			t.Fatalf("call %d err = %v, want errBoom", n, err)
		}
	}
	if err := g.Do(func() error { calls.Add(1); return errBoom }); !errors.Is(err, ErrOpen) {
		t.Fatalf("after threshold err = %v, want ErrOpen", err)
	}
	if c := calls.Load(); c != 2 {
		t.Errorf("fn ran %d times, want 2 (open circuit skips fn)", c)
	}
}

func TestGate_ResultViaClosure(t *testing.T) {
	g := NewGate(Config{Name: "g"})
	var captured string
	err := g.Do(func() error { captured = "ok"; return nil })
	if err != nil || captured != "ok" {
		t.Fatalf("Do = %v, captured=%q; want nil, ok", err, captured)
	}
}

func TestBreaker_IsOpenReflectsState(t *testing.T) {
	b := New[int](Config{Name: "t", FailureThreshold: 2, Cooldown: time.Minute})
	if b.IsOpen() {
		t.Fatal("fresh breaker should not be open")
	}
	b.Do(func() (int, error) { return 0, errBoom })
	b.Do(func() (int, error) { return 0, errBoom })
	if !b.IsOpen() {
		t.Fatal("breaker should be open after threshold failures")
	}
}
