package snapshots

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestStore creates a snapshot store in a temp dir, returning a cleanup
// func that closes the store. Tests should call the cleanup via t.Cleanup.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "snapshots")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// mkSnapshot is a minimal valid snapshot builder for tests. messageID +
// target are required to make a valid key; everything else is filler.
func mkSnapshot(messageID, target string) *Snapshot {
	return &Snapshot{
		MessageID:    messageID,
		Target:       target,
		TargetType:   "dm",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		AlertType:    "monster",
		TemplateType: "monster",
		Language:     "en",
		Platform:     "discord",
		View:         map[string]any{"name": "Pikachu", "iv": 100},
	}
}

func TestWriteRead(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	snap := mkSnapshot("123456789", "discord:user:42")
	snap.TrackingUIDs = []int64{1, 2, 3}
	snap.MatchedAreas = []string{"Downtown", "Park"}

	if err := store.Write(ctx, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := store.Read(ctx, snap.Key())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.MessageID != snap.MessageID {
		t.Errorf("MessageID: got %q want %q", got.MessageID, snap.MessageID)
	}
	if got.Target != snap.Target {
		t.Errorf("Target: got %q want %q", got.Target, snap.Target)
	}
	if got.AlertType != snap.AlertType {
		t.Errorf("AlertType: got %q want %q", got.AlertType, snap.AlertType)
	}
	if len(got.TrackingUIDs) != 3 || got.TrackingUIDs[0] != 1 {
		t.Errorf("TrackingUIDs: got %v want [1 2 3]", got.TrackingUIDs)
	}
	if len(got.MatchedAreas) != 2 || got.MatchedAreas[0] != "Downtown" {
		t.Errorf("MatchedAreas: got %v", got.MatchedAreas)
	}
	if got.View["name"] != "Pikachu" {
		t.Errorf("View[name]: got %v want Pikachu", got.View["name"])
	}
	// Version is set by Write — make sure it landed.
	if got.Version != SchemaVersion {
		t.Errorf("Version: got %d want %d", got.Version, SchemaVersion)
	}
}

func TestReadMissing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Read(ctx, "no-such-key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Read missing key: got %v, want ErrNotFound", err)
	}
}

func TestWriteOverwrites(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	snap1 := mkSnapshot("abc", "discord:user:42")
	snap1.View = map[string]any{"version": "first"}
	if err := store.Write(ctx, snap1); err != nil {
		t.Fatalf("Write first: %v", err)
	}

	snap2 := mkSnapshot("abc", "discord:user:42")
	snap2.View = map[string]any{"version": "second"}
	if err := store.Write(ctx, snap2); err != nil {
		t.Fatalf("Write second: %v", err)
	}

	got, err := store.Read(ctx, snap1.Key())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.View["version"] != "second" {
		t.Errorf("expected overwrite; got View=%v", got.View)
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	snap := mkSnapshot("delete-me", "discord:user:42")
	if err := store.Write(ctx, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := store.Delete(ctx, snap.Key()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Read(ctx, snap.Key())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete, Read got %v, want ErrNotFound", err)
	}

	// Delete on missing key is not an error.
	if err := store.Delete(ctx, "no-such-key"); err != nil {
		t.Errorf("Delete missing: got %v, want nil", err)
	}
}

func TestVersionMismatchReturnsMiss(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snapshots")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Inject a record with a higher schema version by writing the raw bytes.
	// The pogrebStore is unexported, so reach in via type assertion.
	s := store.(*pogrebStore)
	future := struct {
		Version   int    `json:"version"`
		MessageID string `json:"messageId"`
		Target    string `json:"target"`
	}{
		Version:   SchemaVersion + 5,
		MessageID: "future",
		Target:    "discord:user:99",
	}
	raw, _ := json.Marshal(future)
	if err := s.db.Put([]byte(MakeKey(future.Target, future.MessageID)), raw); err != nil {
		t.Fatalf("Put raw: %v", err)
	}

	_, err = store.Read(context.Background(), MakeKey(future.Target, future.MessageID))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for newer-version record, got %v", err)
	}
}

func TestCorruptRecordReturnsMiss(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snapshots")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	s := store.(*pogrebStore)
	key := MakeKey("discord:user:1", "corrupt-key")
	if err := s.db.Put([]byte(key), []byte("not json {{{")); err != nil {
		t.Fatalf("Put raw: %v", err)
	}

	_, err = store.Read(context.Background(), key)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for corrupt record, got %v", err)
	}
}

func TestSweepDeletesExpired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Unix()

	// Three records: one fresh, one within grace (should NOT be swept), one
	// past grace (should be swept).
	fresh := mkSnapshot("fresh", "discord:user:42")
	fresh.ExpiresAt = now + 3600
	if err := store.Write(ctx, fresh); err != nil {
		t.Fatalf("Write fresh: %v", err)
	}

	withinGrace := mkSnapshot("within-grace", "discord:user:42")
	withinGrace.ExpiresAt = now - 60
	if err := store.Write(ctx, withinGrace); err != nil {
		t.Fatalf("Write withinGrace: %v", err)
	}

	pastGrace := mkSnapshot("past-grace", "discord:user:42")
	pastGrace.ExpiresAt = now - 86400
	if err := store.Write(ctx, pastGrace); err != nil {
		t.Fatalf("Write pastGrace: %v", err)
	}

	// Threshold = now - 1h: anything with ExpiresAt < threshold is swept.
	threshold := now - 3600
	deleted, err := store.Sweep(ctx, threshold)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Sweep deleted %d, want 1 (only pastGrace)", deleted)
	}

	if _, err := store.Read(ctx, fresh.Key()); err != nil {
		t.Errorf("fresh should survive, got %v", err)
	}
	if _, err := store.Read(ctx, withinGrace.Key()); err != nil {
		t.Errorf("withinGrace should survive, got %v", err)
	}
	if _, err := store.Read(ctx, pastGrace.Key()); !errors.Is(err, ErrNotFound) {
		t.Errorf("pastGrace should be gone, got %v", err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const goroutines = 8
	const writes = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				snap := mkSnapshot("msg", "discord:user:42")
				snap.View = map[string]any{"goroutine": id, "iter": i}
				if err := store.Write(ctx, snap); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// All writers used the same key; the final record exists and is valid.
	got, err := store.Read(ctx, MakeKey("discord:user:42", "msg"))
	if err != nil {
		t.Fatalf("Read after concurrent writes: %v", err)
	}
	if got.View == nil {
		t.Errorf("expected non-nil View after concurrent writes")
	}
}

func TestWriteRejectsMissingIdentity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Write(ctx, nil); err == nil {
		t.Errorf("Write(nil): want error, got nil")
	}
	if err := store.Write(ctx, &Snapshot{Target: "t"}); err == nil {
		t.Errorf("Write missing MessageID: want error, got nil")
	}
	if err := store.Write(ctx, &Snapshot{MessageID: "m"}); err == nil {
		t.Errorf("Write missing Target: want error, got nil")
	}
}

func TestClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snapshots")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, mkSnapshot("m", "t")); !errors.Is(err, ErrClosed) {
		t.Errorf("Write after Close: got %v, want ErrClosed", err)
	}
	if _, err := store.Read(ctx, "k"); !errors.Is(err, ErrClosed) {
		t.Errorf("Read after Close: got %v, want ErrClosed", err)
	}
	if err := store.Delete(ctx, "k"); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete after Close: got %v, want ErrClosed", err)
	}
	if _, err := store.Sweep(ctx, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("Sweep after Close: got %v, want ErrClosed", err)
	}

	// Close is idempotent.
	if err := store.Close(); err != nil {
		t.Errorf("second Close: got %v, want nil", err)
	}
}

func TestKeyFormat(t *testing.T) {
	s := &Snapshot{MessageID: "msg1", Target: "discord:user:42"}
	if k := s.Key(); k != "discord:user:42:msg1" {
		t.Errorf("Key: got %q, want %q", k, "discord:user:42:msg1")
	}
	if k := MakeKey("discord:user:42", "msg1"); k != "discord:user:42:msg1" {
		t.Errorf("MakeKey: got %q, want %q", k, "discord:user:42:msg1")
	}
}
