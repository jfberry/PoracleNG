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

func (m *mockSender) Edit(ctx context.Context, sentID string, message json.RawMessage, _ []byte) error {
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
	t.Cleanup(func() {
		mt.cache.Stop()
	})
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

func TestLookupReplyReturnsLatest(t *testing.T) {
	mt, _ := newTestTracker(t)

	mt.Track("edit-1", &TrackedMessage{
		SentID: "msg-1", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	mt.Track("edit-2", &TrackedMessage{
		SentID: "msg-2", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	if got := mt.LookupReply("rk1", "targetA"); got != "msg-2" {
		t.Fatalf("LookupReply = %q, want msg-2", got)
	}
	if got := mt.LookupReply("rk1", "targetB"); got != "" {
		t.Errorf("LookupReply cross-target = %q, want empty", got)
	}
	if got := mt.LookupReply("rk-other", "targetA"); got != "" {
		t.Errorf("LookupReply wrong key = %q, want empty", got)
	}
	// Empty replyKey — never matches, defensive against bugs.
	if got := mt.LookupReply("", "targetA"); got != "" {
		t.Errorf("LookupReply empty key = %q, want empty", got)
	}
}

// LookupReplyMessage must return the full prior TrackedMessage so
// change-event dispatch can inherit rule-level fields (Clean, Type)
// when reconstructing prior-only recipients. Without this, the
// monsterChanged reply doesn't auto-delete alongside the original.
func TestLookupReplyMessage_ReturnsFullPrior(t *testing.T) {
	mt, _ := newTestTracker(t)

	mt.Track("edit-clean", &TrackedMessage{
		SentID: "msg-clean", Target: "userA", Type: "discord:user",
		Clean: 1, ReplyKey: "enc-x",
	}, time.Hour)

	got := mt.LookupReplyMessage("enc-x", "userA")
	if got == nil {
		t.Fatalf("LookupReplyMessage returned nil for a live entry")
	}
	if got.SentID != "msg-clean" {
		t.Errorf("SentID = %q, want msg-clean", got.SentID)
	}
	if got.Clean != 1 {
		t.Errorf("Clean = %d, want 1 (inherited by monsterChanged dispatch)", got.Clean)
	}
	if got.Type != "discord:user" {
		t.Errorf("Type = %q, want discord:user", got.Type)
	}

	// Misses return nil.
	if got := mt.LookupReplyMessage("enc-x", "userB"); got != nil {
		t.Errorf("LookupReplyMessage cross-target = %+v, want nil", got)
	}
	if got := mt.LookupReplyMessage("", "userA"); got != nil {
		t.Errorf("LookupReplyMessage empty key = %+v, want nil", got)
	}
}

func TestLookupReplyTargets_EnumeratesAllAndEvicts(t *testing.T) {
	mt, _ := newTestTracker(t)

	mt.Track("edit-1", &TrackedMessage{
		SentID: "msg-1", Target: "userA", Type: "discord:user", ReplyKey: "enc-1",
	}, time.Hour)
	mt.Track("edit-2", &TrackedMessage{
		SentID: "msg-2", Target: "userB", Type: "discord:user", ReplyKey: "enc-1",
	}, time.Hour)
	mt.Track("edit-3", &TrackedMessage{
		SentID: "msg-3", Target: "userC", Type: "discord:user", ReplyKey: "enc-other",
	}, time.Hour)

	got := mt.LookupReplyTargets("enc-1")
	if len(got) != 2 {
		t.Fatalf("LookupReplyTargets = %v, want 2 targets (userA + userB)", got)
	}
	gotSet := map[string]bool{got[0]: true, got[1]: true}
	if !gotSet["userA"] || !gotSet["userB"] {
		t.Errorf("LookupReplyTargets missing expected targets, got %v", got)
	}

	// Cross-key: enc-other should only return userC.
	if got := mt.LookupReplyTargets("enc-other"); len(got) != 1 || got[0] != "userC" {
		t.Errorf("LookupReplyTargets(enc-other) = %v, want [userC]", got)
	}

	// Empty key returns nil (defensive).
	if got := mt.LookupReplyTargets(""); got != nil {
		t.Errorf("LookupReplyTargets(\"\") = %v, want nil", got)
	}

	// Unknown key returns nil.
	if got := mt.LookupReplyTargets("does-not-exist"); got != nil {
		t.Errorf("LookupReplyTargets unknown = %v, want nil", got)
	}
}

// TestLookupReplyTargets_EvictedEntriesDropFromReverseIndex pins the
// reverse-index maintenance: when a replyIndex entry expires, its
// reverse-index slot must be removed too, otherwise LookupReplyTargets
// would return stale targets pointing at SentIDs whose underlying
// messages have already been clean-deleted.
func TestLookupReplyTargets_EvictedEntriesDropFromReverseIndex(t *testing.T) {
	mt, _ := newTestTracker(t)

	mt.Track("edit-short", &TrackedMessage{
		SentID: "msg-short", Target: "userA", Type: "discord:user", ReplyKey: "enc-evict",
	}, 50*time.Millisecond)
	mt.Track("edit-long", &TrackedMessage{
		SentID: "msg-long", Target: "userB", Type: "discord:user", ReplyKey: "enc-evict",
	}, time.Hour)

	if got := mt.LookupReplyTargets("enc-evict"); len(got) != 2 {
		t.Fatalf("pre-eviction LookupReplyTargets = %v, want 2", got)
	}

	time.Sleep(150 * time.Millisecond)

	// userA's entry should have expired; userB should remain.
	got := mt.LookupReplyTargets("enc-evict")
	if len(got) != 1 || got[0] != "userB" {
		t.Errorf("post-eviction LookupReplyTargets = %v, want [userB] only", got)
	}
}

func TestLookupReplyEvictionAlignedWithEditCache(t *testing.T) {
	mt, _ := newTestTracker(t)
	mt.Track("edit-x", &TrackedMessage{
		SentID: "msg-x", Target: "u1", ReplyKey: "rk-evict",
	}, 50*time.Millisecond)

	if got := mt.LookupReply("rk-evict", "u1"); got != "msg-x" {
		t.Fatalf("pre-eviction LookupReply = %q, want msg-x", got)
	}
	time.Sleep(150 * time.Millisecond)
	if got := mt.LookupReply("rk-evict", "u1"); got != "" {
		t.Errorf("post-eviction LookupReply = %q, want empty", got)
	}
}

func TestTrackerSaveLoadPreservesReplyIndex(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSender{}
	senders := map[string]Sender{"discord": mock}

	mt1 := NewMessageTracker(dir, senders)
	mt1.Track("edit-r", &TrackedMessage{
		SentID: "msg-r", Target: "u1", Type: "discord:user", ReplyKey: "rk-save",
	}, time.Hour)
	if err := mt1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	mt1.cache.Stop()

	mt2 := NewMessageTracker(dir, senders)
	if err := mt2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer mt2.cache.Stop()

	if got := mt2.LookupReply("rk-save", "u1"); got != "msg-r" {
		t.Errorf("LookupReply after Load = %q, want msg-r", got)
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
