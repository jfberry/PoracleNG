package delivery

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"
)

type mockSender struct {
	deleted []string
	mu      sync.Mutex
}

func (m *mockSender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	return nil, nil
}

func (m *mockSender) Delete(ctx context.Context, sentID string) error {
	m.mu.Lock()
	m.deleted = append(m.deleted, sentID)
	m.mu.Unlock()
	return nil
}

func (m *mockSender) Edit(ctx context.Context, sentID string, message json.RawMessage) error {
	return nil
}

func (m *mockSender) Platform() string { return "discord" }

func (m *mockSender) WaitForRateLimit(target string) {}

func (m *mockSender) getDeleted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.deleted))
	copy(result, m.deleted)
	return result
}

func newTestTracker(t *testing.T) (*MessageTracker, *mockSender) {
	t.Helper()
	mock := &mockSender{}
	senders := map[string]Sender{"discord": mock}
	mt := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { mt.cache.Stop() })
	return mt, mock
}

func TestTrackerTrackAndLookup(t *testing.T) {
	mt, _ := newTestTracker(t)

	msg := &TrackedMessage{
		SentID: "msg-123",
		Target: "user-1",
		Type:   "discord:user",
		Clean:  0,
	}
	mt.Track("edit:pokemon:user-1", msg, 5*time.Minute)

	got := mt.LookupEdit("edit:pokemon:user-1")
	if got == nil {
		t.Fatal("expected tracked message, got nil")
	}
	if got.SentID != "msg-123" {
		t.Errorf("expected SentID msg-123, got %s", got.SentID)
	}
	if got.Target != "user-1" {
		t.Errorf("expected Target user-1, got %s", got.Target)
	}
}

func TestTrackerLookupMissing(t *testing.T) {
	mt, _ := newTestTracker(t)

	got := mt.LookupEdit("nonexistent-key")
	if got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestTrackerUpdateEdit(t *testing.T) {
	mt, _ := newTestTracker(t)

	msg := &TrackedMessage{
		SentID: "msg-100",
		Target: "user-1",
		Type:   "discord:user",
		Clean:  0,
	}
	mt.Track("edit:raid:user-1", msg, 5*time.Minute)

	mt.UpdateEdit("edit:raid:user-1", "msg-200")

	got := mt.LookupEdit("edit:raid:user-1")
	if got == nil {
		t.Fatal("expected tracked message after update, got nil")
	}
	if got.SentID != "msg-200" {
		t.Errorf("expected updated SentID msg-200, got %s", got.SentID)
	}
}

func TestTrackerCleanOnExpiry(t *testing.T) {
	mt, mock := newTestTracker(t)

	msg := &TrackedMessage{
		SentID: "msg-clean-1",
		Target: "chan-1",
		Type:   "discord:channel",
		Clean:  1,
	}
	mt.Track("clean:discord:channel:chan-1:msg-clean-1", msg, 50*time.Millisecond)

	// Wait for expiry + eviction processing + goroutine
	time.Sleep(200 * time.Millisecond)

	deleted := mock.getDeleted()
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d: %v", len(deleted), deleted)
	}
	if deleted[0] != "msg-clean-1" {
		t.Errorf("expected deleted msg-clean-1, got %s", deleted[0])
	}
}

func TestTrackerNonCleanNoDelete(t *testing.T) {
	mt, mock := newTestTracker(t)

	msg := &TrackedMessage{
		SentID: "msg-noclean",
		Target: "user-2",
		Type:   "discord:user",
		Clean:  0,
	}
	mt.Track("edit:pokemon:user-2", msg, 50*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	deleted := mock.getDeleted()
	if len(deleted) != 0 {
		t.Errorf("expected no deletions for non-clean message, got %d: %v", len(deleted), deleted)
	}
}

func TestTrackerSaveLoad(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSender{}
	senders := map[string]Sender{"discord": mock}

	mt1 := NewMessageTracker(dir, senders)
	mt1.Track("edit:a", &TrackedMessage{SentID: "s1", Target: "t1", Type: "discord:user", Clean: 0}, 5*time.Minute)
	mt1.Track("edit:b", &TrackedMessage{SentID: "s2", Target: "t2", Type: "discord:channel", Clean: 1}, 5*time.Minute)

	if err := mt1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	mt1.cache.Stop()

	mt2 := NewMessageTracker(dir, senders)
	if err := mt2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer mt2.cache.Stop()

	got := mt2.LookupEdit("edit:a")
	if got == nil {
		t.Fatal("expected edit:a to be restored, got nil")
	}
	if got.SentID != "s1" {
		t.Errorf("expected SentID s1, got %s", got.SentID)
	}

	got2 := mt2.LookupEdit("edit:b")
	if got2 == nil {
		t.Fatal("expected edit:b to be restored, got nil")
	}
	if got2.SentID != "s2" {
		t.Errorf("expected SentID s2, got %s", got2.SentID)
	}
	if got2.Clean == 0 {
		t.Error("expected edit:b to have Clean=true")
	}
}

func TestTrackerLoadExpiredClean(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSender{}
	senders := map[string]Sender{"discord": mock}

	// Create a tracker, track with short TTL, save, stop
	mt1 := NewMessageTracker(dir, senders)
	mt1.Track("clean:discord:user:u1:expired-msg", &TrackedMessage{
		SentID: "expired-msg",
		Target: "u1",
		Type:   "discord:user",
		Clean:  1,
	}, 10*time.Millisecond)

	// Wait for the TTL to pass but save before eviction processes it
	// (we want to test Load's expired-clean handling)
	if err := mt1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	mt1.cache.Stop()

	// Wait so the saved entry is definitely expired
	time.Sleep(50 * time.Millisecond)

	// Load in a new tracker — should detect expired clean and schedule deletion
	mt2 := NewMessageTracker(dir, senders)
	if err := mt2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer mt2.cache.Stop()

	// Wait for the async delete goroutine
	time.Sleep(100 * time.Millisecond)

	deleted := mock.getDeleted()
	found := slices.Contains(deleted, "expired-msg")
	if !found {
		t.Errorf("expected expired-msg to be deleted on load, got deletions: %v", deleted)
	}

	// Should NOT be in the cache
	if mt2.LookupEdit("clean:discord:user:u1:expired-msg") != nil {
		t.Error("expired entry should not be in cache after load")
	}
}

func TestTrackerSize(t *testing.T) {
	mt, _ := newTestTracker(t)

	mt.Track("k1", &TrackedMessage{SentID: "s1", Target: "t1", Type: "discord:user"}, 5*time.Minute)
	mt.Track("k2", &TrackedMessage{SentID: "s2", Target: "t2", Type: "discord:channel"}, 5*time.Minute)
	mt.Track("k3", &TrackedMessage{SentID: "s3", Target: "t3", Type: "telegram:user"}, 5*time.Minute)

	if mt.Size() != 3 {
		t.Errorf("expected Size() = 3, got %d", mt.Size())
	}
}
