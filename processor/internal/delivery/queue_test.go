package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// queueMockSender is a configurable mock for queue tests.
// (Named differently from mockSender in tracker_test.go to avoid conflict.)
type queueMockSender struct {
	platform   string
	sendCalls  []*Job
	editCalls  []string // sentIDs passed to Edit
	mu         sync.Mutex
	sendErr    error
	editErr    error
	sendDelay  time.Duration
	sentID     string // returned from Send
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
	for i := 0; i < 6; i++ {
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
		Clean:  false,
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
		Clean:  false,
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
		Clean:   true,
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
	if !tracked.Clean {
		t.Error("expected tracked message to have Clean=true")
	}
}

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
	for i := 0; i < 5; i++ {
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
