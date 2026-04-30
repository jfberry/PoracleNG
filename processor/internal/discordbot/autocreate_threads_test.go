package discordbot

import (
	"path/filepath"
	"testing"
)

func TestEncodeThreadJoinID(t *testing.T) {
	got := encodeThreadJoinID("12345", "67890")
	want := "poracle:thread:12345:67890:join"
	if got != want {
		t.Errorf("encodeThreadJoinID = %q, want %q", got, want)
	}
}

func TestDecodeThreadJoinID(t *testing.T) {
	tests := []struct {
		in         string
		wantMaster string
		wantThread string
		wantOK     bool
	}{
		{"poracle:thread:111:222:join", "111", "222", true},
		{"poracle:thread:111:222", "", "", false},
		{"poracle:thread::222:join", "", "", false},
		{"random:button", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		master, thread, ok := decodeThreadJoinID(tc.in)
		if ok != tc.wantOK || master != tc.wantMaster || thread != tc.wantThread {
			t.Errorf("decodeThreadJoinID(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, master, thread, ok, tc.wantMaster, tc.wantThread, tc.wantOK)
		}
	}
}

func TestThreadCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autocreate-threads.json")

	c := &threadCache{path: path}
	if err := c.load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}

	c.upsertMaster("guild1", "master1", "999")
	c.upsertThread("master1", threadCacheEntry{ThreadID: "t1", Label: "Hundo"})
	c.upsertThread("master1", threadCacheEntry{ThreadID: "t2", Label: "Nundo"})

	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2 := &threadCache{path: path}
	if err := c2.load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	m, ok := c2.master("master1")
	if !ok {
		t.Fatal("master not found after reload")
	}
	if m.GuildID != "guild1" || m.PickerMessageID != "999" {
		t.Errorf("master fields = %+v", m)
	}
	if len(m.Threads) != 2 || m.Threads[0].ThreadID != "t1" || m.Threads[1].Label != "Nundo" {
		t.Errorf("threads = %+v", m.Threads)
	}
}

func TestThreadCacheMastersForUser(t *testing.T) {
	c := &threadCache{}
	c.upsertMaster("g1", "m1", "")
	c.upsertMaster("g1", "m2", "")
	c.upsertThread("m1", threadCacheEntry{ThreadID: "t1"})
	c.upsertThread("m2", threadCacheEntry{ThreadID: "t2"})

	all := c.allMasters()
	if len(all) != 2 {
		t.Errorf("allMasters len = %d, want 2", len(all))
	}
}
