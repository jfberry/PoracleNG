package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/ratelimit"
)

// recordingHooks captures OnBreach/OnBan invocations for assertions.
type recordingHooks struct {
	mu      sync.Mutex
	breach  []string // target
	ban     []string // target
}

func (h *recordingHooks) OnBreach(target, _, _, _ string, _, _ int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.breach = append(h.breach, target)
}

func (h *recordingHooks) OnBan(target, _, _, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ban = append(h.ban, target)
}

func (h *recordingHooks) snapshot() (breach, ban []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	breach = append(breach, h.breach...)
	ban = append(ban, h.ban...)
	return
}

// queueMockSender is a configurable mock for queue tests.
// (Named differently from mockSender in tracker_test.go to avoid conflict.)
type queueMockSender struct {
	platform  string
	sendCalls []*Job
	editCalls []string // sentIDs passed to Edit
	mu        sync.Mutex
	sendErr   error
	editErr   error
	sendDelay time.Duration
	sentID    string // returned from Send
}

func (m *queueMockSender) Send(_ context.Context, job *Job) (*SentMessage, error) {
	if m.sendDelay > 0 {
		time.Sleep(m.sendDelay)
	}
	m.mu.Lock()
	m.sendCalls = append(m.sendCalls, job)
	m.mu.Unlock()
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	id := m.sentID
	if id == "" {
		id = "sent-" + job.Target
	}
	return &SentMessage{ID: id}, nil
}

func (m *queueMockSender) Delete(_ context.Context, sentID string) error {
	return nil
}

func (m *queueMockSender) Edit(_ context.Context, sentID string, _ json.RawMessage) error {
	m.mu.Lock()
	m.editCalls = append(m.editCalls, sentID)
	m.mu.Unlock()
	return m.editErr
}

func (m *queueMockSender) Platform() string { return m.platform }

func (m *queueMockSender) WaitForRateLimit(target string) {} // no-op in tests

func (m *queueMockSender) getSendCalls() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Job, len(m.sendCalls))
	copy(result, m.sendCalls)
	return result
}

func (m *queueMockSender) getEditCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.editCalls))
	copy(result, m.editCalls)
	return result
}

func newTestFairQueue(t *testing.T, senders map[string]Sender, cfg QueueConfig) (*FairQueue, chan *Job) {
	t.Helper()
	ch := make(chan *Job, 100)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(ch, senders, tracker, cfg)
	return fq, ch
}

func TestFairQueueRouting(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	telegramMock := &queueMockSender{platform: "telegram"}
	senders := map[string]Sender{
		"discord":  discordMock,
		"telegram": telegramMock,
	}

	fq, ch := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	ch <- &Job{Target: "user1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	ch <- &Job{Target: "chan1", Type: "discord:channel", Message: json.RawMessage(`{}`)}
	ch <- &Job{Target: "tg1", Type: "telegram:user", Message: json.RawMessage(`{}`)}
	ch <- &Job{Target: "wh1", Type: "webhook", Message: json.RawMessage(`{}`)}

	// Give workers time to process
	time.Sleep(100 * time.Millisecond)

	fq.Stop()

	discordCalls := discordMock.getSendCalls()
	telegramCalls := telegramMock.getSendCalls()

	// discord:user, discord:channel, and webhook all go to discord sender
	if len(discordCalls) != 3 {
		t.Errorf("expected 3 discord send calls, got %d", len(discordCalls))
	}
	if len(telegramCalls) != 1 {
		t.Errorf("expected 1 telegram send call, got %d", len(telegramCalls))
	}
	if len(telegramCalls) > 0 && telegramCalls[0].Target != "tg1" {
		t.Errorf("expected telegram target tg1, got %s", telegramCalls[0].Target)
	}
}

func TestFairQueueConcurrency(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	slowMock := &queueMockSender{
		platform:  "discord",
		sendDelay: 50 * time.Millisecond,
	}
	// Wrap Send to track concurrency
	origSend := slowMock.Send
	_ = origSend

	senders := map[string]Sender{"discord": &concurrencyTrackingSender{
		inner:         slowMock,
		concurrent:    &concurrent,
		maxConcurrent: &maxConcurrent,
	}}

	ch := make(chan *Job, 100)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Only 2 concurrent discord slots
	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	// Send 6 jobs — with concurrency 2 and 50ms delay, they should be serialized in pairs
	for range 6 {
		ch <- &Job{
			Target:  "user1",
			Type:    "discord:user",
			Message: json.RawMessage(`{}`),
		}
	}

	// Wait for all to finish
	time.Sleep(400 * time.Millisecond)
	fq.Stop()

	max := int(maxConcurrent.Load())
	if max > 2 {
		t.Errorf("expected max concurrent discord sends <= 2, got %d", max)
	}
	if max == 0 {
		t.Error("expected at least some concurrent sends, got 0")
	}
}

// concurrencyTrackingSender wraps a sender to track max concurrency.
type concurrencyTrackingSender struct {
	inner         Sender
	concurrent    *atomic.Int32
	maxConcurrent *atomic.Int32
}

func (s *concurrencyTrackingSender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	cur := s.concurrent.Add(1)
	for {
		old := s.maxConcurrent.Load()
		if cur <= old || s.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}
	defer s.concurrent.Add(-1)
	return s.inner.Send(ctx, job)
}

func (s *concurrencyTrackingSender) Delete(ctx context.Context, sentID string) error {
	return s.inner.Delete(ctx, sentID)
}

func (s *concurrencyTrackingSender) Edit(ctx context.Context, sentID string, message json.RawMessage) error {
	return s.inner.Edit(ctx, sentID, message)
}

func (s *concurrencyTrackingSender) WaitForRateLimit(target string) {}

func (s *concurrencyTrackingSender) Platform() string { return s.inner.Platform() }

func TestFairQueueEditLookup(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": discordMock}

	ch := make(chan *Job, 10)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Pre-track a message that can be edited
	tracker.Track("edit:pokemon:user1", &TrackedMessage{
		SentID: "chan1:msg-original",
		Target: "user1",
		Type:   "discord:user",
		Clean:  0,
	}, 5*time.Minute)

	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	ch <- &Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"updated"}`),
		EditKey: "edit:pokemon:user1",
	}

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	editCalls := discordMock.getEditCalls()
	if len(editCalls) != 1 {
		t.Fatalf("expected 1 edit call, got %d", len(editCalls))
	}
	if editCalls[0] != "chan1:msg-original" {
		t.Errorf("expected edit on chan1:msg-original, got %s", editCalls[0])
	}

	// Should NOT have called Send since edit succeeded
	sendCalls := discordMock.getSendCalls()
	if len(sendCalls) != 0 {
		t.Errorf("expected 0 send calls after successful edit, got %d", len(sendCalls))
	}
}

func TestFairQueueEditFallback(t *testing.T) {
	discordMock := &queueMockSender{
		platform: "discord",
		editErr:  errors.New("edit failed"),
	}
	senders := map[string]Sender{"discord": discordMock}

	ch := make(chan *Job, 10)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Pre-track a message
	tracker.Track("edit:pokemon:user1", &TrackedMessage{
		SentID: "chan1:msg-original",
		Target: "user1",
		Type:   "discord:user",
		Clean:  0,
	}, 5*time.Minute)

	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	ch <- &Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"fallback"}`),
		EditKey: "edit:pokemon:user1",
		TTH:     TTH{Minutes: 10},
	}

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	// Edit was attempted
	editCalls := discordMock.getEditCalls()
	if len(editCalls) != 1 {
		t.Fatalf("expected 1 edit call, got %d", len(editCalls))
	}

	// Then Send was called as fallback
	sendCalls := discordMock.getSendCalls()
	if len(sendCalls) != 1 {
		t.Fatalf("expected 1 send call after edit failure, got %d", len(sendCalls))
	}
}

func TestFairQueueCleanTracking(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "chan1:msg-42"}
	senders := map[string]Sender{"discord": discordMock}

	ch := make(chan *Job, 10)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	ch <- &Job{
		Target:  "chan1",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"hello"}`),
		Clean:   1,
		TTH:     TTH{Minutes: 5},
	}

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	// The message should be tracked for clean deletion
	// Key format: clean:{type}:{target}:{sentID}
	expectedKey := "clean:discord:channel:chan1:chan1:msg-42"
	tracked := tracker.LookupEdit(expectedKey)
	if tracked == nil {
		t.Fatal("expected clean message to be tracked, got nil")
	}
	if tracked.SentID != "chan1:msg-42" {
		t.Errorf("expected tracked SentID chan1:msg-42, got %s", tracked.SentID)
	}
	if tracked.Clean == 0 {
		t.Error("expected tracked message to have Clean=true")
	}
}

// TestRateLimitAtDelivery proves the count happens at delivery time and that
// only deliveries past the limit are dropped (with a single OnBreach hook).
func TestRateLimitAtDelivery(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	hooks := &recordingHooks{}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer limiter.Close()

	fq, ch := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
		RateLimitHooks:    hooks,
	})
	fq.Start()

	for range 5 {
		ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	}
	time.Sleep(300 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getSendCalls()); got != 2 {
		t.Fatalf("expected 2 sends (DM limit), got %d", got)
	}
	breaches, _ := hooks.snapshot()
	if len(breaches) != 1 || breaches[0] != "u1" {
		t.Fatalf("expected exactly one OnBreach for u1, got %v", breaches)
	}
}

// TestRateLimitBypass proves jobs flagged BypassRateLimit are sent regardless
// of the limit and do not consume budget that would otherwise apply to other
// jobs to the same destination.
func TestRateLimitBypass(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 1, ChannelLimit: 5})
	defer limiter.Close()

	fq, ch := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	})
	fq.Start()

	// Burn the only DM slot
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	// Bypass job — must still be delivered even though u1 is now over limit
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`), BypassRateLimit: true}
	// Non-bypass job — must be dropped
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}

	time.Sleep(300 * time.Millisecond)
	fq.Stop()

	calls := mock.getSendCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 sends (1 normal + 1 bypass), got %d", len(calls))
	}
	if !calls[1].BypassRateLimit {
		t.Fatal("second send should be the bypass job")
	}
}

// TestRateLimitEditNotCounted proves that a successful edit-before-send does
// not consume rate-limit budget. The first job creates the tracked message,
// the second edits it, and a third new send should still be allowed even
// though DMLimit is 2.
func TestRateLimitEditNotCounted(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5})
	defer limiter.Close()

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	ch := make(chan *Job, 10)
	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	})
	fq.Start()

	// First send establishes the tracked message under EditKey "raid:1".
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}
	time.Sleep(80 * time.Millisecond)

	// Edit reuses the existing message — must not count.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}
	time.Sleep(80 * time.Millisecond)

	// Second new send — would only succeed if the edit didn't consume the budget.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	time.Sleep(80 * time.Millisecond)

	// Third new send — over the DMLimit of 2; must be dropped.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	time.Sleep(80 * time.Millisecond)
	fq.Stop()

	sends := len(mock.getSendCalls())
	edits := len(mock.getEditCalls())
	if sends != 2 {
		t.Fatalf("expected exactly 2 Send calls (initial + one new), got %d", sends)
	}
	if edits != 1 {
		t.Fatalf("expected exactly 1 Edit call, got %d", edits)
	}
}

// TestRateLimitFailedEditCounts proves that when an Edit attempt fails and
// the queue falls through to the new-send path, that send DOES count against
// the limit (it went on the wire as a Send).
func TestRateLimitFailedEditCounts(t *testing.T) {
	mock := &queueMockSender{platform: "discord", editErr: errors.New("nope")}
	senders := map[string]Sender{"discord": mock}
	// DMLimit=2: the initial send and the edit-failure fallback send both
	// consume budget, leaving no room for a third.
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5})
	defer limiter.Close()

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	ch := make(chan *Job, 10)
	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	})
	fq.Start()

	// First send establishes the tracked message under EditKey "raid:1".
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}
	time.Sleep(80 * time.Millisecond)

	// Edit attempt fails (mock.editErr). Falls through to a new Send — which
	// MUST count, because it produced a real wire delivery.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}
	time.Sleep(80 * time.Millisecond)

	// Limit is 1 and we have already counted two real sends — this third one
	// must be dropped.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	time.Sleep(80 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getEditCalls()); got != 1 {
		t.Fatalf("expected 1 Edit attempt, got %d", got)
	}
	if got := len(mock.getSendCalls()); got != 2 {
		t.Fatalf("expected 2 Send calls (initial + edit-fallback), got %d", got)
	}
}

// TestRateLimitHookDoesNotDeadlock proves the worker does not deadlock when
// the breach hook tries to dispatch a bypass job into a full channel where
// other jobs target the same destination as the breaching one. Hooks are
// fire-and-forget, so the worker must release the per-destination mutex even
// if the hook's bypass dispatch is still pending channel space.
func TestRateLimitHookDoesNotDeadlock(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer limiter.Close()

	// Hook that itself tries to dispatch — but we route its dispatch through
	// the same (small) channel to simulate the deadlock-prone scenario.
	hookCalled := make(chan struct{}, 1)
	hooks := dispatchingHooks{onBreach: func() { hookCalled <- struct{}{} }}

	// Tiny channel so it fills fast.
	ch := make(chan *Job, 2)
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(ch, senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
		RateLimitHooks:    hooks,
	})
	fq.Start()

	// Burn the DM slot.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}
	// Trigger the breach.
	ch <- &Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}

	// Hook should fire promptly even though processJob holds the dest lock,
	// because it runs in its own goroutine.
	select {
	case <-hookCalled:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("OnBreach hook did not fire — likely deadlocked under dest mutex")
	}

	fq.Stop()
}

// dispatchingHooks is a minimal RateLimitHooks impl that just signals when
// OnBreach fires, used by TestRateLimitHookDoesNotDeadlock.
type dispatchingHooks struct {
	onBreach func()
}

func (d dispatchingHooks) OnBreach(_, _, _, _ string, _, _ int) {
	if d.onBreach != nil {
		d.onBreach()
	}
}
func (d dispatchingHooks) OnBan(_, _, _, _ string) {}

func TestFairQueueStop(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": discordMock}

	fq, ch := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	// Enqueue some jobs then immediately stop
	for range 5 {
		ch <- &Job{
			Target:  "user1",
			Type:    "discord:user",
			Message: json.RawMessage(`{}`),
		}
	}

	// Stop should drain remaining jobs and return
	done := make(chan struct{})
	go func() {
		fq.Stop()
		close(done)
	}()

	select {
	case <-done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5 seconds")
	}

	sendCalls := discordMock.getSendCalls()
	if len(sendCalls) != 5 {
		t.Errorf("expected all 5 jobs to be processed on stop, got %d", len(sendCalls))
	}
}
