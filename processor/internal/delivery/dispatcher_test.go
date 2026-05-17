package delivery

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestDispatcherIntegration(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord"}
	telegramMock := &queueMockSender{platform: "telegram"}
	senders := map[string]Sender{
		"discord":  discordMock,
		"telegram": telegramMock,
	}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	d := NewDispatcherWithSenders(senders, tracker, 100, QueueConfig{
		ConcurrentDiscord:  2,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	d.Start()

	d.Dispatch(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"hello discord"}`),
	})
	d.Dispatch(&Job{
		Target:  "tg1",
		Type:    "telegram:user",
		Message: json.RawMessage(`{"content":"hello telegram"}`),
	})

	time.Sleep(100 * time.Millisecond)

	if d.QueueDepth() != 0 {
		t.Errorf("expected queue depth 0 after processing, got %d", d.QueueDepth())
	}

	d.Stop()

	discordCalls := discordMock.getSendCalls()
	telegramCalls := telegramMock.getSendCalls()

	if len(discordCalls) != 1 {
		t.Errorf("expected 1 discord send call, got %d", len(discordCalls))
	}
	if len(telegramCalls) != 1 {
		t.Errorf("expected 1 telegram send call, got %d", len(telegramCalls))
	}
}

func TestDispatcherTrackerSize(t *testing.T) {
	discordMock := &queueMockSender{platform: "discord", sentID: "ch1:msg1"}
	senders := map[string]Sender{"discord": discordMock}

	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })

	d := NewDispatcherWithSenders(senders, tracker, 100, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	d.Start()

	d.Dispatch(&Job{
		Target:  "chan1",
		Type:    "discord:channel",
		Message: json.RawMessage(`{"content":"tracked"}`),
		Clean:   1,
		TTH:     TTH{Minutes: 10},
	})

	time.Sleep(100 * time.Millisecond)
	d.Stop()

	if d.TrackerSize() != 1 {
		t.Errorf("expected tracker size 1 after clean job, got %d", d.TrackerSize())
	}
}

// newPauseTestDispatcher creates a Dispatcher with a single discord sender for
// pause/resume tests. concurrentDiscord controls worker parallelism; pass 1 for
// serialised tests. The caller is responsible for calling d.Stop().
func newPauseTestDispatcher(t *testing.T, concurrentDiscord int) (*Dispatcher, *queueMockSender) {
	t.Helper()
	mock := &queueMockSender{platform: "discord"}
	senders := map[string]Sender{"discord": mock}
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	d := NewDispatcherWithSenders(senders, tracker, 100, QueueConfig{
		ConcurrentDiscord:  concurrentDiscord,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	return d, mock
}

func normalJob() *Job {
	return &Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"hello"}`),
	}
}

// TestDispatcher_PauseResume_State verifies the PauseState accessors.
func TestDispatcher_PauseResume_State(t *testing.T) {
	d, _ := newPauseTestDispatcher(t, 1)
	d.Start()
	defer d.Stop()

	paused, reason, since := d.PauseState()
	if paused || reason != "" || !since.IsZero() {
		t.Fatalf("expected initial state (false,'',zero), got (%v,%q,%v)", paused, reason, since)
	}

	before := time.Now()
	d.Pause("maintenance")
	after := time.Now()

	paused, reason, since = d.PauseState()
	if !paused {
		t.Error("expected paused=true after Pause")
	}
	if reason != "maintenance" {
		t.Errorf("expected reason %q, got %q", "maintenance", reason)
	}
	if since.Before(before) || since.After(after) {
		t.Errorf("pausedSince %v outside expected window [%v, %v]", since, before, after)
	}

	d.Resume()

	paused, reason, since = d.PauseState()
	if paused || reason != "" || !since.IsZero() {
		t.Fatalf("expected state (false,'',zero) after Resume, got (%v,%q,%v)", paused, reason, since)
	}
}

// TestDispatcher_NotPaused_NoOp verifies normal dispatch is unaffected when not paused.
func TestDispatcher_NotPaused_NoOp(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()
	defer d.Stop()

	d.Dispatch(normalJob())
	time.Sleep(100 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 1 {
		t.Errorf("expected 1 send without pause, got %d", got)
	}
}

// TestDispatcher_PausedHoldsNormalJob verifies a normal job blocks during pause
// and drains after Resume.
func TestDispatcher_PausedHoldsNormalJob(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()

	d.Pause("test hold")

	done := make(chan struct{})
	go func() {
		d.Dispatch(normalJob())
		close(done) // not meaningful for ordering, but stops goroutine leak
	}()

	// The job should NOT have been sent within 100ms while paused.
	time.Sleep(100 * time.Millisecond)
	if got := len(mock.getSendCalls()); got != 0 {
		t.Fatalf("expected 0 sends while paused, got %d", got)
	}

	// Resume — job should now drain.
	d.Resume()
	time.Sleep(200 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 1 {
		t.Errorf("expected 1 send after resume, got %d", got)
	}

	d.Stop()
}

// TestDispatcher_PausedAllowsBypassJob verifies bypass jobs skip the pause gate.
func TestDispatcher_PausedAllowsBypassJob(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()

	d.Pause("test bypass")

	d.DispatchBypass(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"bypass"}`),
	})

	// Bypass job must arrive within 200ms without needing Resume.
	deadline := time.After(200 * time.Millisecond)
	for {
		if len(mock.getSendCalls()) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("bypass job did not send within 200ms while paused (sends=%d)", len(mock.getSendCalls()))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Clean up: resume so Stop doesn't block on the waiting worker.
	d.Resume()
	d.Stop()
}

// TestDispatcher_PausedAllowsEditJob verifies edit jobs (EditKey != "") skip the pause gate.
func TestDispatcher_PausedAllowsEditJob(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()

	d.Pause("test edit bypass")

	d.Dispatch(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"edit"}`),
		EditKey: "some-key", // non-empty → skips pause gate
	})

	// Edit job must arrive within 200ms without needing Resume.
	deadline := time.After(200 * time.Millisecond)
	for {
		if len(mock.getSendCalls()) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("edit job did not send within 200ms while paused (sends=%d)", len(mock.getSendCalls()))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	d.Resume()
	d.Stop()
}

// TestDispatcher_ResumeWakesMultipleWaiters verifies that Resume unblocks all
// goroutines waiting in the pause gate simultaneously.
func TestDispatcher_ResumeWakesMultipleWaiters(t *testing.T) {
	// 3 workers + 3 jobs ensures all three are parked in waitWhilePaused
	// at the same time, so Resume must broadcast (not signal) to wake them.
	d, mock := newPauseTestDispatcher(t, 3)
	d.Start()

	d.Pause("test multiple")

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Dispatch(normalJob())
		}()
	}

	// Give goroutines time to reach the pause gate.
	time.Sleep(100 * time.Millisecond)
	if got := len(mock.getSendCalls()); got != 0 {
		t.Fatalf("expected 0 sends while paused (3 jobs queued), got %d", got)
	}

	d.Resume()

	// All 3 should drain within a generous timeout.
	deadline := time.After(2 * time.Second)
	for {
		if len(mock.getSendCalls()) == 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d/3 sends completed after resume", len(mock.getSendCalls()))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	wg.Wait()
	d.Stop()
}

// TestDispatcher_PauseIsIdempotent verifies that calling Pause twice keeps the
// original reason and timestamp (not reset by the second call).
func TestDispatcher_PauseIsIdempotent(t *testing.T) {
	d, _ := newPauseTestDispatcher(t, 1)
	d.Start()
	defer d.Stop()

	d.Pause("first reason")
	_, _, since1 := d.PauseState()

	time.Sleep(10 * time.Millisecond)
	d.Pause("second reason")

	paused, reason, since2 := d.PauseState()

	if !paused {
		t.Error("expected paused=true")
	}
	if reason != "first reason" {
		t.Errorf("expected reason %q (first call wins), got %q", "first reason", reason)
	}
	if !since2.Equal(since1) {
		t.Errorf("pausedSince should not change on second Pause call: first=%v second=%v", since1, since2)
	}

	d.Resume()
}
