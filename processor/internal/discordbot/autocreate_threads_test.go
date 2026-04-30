package discordbot

import (
	"path/filepath"
	"testing"

	"github.com/bwmarrin/discordgo"
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

func TestBuildPickerPayload(t *testing.T) {
	picker := &threadPickerDef{
		EmbedTitle:       "Area alerts for {0}",
		EmbedDescription: "Click to join.",
		Pinned:           true,
	}
	threads := []threadCacheEntry{
		{ThreadID: "t1", Label: "Hundo"},
		{ThreadID: "t2", Label: "Nundo"},
	}

	embeds, components := buildPickerPayload("master1", picker, threads, []string{"amsterdam_apollo"})

	if len(embeds) != 1 {
		t.Fatalf("embeds len = %d, want 1", len(embeds))
	}
	if embeds[0].Title != "Area alerts for amsterdam_apollo" {
		t.Errorf("title = %q, want template-expanded", embeds[0].Title)
	}
	if len(components) != 1 {
		t.Fatalf("components len = %d, want one ActionsRow", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("first component not ActionsRow: %T", components[0])
	}
	if len(row.Components) != 2 {
		t.Errorf("buttons = %d, want 2", len(row.Components))
	}
	btn0 := row.Components[0].(discordgo.Button)
	if btn0.Label != "Hundo" {
		t.Errorf("button label = %q, want Hundo", btn0.Label)
	}
	wantID := "poracle:thread:master1:t1:join"
	if btn0.CustomID != wantID {
		t.Errorf("custom_id = %q, want %q", btn0.CustomID, wantID)
	}
}

// TestBuildPickerPayloadHonoursStyle exercises the styled-button path so a
// future refactor that drops the field doesn't silently regress to "all
// buttons are secondary".
func TestBuildPickerPayloadHonoursStyle(t *testing.T) {
	picker := &threadPickerDef{EmbedTitle: "t", EmbedDescription: "d"}
	threads := []threadCacheEntry{
		{ThreadID: "a", Label: "A", Style: "primary"},
		{ThreadID: "b", Label: "B", Style: "success"},
		{ThreadID: "c", Label: "C", Style: "danger"},
		{ThreadID: "d", Label: "D", Style: ""}, // default → secondary
	}
	_, components := buildPickerPayload("m", picker, threads, nil)
	row := components[0].(discordgo.ActionsRow)
	wants := []discordgo.ButtonStyle{
		discordgo.PrimaryButton, discordgo.SuccessButton, discordgo.DangerButton, discordgo.SecondaryButton,
	}
	for i, want := range wants {
		got := row.Components[i].(discordgo.Button).Style
		if got != want {
			t.Errorf("button %d style = %v, want %v", i, got, want)
		}
	}
}

// TestBuildPickerPayloadTruncates ensures we never emit a row with more
// than the Discord ActionsRow limit, matching the warning surfaced to
// admins at autocreate time.
func TestBuildPickerPayloadTruncates(t *testing.T) {
	picker := &threadPickerDef{EmbedTitle: "t", EmbedDescription: "d"}
	threads := make([]threadCacheEntry, 8)
	for i := range threads {
		threads[i] = threadCacheEntry{ThreadID: "t", Label: "L"}
	}
	_, components := buildPickerPayload("m", picker, threads, nil)
	row := components[0].(discordgo.ActionsRow)
	if len(row.Components) != pickerMaxButtons {
		t.Errorf("row buttons = %d, want %d (truncation)", len(row.Components), pickerMaxButtons)
	}
}

// TestThreadCacheSaveCreatesDir confirms save() materialises the cache
// directory on a fresh install rather than failing silently — a real
// regression risk for first-run !autocreate where config/.cache may not
// yet exist.
func TestThreadCacheSaveCreatesDir(t *testing.T) {
	root := t.TempDir()
	// Path nested two directories deep that don't exist yet.
	path := filepath.Join(root, "config", ".cache", "autocreate-threads.json")

	c := newThreadCache(path)
	c.upsertMaster("g1", "m1", "msg1")
	c.upsertThread("m1", threadCacheEntry{ThreadID: "t1", Label: "Hundo"})

	if err := c.save(); err != nil {
		t.Fatalf("save into non-existent dir: %v", err)
	}

	// Reload via a fresh instance to confirm what was persisted is what
	// emitPickerPost would see on the next !autocreate run.
	c2 := newThreadCache(path)
	if err := c2.load(); err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	m, ok := c2.master("m1")
	if !ok || m.PickerMessageID != "msg1" {
		t.Errorf("PickerMessageID = %q after round-trip, want %q", m.PickerMessageID, "msg1")
	}
	if len(m.Threads) != 1 || m.Threads[0].Label != "Hundo" {
		t.Errorf("threads = %+v, want one entry with Label=Hundo", m.Threads)
	}
}
