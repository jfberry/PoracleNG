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

// TestDispatcher_PausedDropsNormalJob verifies normal jobs are dropped (not
// buffered) during pause. Buffering would OOM on long pauses and flood users
// with stale alerts on resume.
func TestDispatcher_PausedDropsNormalJob(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()

	d.Pause("test drop")

	d.Dispatch(normalJob())
	// Give the worker time to pick the job up and drop it.
	time.Sleep(100 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 0 {
		t.Fatalf("expected 0 sends (job dropped) while paused, got %d", got)
	}

	// Resume — nothing new should arrive because the previous job is gone, not buffered.
	d.Resume()
	time.Sleep(200 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 0 {
		t.Errorf("expected 0 sends after resume (dropped job is gone), got %d", got)
	}

	// A subsequent job after resume should send normally.
	d.Dispatch(normalJob())
	time.Sleep(200 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 1 {
		t.Errorf("expected 1 send for the post-resume job, got %d", got)
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

// TestDispatcher_PausedDropsEditJob verifies edit jobs are ALSO dropped during
// pause — operator's mental model is "throw everything on the floor"; updating
// already-sent messages while paused doesn't serve maintenance.
func TestDispatcher_PausedDropsEditJob(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 1)
	d.Start()

	d.Pause("test edit drop")

	d.Dispatch(&Job{
		Target:  "user1",
		Type:    "discord:user",
		Message: json.RawMessage(`{"content":"edit"}`),
		EditKey: "some-key",
	})
	time.Sleep(150 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 0 {
		t.Errorf("expected 0 sends (edit job dropped) while paused, got %d", got)
	}

	d.Resume()
	d.Stop()
}

// TestDispatcher_PausedDropsMultipleJobs verifies that many normal jobs
// dispatched during pause are all dropped (no buffering / memory growth).
func TestDispatcher_PausedDropsMultipleJobs(t *testing.T) {
	d, mock := newPauseTestDispatcher(t, 3)
	d.Start()

	d.Pause("test multiple drop")

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			d.Dispatch(normalJob())
		})
	}
	wg.Wait()

	// Give workers time to process all 5 dropped jobs.
	time.Sleep(200 * time.Millisecond)

	if got := len(mock.getSendCalls()); got != 0 {
		t.Errorf("expected 0 sends (all dropped) while paused, got %d", got)
	}

	d.Resume()
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
