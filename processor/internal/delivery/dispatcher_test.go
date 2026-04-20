package delivery

import (
	"encoding/json"
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
