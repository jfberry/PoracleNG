package tracker

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func sortQuests(qs []BufferedQuest) {
	sort.Slice(qs, func(i, j int) bool {
		if qs[i].PokestopID != qs[j].PokestopID {
			return qs[i].PokestopID < qs[j].PokestopID
		}
		if qs[i].RewardType != qs[j].RewardType {
			return qs[i].RewardType < qs[j].RewardType
		}
		if qs[i].Reward != qs[j].Reward {
			return qs[i].Reward < qs[j].Reward
		}
		return !qs[i].WithAR && qs[j].WithAR
	})
}

func TestSummaryBuffer_AppendAndList(t *testing.T) {
	sb := NewSummaryBuffer("")

	q1 := BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "stop-A", Payload: []byte(`{"a":1}`), ExpiresAt: 100, CreatedAt: 50}
	q2 := BufferedQuest{RewardType: 2, Reward: 1301, PokestopID: "stop-B", Payload: []byte(`{"b":2}`), ExpiresAt: 200, CreatedAt: 60}

	sb.Append("user-1", "quest", q1)
	sb.Append("user-1", "quest", q2)

	got := sb.List("user-1", "quest")
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	sortQuests(got)
	if got[0].PokestopID != "stop-A" || got[1].PokestopID != "stop-B" {
		t.Errorf("List ordering: %+v", got)
	}

	// Distinct alertType bucket is independent.
	if got := sb.List("user-1", "raid"); len(got) != 0 {
		t.Errorf("raid bucket should be empty, got %v", got)
	}
	// Unknown user returns empty.
	if got := sb.List("ghost", "quest"); len(got) != 0 {
		t.Errorf("unknown user bucket should be empty, got %v", got)
	}
}

func TestSummaryBuffer_Dedup(t *testing.T) {
	sb := NewSummaryBuffer("")

	first := BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "stop-A", WithAR: false, Payload: []byte("first"), ExpiresAt: 100, CreatedAt: 50}
	second := BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "stop-A", WithAR: false, Payload: []byte("second"), ExpiresAt: 999, CreatedAt: 500}

	sb.Append("user-1", "quest", first)
	sb.Append("user-1", "quest", second)

	got := sb.List("user-1", "quest")
	if len(got) != 1 {
		t.Fatalf("dedup: len = %d, want 1", len(got))
	}
	if string(got[0].Payload) != "second" || got[0].ExpiresAt != 999 || got[0].CreatedAt != 500 {
		t.Errorf("dedup: latest write should win, got %+v", got[0])
	}

	// WithAR=true is a different key.
	withAR := BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "stop-A", WithAR: true, Payload: []byte("ar"), ExpiresAt: 1000, CreatedAt: 600}
	sb.Append("user-1", "quest", withAR)

	got = sb.List("user-1", "quest")
	if len(got) != 2 {
		t.Fatalf("WithAR variant should add a slot: len = %d, want 2", len(got))
	}
}

func TestSummaryBuffer_Clear(t *testing.T) {
	sb := NewSummaryBuffer("")

	q := BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "stop-A", ExpiresAt: 100}
	sb.Append("user-1", "quest", q)
	sb.Append("user-1", "raid", q)
	sb.Append("user-2", "quest", q)

	sb.Clear("user-1", "quest")

	if got := sb.List("user-1", "quest"); len(got) != 0 {
		t.Errorf("Clear should empty target bucket, got %v", got)
	}
	if got := sb.List("user-1", "raid"); len(got) != 1 {
		t.Errorf("Clear leaked into other alertType: %v", got)
	}
	if got := sb.List("user-2", "quest"); len(got) != 1 {
		t.Errorf("Clear leaked into other user: %v", got)
	}

	// Clearing a missing bucket is silent.
	sb.Clear("ghost", "quest")
}

func TestSummaryBuffer_SweepExpired(t *testing.T) {
	sb := NewSummaryBuffer("")

	sb.Append("user-1", "quest", BufferedQuest{PokestopID: "old1", ExpiresAt: 50})
	sb.Append("user-1", "quest", BufferedQuest{PokestopID: "old2", ExpiresAt: 99})
	sb.Append("user-1", "quest", BufferedQuest{PokestopID: "fresh", ExpiresAt: 200})
	sb.Append("user-2", "quest", BufferedQuest{PokestopID: "old3", ExpiresAt: 10})

	removed := sb.SweepExpired(100)
	if removed != 3 {
		t.Errorf("SweepExpired returned %d, want 3", removed)
	}

	got := sb.List("user-1", "quest")
	if len(got) != 1 || got[0].PokestopID != "fresh" {
		t.Errorf("after sweep user-1: %+v", got)
	}
	// user-2 bucket should be empty (and pruned).
	if got := sb.List("user-2", "quest"); len(got) != 0 {
		t.Errorf("user-2 bucket should be empty, got %v", got)
	}

	// Boundary: ExpiresAt == asOf is NOT removed (strict less-than).
	boundary := NewSummaryBuffer("")
	boundary.Append("user-3", "quest", BufferedQuest{PokestopID: "edge", ExpiresAt: 500})
	if removed := boundary.SweepExpired(500); removed != 0 {
		t.Errorf("boundary sweep removed %d, want 0", removed)
	}
	if got := boundary.List("user-3", "quest"); len(got) != 1 {
		t.Errorf("boundary entry should remain, got %v", got)
	}
}

func TestSummaryBuffer_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary-buffer.json")

	src := NewSummaryBuffer(path)
	src.Append("user-1", "quest", BufferedQuest{
		RewardType: 7, Reward: 25, PokestopID: "stop-A", WithAR: false,
		Payload: []byte(`{"a":1}`), ExpiresAt: 100, CreatedAt: 50,
	})
	src.Append("user-1", "quest", BufferedQuest{
		RewardType: 2, Reward: 1301, PokestopID: "stop-B", WithAR: true,
		Payload: []byte(`{"b":2}`), ExpiresAt: 200, CreatedAt: 60,
	})
	src.Append("user-2", "raid", BufferedQuest{
		RewardType: 1, Reward: 1, PokestopID: "stop-C",
		Payload: []byte(`{"c":3}`), ExpiresAt: 300, CreatedAt: 70,
	})

	if err := src.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dst := NewSummaryBuffer(path)
	if err := dst.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	a := dst.List("user-1", "quest")
	if len(a) != 2 {
		t.Errorf("user-1/quest len = %d, want 2", len(a))
	}
	sortQuests(a)
	if a[0].PokestopID != "stop-A" || string(a[0].Payload) != `{"a":1}` || a[0].WithAR {
		t.Errorf("user-1/quest[0] = %+v", a[0])
	}
	if a[1].PokestopID != "stop-B" || !a[1].WithAR {
		t.Errorf("user-1/quest[1] = %+v", a[1])
	}

	b := dst.List("user-2", "raid")
	if len(b) != 1 || b[0].PokestopID != "stop-C" {
		t.Errorf("user-2/raid = %+v", b)
	}

	// Re-appending an existing key on the loaded buffer dedups correctly
	// (i.e. the bufferKey was reconstructed from the snapshot).
	dst.Append("user-1", "quest", BufferedQuest{
		RewardType: 7, Reward: 25, PokestopID: "stop-A", WithAR: false,
		Payload: []byte("updated"), ExpiresAt: 999, CreatedAt: 999,
	})
	a = dst.List("user-1", "quest")
	if len(a) != 2 {
		t.Errorf("after re-append len = %d, want 2 (dedup broken)", len(a))
	}
}

func TestSummaryBuffer_LoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	sb := NewSummaryBuffer(path)
	if err := sb.Load(); err != nil {
		t.Fatalf("Load missing file should be silent, got %v", err)
	}
	if got := sb.List("user-1", "quest"); len(got) != 0 {
		t.Errorf("buffer should be empty after Load of missing file, got %v", got)
	}
}

func TestSummaryBuffer_LoadMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	sb := NewSummaryBuffer(path)
	// Must not panic and must not return an error — startup cannot block
	// on a corrupt snapshot.
	if err := sb.Load(); err != nil {
		t.Errorf("malformed Load returned error: %v", err)
	}
	if got := sb.List("user-1", "quest"); len(got) != 0 {
		t.Errorf("malformed Load should yield empty buffer, got %v", got)
	}
}

func TestSummaryBuffer_SaveLoadEmpty(t *testing.T) {
	// "" path disables persistence — both should be no-ops.
	sb := NewSummaryBuffer("")
	if err := sb.Save(); err != nil {
		t.Errorf("Save with empty path: %v", err)
	}
	if err := sb.Load(); err != nil {
		t.Errorf("Load with empty path: %v", err)
	}
}
