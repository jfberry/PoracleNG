package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/ratelimit"
)

// recordingHooks captures OnBreach/OnBan invocations for assertions.
type recordingHooks struct {
	mu     sync.Mutex
	breach []string // target
	ban    []string // target
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

func (m *queueMockSender) Edit(_ context.Context, sentID string, _ json.RawMessage, _ []byte) error {
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

func newTestFairQueue(t *testing.T, senders map[string]Sender, cfg QueueConfig) (*FairQueue, func(*Job)) {
	t.Helper()
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(senders, tracker, cfg, nil)
	return fq, func(j *Job) { fq.enqueue(j, true) }
}

func TestFairQueueRouting(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	telegramMock := &queueMockSender{platform: "telegram"}
	senders := map[string]Sender{
		"discord":  discordMock,
		"telegram": telegramMock,
	}

	fq, enq := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	enq(&Job{Target: "user1", Type: "discord:user", Message: json.RawMessage(`{}`)})
	enq(&Job{Target: "chan1", Type: "discord:channel", Message: json.RawMessage(`{}`)})
	enq(&Job{Target: "tg1", Type: "telegram:user", Message: json.RawMessage(`{}`)})
	enq(&Job{Target: "wh1", Type: "webhook", Message: json.RawMessage(`{}`)})

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

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Only 2 concurrent discord slots
	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	// Send 6 jobs — all to the same target, so the lane drainer serializes
	// them regardless of platform concurrency; see the relaxed assertions
	// below (max <= 2, max != 0).
	for range 6 {
		fq.enqueue(&Job{
			Target:  "user1",
			Type:    "discord:user",
			Message: json.RawMessage(`{}`),
		}, true)
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

func (s *concurrencyTrackingSender) Edit(ctx context.Context, sentID string, message json.RawMessage, _ []byte) error {
	return s.inner.Edit(ctx, sentID, message, nil)
}

func (s *concurrencyTrackingSender) WaitForRateLimit(target string) {}

func (s *concurrencyTrackingSender) Platform() string { return s.inner.Platform() }

func TestFairQueueEditLookup(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Pre-track a message that can be edited
	tracker.Track("edit:pokemon:user1", &TrackedMessage{
		SentID: "chan1:msg-original",
		Target: "user1",
		Type:   "discord:user",
		Clean:  0,
	}, 5*time.Minute)

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"updated"}`),
		EditKey: "edit:pokemon:user1",
	}, true)

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

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Pre-track a message
	tracker.Track("edit:pokemon:user1", &TrackedMessage{
		SentID: "chan1:msg-original",
		Target: "user1",
		Type:   "discord:user",
		Clean:  0,
	}, 5*time.Minute)

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"fallback"}`),
		EditKey: "edit:pokemon:user1",
		TTH:     TTH{Minutes: 10},
	}, true)

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

// deleteTrackingSender records max Delete concurrency + delete/send counts.
type deleteTrackingSender struct {
	concurrent    atomic.Int32
	maxConcurrent atomic.Int32
	deletes       atomic.Int32
	sends         atomic.Int32
}

func (s *deleteTrackingSender) Send(_ context.Context, job *Job) (*SentMessage, error) {
	s.sends.Add(1)
	return &SentMessage{ID: "sent-" + job.Target}, nil
}

func (s *deleteTrackingSender) Delete(_ context.Context, _ string) error {
	cur := s.concurrent.Add(1)
	for {
		old := s.maxConcurrent.Load()
		if cur <= old || s.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond) // hold the slot to expose any concurrency
	s.concurrent.Add(-1)
	s.deletes.Add(1)
	return nil
}

func (s *deleteTrackingSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}
func (s *deleteTrackingSender) WaitForRateLimit(string) {}
func (s *deleteTrackingSender) Platform() string        { return "discord" }

// TestFairQueue_CleanDeleteSerializedPerTarget proves the fix for the reported
// 429 storm: many clean-deletes to the SAME channel are serialised (max 1
// in-flight) by the destination's lane drainer — instead of firing
// concurrently and 429-ing each other into failure. ConcurrentDiscord=8 would
// otherwise allow 8 at once. A delete job must also never trigger a Send.
func TestFairQueue_CleanDeleteSerializedPerTarget(t *testing.T) {
	sender := &deleteTrackingSender{}
	senders := map[string]Sender{"discord": sender}
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  8,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	const n = 20
	for i := range n {
		fq.enqueue(&Job{Type: "discord:channel", Target: "chan1", DeleteSentID: "chan1:msg" + strconv.Itoa(i)}, false)
	}
	fq.Stop() // closes every lane and drains all drainers

	if got := sender.deletes.Load(); got != n {
		t.Errorf("expected %d deletes, got %d", n, got)
	}
	if got := sender.maxConcurrent.Load(); got != 1 {
		t.Errorf("clean-deletes to the same target must serialise (max 1 concurrent), got %d — the concurrency bug", got)
	}
	if got := sender.sends.Load(); got != 0 {
		t.Errorf("a delete job must not trigger a Send, got %d", got)
	}
}

func TestFairQueueCleanTracking(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "chan1:msg-42"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:  "chan1",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"hello"}`),
		Clean:   1,
		TTH:     TTH{Minutes: 5},
	}, true)

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

// TestFairQueueSuppressesExpiredCleanMessage proves that clean-requested jobs
// with a zero/negative TTH are dropped BEFORE send — otherwise the message
// would post and never be cleaned (the tracker can't schedule a deletion for
// a past TTL, so it would live forever in the destination).
func TestFairQueueSuppressesExpiredCleanMessage(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "chan1:msg-99"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:  "chan1",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"late"}`),
		Clean:   1,
		TTH:     TTH{}, // all zero — event already expired at enrichment
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := len(discordMock.getSendCalls()); got != 0 {
		t.Errorf("expected clean+expired message to be suppressed, got %d send calls", got)
	}
}

// TestFairQueueEditOnlyExpiredStillSends proves that edit-only (clean=2)
// jobs with expired TTH are NOT suppressed — the message is still valid as
// a one-shot even if it can't be tracked for future edits.
func TestFairQueueEditOnlyExpiredStillSends(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "chan1:msg-100"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:  "chan1",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"edit-only"}`),
		Clean:   2, // edit bit, no clean bit
		TTH:     TTH{},
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := len(discordMock.getSendCalls()); got != 1 {
		t.Errorf("edit-only job should still send even with expired TTH, got %d send calls", got)
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

	fq, enq := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
		RateLimitHooks:    hooks,
	})
	fq.Start()

	for range 5 {
		enq(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)})
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
//
// All three jobs target the same destination, so they land in the same lane
// and are drained FIFO by that lane's single drainer — order is deterministic
// here, but we still assert on counts (not positions) since that's the
// property under test.
func TestRateLimitBypass(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 1, ChannelLimit: 5})
	defer limiter.Close()

	fq, enq := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	})
	fq.Start()

	// Burn the only DM slot
	enq(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)})
	// Bypass job — must still be delivered even though u1 is now over limit
	enq(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`), BypassRateLimit: true})
	// Non-bypass job — must be dropped
	enq(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)})

	fq.Stop() // Stop closes every lane and waits for all queued jobs to drain.

	calls := mock.getSendCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 sends (1 normal + 1 bypass), got %d", len(calls))
	}
	var bypass, normal int
	for _, c := range calls {
		if c.BypassRateLimit {
			bypass++
		} else {
			normal++
		}
	}
	if bypass != 1 || normal != 1 {
		t.Fatalf("expected 1 bypass send + 1 normal send, got bypass=%d normal=%d", bypass, normal)
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
	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	}, nil)
	fq.Start()

	// First send establishes the tracked message under EditKey "raid:1".
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}, true)
	time.Sleep(80 * time.Millisecond)

	// Edit reuses the existing message — must not count.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}, true)
	time.Sleep(80 * time.Millisecond)

	// Second new send — would only succeed if the edit didn't consume the budget.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}, true)
	time.Sleep(80 * time.Millisecond)

	// Third new send — over the DMLimit of 2; must be dropped.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}, true)
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
	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
	}, nil)
	fq.Start()

	// First send establishes the tracked message under EditKey "raid:1".
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}, true)
	time.Sleep(80 * time.Millisecond)

	// Edit attempt fails (mock.editErr). Falls through to a new Send — which
	// MUST count, because it produced a real wire delivery.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`),
		EditKey: "raid:1", Clean: 2, TTH: TTH{Hours: 1}}, true)
	time.Sleep(80 * time.Millisecond)

	// Limit is 1 and we have already counted two real sends — this third one
	// must be dropped.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}, true)
	time.Sleep(80 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getEditCalls()); got != 1 {
		t.Fatalf("expected 1 Edit attempt, got %d", got)
	}
	if got := len(mock.getSendCalls()); got != 2 {
		t.Fatalf("expected 2 Send calls (initial + edit-fallback), got %d", got)
	}
}

// TestRateLimitHookDoesNotDeadlock proves the lane drainer does not deadlock
// when the breach hook fires for a destination that has more jobs queued
// behind the breaching one. Hooks are fire-and-forget (their own goroutine),
// so the drainer must move on to the next job without waiting on the hook.
func TestRateLimitHookDoesNotDeadlock(t *testing.T) {
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	limiter := ratelimit.New(ratelimit.Config{TimingPeriod: 60, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer limiter.Close()

	// Hook that itself tries to dispatch — but we route its dispatch through
	// this signal channel to simulate the deadlock-prone scenario.
	hookCalled := make(chan struct{}, 1)
	hooks := dispatchingHooks{onBreach: func() { hookCalled <- struct{}{} }}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord: 1,
		RateLimiter:       limiter,
		RateLimitHooks:    hooks,
	}, nil)
	fq.Start()

	// Burn the DM slot.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}, true)
	// Trigger the breach.
	fq.enqueue(&Job{Target: "u1", Type: "discord:user", Message: json.RawMessage(`{}`)}, true)

	// Hook should fire promptly even though processJob runs on this
	// destination's lane drainer, because the hook runs in its own goroutine.
	select {
	case <-hookCalled:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("OnBreach hook did not fire — likely deadlocked in the lane drainer")
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

// TestQueueStampsReplyToID proves that when a Job carries a ReplyKey and the
// tracker has a known prior message under (ReplyKey, Target), the queue
// stamps Job.ReplyToID with that prior SentID before handing the job to the
// sender.
func TestQueueStampsReplyToID(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "chan1:msg-new"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Pre-populate the tracker with a prior under (replyKey="rk1", target="chan1")
	tracker.Track("clean:discord:channel:chan1:chan1:msg-prior", &TrackedMessage{
		SentID:   "chan1:msg-prior",
		Target:   "chan1",
		Type:     "discord:channel",
		ReplyKey: "rk1",
	}, 5*time.Minute)

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"changed"}`),
		ReplyKey: "rk1",
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	calls := discordMock.getSendCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(calls))
	}
	if calls[0].ReplyToID != "chan1:msg-prior" {
		t.Errorf("expected ReplyToID=chan1:msg-prior on sent job, got %q", calls[0].ReplyToID)
	}
}

// TestQueueDoesNotStampWhenEditKeyMatches proves that when both EditKey and
// ReplyKey are set on a job and the tracker has a prior under EditKey, the
// edit path runs and ReplyToID is NOT stamped (the message reuses the prior
// rather than replying to it).
func TestQueueDoesNotStampWhenEditKeyMatches(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	// Prior tracked under EditKey
	tracker.Track("edit:raid:1", &TrackedMessage{
		SentID: "chan1:msg-original",
		Target: "user1",
		Type:   "discord:user",
		Clean:  0,
	}, 5*time.Minute)
	// And ALSO a prior under (replyKey, target) — would be stamped if reply
	// path ran, but it shouldn't because edit takes priority.
	tracker.Track("clean:discord:user:user1:other-prior", &TrackedMessage{
		SentID:   "user1-dm:other-prior",
		Target:   "user1",
		Type:     "discord:user",
		ReplyKey: "rk-x",
	}, 5*time.Minute)

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:   "user1",
		Type:     "discord:user",
		Message:  json.RawMessage(`{"content":"updated"}`),
		EditKey:  "edit:raid:1",
		ReplyKey: "rk-x",
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	// Edit ran, no Send.
	if got := len(discordMock.getEditCalls()); got != 1 {
		t.Fatalf("expected 1 edit call, got %d", got)
	}
	if got := len(discordMock.getSendCalls()); got != 0 {
		t.Fatalf("expected 0 send calls (edit path used), got %d", got)
	}
}

// TestQueueTracksReplyKeyAfterSend proves that successful sends propagate the
// ReplyKey into the tracker so a follow-up job with the same (ReplyKey, Target)
// gets stamped with the most recent SentID.
func TestQueueTracksReplyKeyAfterSend(t *testing.T) {
	// Sender returns deterministic incrementing sentIDs so we can verify the
	// second job picks up the first job's id.
	var counter int32
	mock := &counterSender{
		platform: "discord",
		next: func() string {
			n := atomic.AddInt32(&counter, 1)
			return "chan1:msg-" + strconv.Itoa(int(n))
		},
	}
	senders := map[string]Sender{"discord": mock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	// First job: no prior, but carries ReplyKey so the send is tracked under it.
	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"first"}`),
		ReplyKey: "rk-chain",
		// Need TTH > 0 so post-send tracking inserts into cache.
		TTH: TTH{Hours: 1},
	}, true)
	time.Sleep(80 * time.Millisecond)

	// Second job with same ReplyKey/Target — must get ReplyToID stamped to
	// the first job's SentID.
	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"second"}`),
		ReplyKey: "rk-chain",
		TTH:      TTH{Hours: 1},
	}, true)
	time.Sleep(80 * time.Millisecond)
	fq.Stop()

	calls := mock.getSendCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(calls))
	}
	if calls[0].ReplyToID != "" {
		t.Errorf("first send should not have ReplyToID stamped, got %q", calls[0].ReplyToID)
	}
	if calls[1].ReplyToID != "chan1:msg-1" {
		t.Errorf("second send should have ReplyToID=chan1:msg-1 (first job's sentID), got %q", calls[1].ReplyToID)
	}

	// Sanity: tracker.LookupReply now returns the second's SentID.
	if got := tracker.LookupReply("rk-chain", "chan1"); got != "chan1:msg-2" {
		t.Errorf("LookupReply after both sends = %q, want chan1:msg-2", got)
	}
}

// counterSender returns incrementing SentIDs for each Send.
type counterSender struct {
	platform  string
	mu        sync.Mutex
	sendCalls []*Job
	next      func() string
}

func (c *counterSender) Send(_ context.Context, job *Job) (*SentMessage, error) {
	c.mu.Lock()
	c.sendCalls = append(c.sendCalls, job)
	c.mu.Unlock()
	return &SentMessage{ID: c.next()}, nil
}
func (c *counterSender) Delete(_ context.Context, _ string) error { return nil }
func (c *counterSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}
func (c *counterSender) Platform() string          { return c.platform }
func (c *counterSender) WaitForRateLimit(_ string) {}
func (c *counterSender) getSendCalls() []*Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Job, len(c.sendCalls))
	copy(out, c.sendCalls)
	return out
}

// TestProcessJob_ReplyOnlyTrackerStorage proves that a job with Clean=0,
// EditKey="", and a non-empty ReplyKey still lands in the tracker after send.
// The entry must have Clean=0 so the auto-delete path (gated on IsClean) does
// not fire when the TTL expires.
func TestProcessJob_ReplyOnlyTrackerStorage(t *testing.T) {
	mock := &queueMockSender{platform: "discord", sentID: "chan1:msg-100"}
	senders := map[string]Sender{"discord": mock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"egg"}`),
		Clean:    0,
		EditKey:  "",
		ReplyKey: "raidlife:test:1700000000",
		TTH:      TTH{Hours: 1},
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	// Tracker must have one entry (the reply-only message).
	if got := tracker.Size(); got != 1 {
		t.Fatalf("expected tracker.Size()=1 after reply-only send, got %d", got)
	}

	// The entry must be reachable via LookupReply.
	sentID := tracker.LookupReply("raidlife:test:1700000000", "chan1")
	if sentID == "" {
		t.Fatal("expected LookupReply to return the sent message ID, got empty")
	}
	if sentID != "chan1:msg-100" {
		t.Errorf("expected sentID=chan1:msg-100, got %q", sentID)
	}

	// The Clean field in the stored entry must be 0 — so auto-delete won't fire.
	msg := tracker.LookupReplyMessage("raidlife:test:1700000000", "chan1")
	if msg == nil {
		t.Fatal("LookupReplyMessage returned nil")
	}
	if msg.Clean != 0 {
		t.Errorf("expected stored entry Clean=0 (no auto-delete), got %d", msg.Clean)
	}
}

// TestProcessJob_NoReplyKey_NotTracked proves that a job with Clean=0,
// EditKey="", and ReplyKey="" is NOT added to the tracker — there is nothing
// to index so there is no point in storing it.
func TestProcessJob_NoReplyKey_NotTracked(t *testing.T) {
	mock := &queueMockSender{platform: "discord", sentID: "chan1:msg-200"}
	senders := map[string]Sender{"discord": mock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"plain"}`),
		Clean:    0,
		EditKey:  "",
		ReplyKey: "",
		TTH:      TTH{Hours: 1},
	}, true)

	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := tracker.Size(); got != 0 {
		t.Fatalf("expected tracker.Size()=0 for job with no clean/edit/reply, got %d", got)
	}
}

// TestProcessJob_ReplyChainWithoutClean proves the end-to-end reply chain:
// job 1 (ReplyKey="k", Clean=0) is sent → tracked; job 2 (same ReplyKey/Target)
// is sent → tracker lookup stamps job 2's ReplyToID with job 1's SentID before
// the platform sender receives it.
func TestProcessJob_ReplyChainWithoutClean(t *testing.T) {
	var n int32
	mock := &counterSender{
		platform: "discord",
		next: func() string {
			id := atomic.AddInt32(&n, 1)
			return "chan1:msg-" + strconv.Itoa(int(id))
		},
	}
	senders := map[string]Sender{"discord": mock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()

	// Job 1: egg alert — no clean, no edit, just ReplyKey.
	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"egg"}`),
		Clean:    0,
		ReplyKey: "k",
		TTH:      TTH{Hours: 1},
	}, true)
	time.Sleep(80 * time.Millisecond)

	// Job 2: raid alert — same ReplyKey, same target.
	fq.enqueue(&Job{
		Target:   "chan1",
		Type:     "discord:channel",
		Message:  json.RawMessage(`{"content":"raid"}`),
		Clean:    0,
		ReplyKey: "k",
		TTH:      TTH{Hours: 1},
	}, true)
	time.Sleep(80 * time.Millisecond)
	fq.Stop()

	calls := mock.getSendCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(calls))
	}
	// Job 1 had no prior — ReplyToID must be empty.
	if calls[0].ReplyToID != "" {
		t.Errorf("job 1 should have no ReplyToID, got %q", calls[0].ReplyToID)
	}
	// Job 2 must carry job 1's SentID so Discord threads it as a reply.
	if calls[1].ReplyToID != "chan1:msg-1" {
		t.Errorf("job 2 ReplyToID = %q, want chan1:msg-1", calls[1].ReplyToID)
	}
}

func TestFairQueueStop(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": discordMock}

	fq, enq := newTestFairQueue(t, senders, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	fq.Start()

	// Enqueue some jobs then immediately stop
	for range 5 {
		enq(&Job{
			Target:  "user1",
			Type:    "discord:user",
			Message: json.RawMessage(`{}`),
		})
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

// laneMockSender is a minimal Sender for lane-isolation/reap/shutdown tests.
// It hooks BOTH Send and Delete so tests can park either path (clean-deletes
// invoke Delete, not Send).
type laneMockSender struct {
	onSend   func(*Job)
	onDelete func(sentID string)
}

func (m *laneMockSender) Send(_ context.Context, job *Job) (*SentMessage, error) {
	if m.onSend != nil {
		m.onSend(job)
	}
	return &SentMessage{ID: "sent-" + job.Target}, nil
}
func (m *laneMockSender) Delete(_ context.Context, sentID string) error {
	if m.onDelete != nil {
		m.onDelete(sentID)
	}
	return nil
}
func (m *laneMockSender) Edit(_ context.Context, _ string, _ json.RawMessage, _ []byte) error {
	return nil
}
func (m *laneMockSender) WaitForRateLimit(string) {}
func (m *laneMockSender) Platform() string        { return "discord" }

// waitFor polls cond until true or the deadline; fails the test on timeout.
func waitFor(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

// TestLanes_SlowTargetDoesNotBlockFreeTarget proves the headline property of
// per-destination lanes (Phase 2): a target parked mid-send does not delay
// delivery to an unrelated target. Before Phase 2 (shared channel + worker
// pool), a parked worker could starve other destinations under a saturated
// pool; with one lane + drainer per target, "free" delivers immediately
// while "slow" sits blocked in its own goroutine.
func TestLanes_SlowTargetDoesNotBlockFreeTarget(t *testing.T) {
	release := make(chan struct{})
	freeDone := make(chan struct{})
	sender := &laneMockSender{
		onSend: func(job *Job) {
			if job.Target == "slow" {
				<-release // park the slow lane
			} else {
				close(freeDone)
			}
		},
	}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 4})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	enq(&Job{Type: "discord:channel", Target: "slow", Message: json.RawMessage(`{}`)})
	enq(&Job{Type: "discord:channel", Target: "free", Message: json.RawMessage(`{}`)})

	select {
	case <-freeDone:
		// pass: free lane delivered while slow lane parked
	case <-time.After(time.Second):
		t.Fatal("free target blocked behind a parked slow target — lanes not isolated")
	}
}

// TestLanes_ReuseAfterDrainDelivers proves a target's lane can be fully
// drained and then reused without losing jobs. This test does NOT force a
// real reap (the lane's drainer is likely still alive and idle, waiting out
// idleTimeout, when the second wave arrives) — it only proves that draining
// a lane to empty and then enqueuing more to the same target still delivers
// everything, whether the lane is reused live or respawned fresh. The actual
// reap path (idle timer fires, lane map entry deleted, drainer goroutine
// exits) is covered by TestLanes_ReapsIdleLane below. The counter-under-lock
// invariant (lane.pending guarded by lanesMu) is what makes reap-vs-enqueue
// races safe in general; that invariant is exercised structurally here and
// in the shutdown-drain test below, and directly raced in
// TestLanes_ReapEnqueueRace.
func TestLanes_ReuseAfterDrainDelivers(t *testing.T) {
	var delivered atomic.Int32
	sender := &laneMockSender{onSend: func(*Job) { delivered.Add(1) }}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 2})
	fq.Start()
	defer fq.Stop()

	for range 50 {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	waitFor(t, func() bool { return delivered.Load() == 50 }, time.Second)

	for range 50 {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	waitFor(t, func() bool { return delivered.Load() == 100 }, time.Second)
}

// TestLanes_ReapsIdleLane proves the actual reap path: with idleTimeout
// shortened, a lane that finishes all its work and sits idle gets deleted
// from fq.lanes (LaneStats active hits 0) and its drainer goroutine exits.
// idleTimeout is set BEFORE the first enqueue — the drainer that reads it
// doesn't exist yet, so there is no concurrent read to race the write. After
// the reap is observed, a second wave to the same target proves reap-then-
// recreate works: the lane is respawned on demand and still delivers.
func TestLanes_ReapsIdleLane(t *testing.T) {
	var delivered atomic.Int32
	sender := &laneMockSender{onSend: func(*Job) { delivered.Add(1) }}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 2})
	fq.idleTimeout = 5 * time.Millisecond // set before any enqueue — no drainer yet to race
	fq.Start()
	defer fq.Stop()

	for range 5 {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	waitFor(t, func() bool { return delivered.Load() == 5 }, time.Second)

	// Prove the lane actually reaped: active lane count drops to 0.
	waitFor(t, func() bool {
		_, active, _, _, _ := fq.LaneStats()
		return active == 0
	}, time.Second)

	// Reap-then-recreate: a fresh wave to the same target must still deliver.
	for range 5 {
		enq(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)})
	}
	waitFor(t, func() bool { return delivered.Load() == 10 }, time.Second)
}

// TestLanes_ReapEnqueueRace races enqueue against the reap timer under a tiny
// idleTimeout: several goroutines hammer a small set of targets with short
// sleeps between bursts (giving lanes a chance to drain and reap between
// bursts), while enqueue may concurrently be spawning a fresh lane for a
// target whose old lane is mid-reap. The counter-under-lock invariant
// (lane.pending incremented under lanesMu before the lock is released, and
// the reaper only deletes when pending==0 under the same lock) means every
// enqueued job must still be delivered — none may be silently lost to a
// reap racing the enqueue — and no send-on-closed-channel panic may occur.
// This is the core risk the spec calls out; it's only meaningful run with
// -race and repeated (-count=10+).
func TestLanes_ReapEnqueueRace(t *testing.T) {
	var delivered atomic.Int64
	sender := &laneMockSender{onSend: func(*Job) { delivered.Add(1) }}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 8})
	fq.idleTimeout = 2 * time.Millisecond // set before any enqueue — no drainer yet to race
	fq.Start()
	defer fq.Stop() // drains anything still buffered

	const goroutines = 8
	const perGoroutine = 50
	const targets = 4

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for j := range perGoroutine {
				target := "t" + strconv.Itoa((g+j)%targets)
				enq(&Job{Type: "discord:channel", Target: target, Message: json.RawMessage(`{}`)})
				if j%5 == 0 {
					// Give lanes a chance to drain + reap between bursts, so
					// enqueue has a real chance of racing a reap in flight.
					time.Sleep(3 * time.Millisecond)
				}
			}
		}(g)
	}
	wg.Wait()

	waitFor(t, func() bool { return delivered.Load() == goroutines*perGoroutine }, 5*time.Second)

	if got := delivered.Load(); got != goroutines*perGoroutine {
		t.Fatalf("expected %d delivered (no job lost to a reap race), got %d", goroutines*perGoroutine, got)
	}
}

// TestLanes_ShutdownDrainsAllLanes proves Stop() blocks until every buffered
// job across every lane has been delivered, not just until the drainers
// exit. Ten lanes are pre-loaded with five jobs apiece (each Send taking
// 1ms), then Stop() is called with none of them consumed yet; Stop must not
// return until all 50 have gone through processJob.
func TestLanes_ShutdownDrainsAllLanes(t *testing.T) {
	var delivered atomic.Int32
	sender := &laneMockSender{onSend: func(*Job) {
		time.Sleep(time.Millisecond)
		delivered.Add(1)
	}}
	senders := map[string]Sender{"discord": sender}
	fq, enq := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 4})
	fq.Start()

	const lanes, per = 10, 5
	for l := range lanes {
		for range per {
			enq(&Job{Type: "discord:channel", Target: "t" + strconv.Itoa(l), Message: json.RawMessage(`{}`)})
		}
	}
	fq.Stop() // must block until every buffered job is delivered

	if got := delivered.Load(); got != lanes*per {
		t.Errorf("expected %d delivered before Stop returned, got %d", lanes*per, got)
	}
}

// TestLanes_CleanDeleteDropsOnFullLane proves overflow policy D5's drop side:
// enqueue(job, false) (clean-deletes) DROP on a full lane and return false,
// rather than blocking like sends do. The drainer is parked on Delete (clean-
// deletes invoke Sender.Delete, not Send) so the tiny two-slot buffer fills;
// "enqueue until one drops" is timing-robust regardless of exactly when the
// drainer picks up the first job. A different target's lane must stay
// unaffected by the full "t" lane.
func TestLanes_CleanDeleteDropsOnFullLane(t *testing.T) {
	release := make(chan struct{})
	sender := &laneMockSender{onDelete: func(string) { <-release }} // park the drainer
	senders := map[string]Sender{"discord": sender}
	fq, _ := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 1, PerRouteBuffer: 2})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	accepted, dropped := 0, 0
	for i := range 20 {
		if fq.enqueue(&Job{Type: "discord:channel", Target: "t", DeleteSentID: "t:" + strconv.Itoa(i)}, false) {
			accepted++
		} else {
			dropped++
		}
	}
	if accepted == 0 {
		t.Fatal("expected some clean-deletes to be accepted")
	}
	if dropped == 0 {
		t.Error("expected some clean-deletes to be dropped on the full lane (drainer parked, buffer=2)")
	}

	// A different target's lane is unaffected.
	if !fq.enqueue(&Job{Type: "discord:channel", Target: "other", DeleteSentID: "other:1"}, false) {
		t.Error("a different target's lane should accept the clean-delete")
	}
}

// TestLaneStats proves LaneStats reports real per-lane aggregates: one lane
// parked in-flight (the drainer's onSend blocks on release) plus six more
// buffered behind it in the same lane's channel yields depth 6 (the in-flight
// job isn't in the channel anymore), active lane count 1, and the deepest
// target name.
func TestLaneStats(t *testing.T) {
	release := make(chan struct{})
	sender := &laneMockSender{onSend: func(*Job) { <-release }}
	senders := map[string]Sender{"discord": sender}
	fq, _ := newTestFairQueue(t, senders, QueueConfig{ConcurrentDiscord: 1, PerRouteBuffer: 10})
	fq.Start()
	defer func() { close(release); fq.Stop() }()

	// One lane parked in-flight + 6 buffered => depth 6, active 1.
	for range 7 {
		fq.enqueue(&Job{Type: "discord:channel", Target: "t", Message: json.RawMessage(`{}`)}, true)
	}
	waitFor(t, func() bool {
		total, active, maxDepth, target, _ := fq.LaneStats()
		return active == 1 && total == 6 && maxDepth == 6 && target == "t"
	}, time.Second)
}
