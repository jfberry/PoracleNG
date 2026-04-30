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

	c.ensureMaster("guild1", "master1")
	c.setPickerMessageIDs("master1", []string{"999"})
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
	if m.GuildID != "guild1" || len(m.PickerMessageIDs) != 1 || m.PickerMessageIDs[0] != "999" {
		t.Errorf("master fields = %+v", m)
	}
	if len(m.Threads) != 2 || m.Threads[0].ThreadID != "t1" || m.Threads[1].Label != "Nundo" {
		t.Errorf("threads = %+v", m.Threads)
	}
}

func TestThreadCacheMastersForUser(t *testing.T) {
	c := &threadCache{}
	c.ensureMaster("g1", "m1")
	c.ensureMaster("g1", "m2")
	c.upsertThread("m1", threadCacheEntry{ThreadID: "t1"})
	c.upsertThread("m2", threadCacheEntry{ThreadID: "t2"})

	all := c.allMasters()
	if len(all) != 2 {
		t.Errorf("allMasters len = %d, want 2", len(all))
	}
}

// TestBuildPickerMessagesSingle covers the small-master case: a handful
// of threads fit in one message with one ActionsRow and the embed.
func TestBuildPickerMessagesSingle(t *testing.T) {
	picker := &threadPickerDef{
		EmbedTitle:       "Area alerts for {0}",
		EmbedDescription: "Click to join.",
		Pinned:           true,
	}
	threads := []threadCacheEntry{
		{ThreadID: "t1", Label: "Hundo"},
		{ThreadID: "t2", Label: "Nundo"},
	}

	messages := buildPickerMessages("master1", picker, threads, []string{"amsterdam_apollo"})
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	msg := messages[0]
	if len(msg.Embeds) != 1 || msg.Embeds[0].Title != "Area alerts for amsterdam_apollo" {
		t.Errorf("embed = %+v, want template-expanded title", msg.Embeds)
	}
	if len(msg.Components) != 1 {
		t.Fatalf("rows = %d, want 1", len(msg.Components))
	}
	row := msg.Components[0].(discordgo.ActionsRow)
	if len(row.Components) != 2 {
		t.Errorf("buttons = %d, want 2", len(row.Components))
	}
	btn0 := row.Components[0].(discordgo.Button)
	if btn0.Label != "Hundo" || btn0.CustomID != "poracle:thread:master1:t1:join" {
		t.Errorf("button[0] = %+v", btn0)
	}
}

// TestBuildPickerMessagesHonoursStyle exercises the styled-button path so a
// future refactor that drops the field doesn't silently regress to "all
// buttons are secondary".
func TestBuildPickerMessagesHonoursStyle(t *testing.T) {
	picker := &threadPickerDef{EmbedTitle: "t", EmbedDescription: "d"}
	threads := []threadCacheEntry{
		{ThreadID: "a", Label: "A", Style: "primary"},
		{ThreadID: "b", Label: "B", Style: "success"},
		{ThreadID: "c", Label: "C", Style: "danger"},
		{ThreadID: "d", Label: "D", Style: ""}, // default → secondary
	}
	messages := buildPickerMessages("m", picker, threads, nil)
	row := messages[0].Components[0].(discordgo.ActionsRow)
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

// TestBuildPickerMessagesRowChunking exercises the in-message chunking:
// 12 threads fit in one message but spill into multiple ActionsRows of
// 5 buttons (5 + 5 + 2).
func TestBuildPickerMessagesRowChunking(t *testing.T) {
	picker := &threadPickerDef{EmbedTitle: "t", EmbedDescription: "d"}
	threads := make([]threadCacheEntry, 12)
	for i := range threads {
		threads[i] = threadCacheEntry{ThreadID: "t", Label: "L"}
	}
	messages := buildPickerMessages("m", picker, threads, nil)
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	rows := messages[0].Components
	wantRowSizes := []int{5, 5, 2}
	if len(rows) != len(wantRowSizes) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantRowSizes))
	}
	for i, want := range wantRowSizes {
		got := len(rows[i].(discordgo.ActionsRow).Components)
		if got != want {
			t.Errorf("row %d size = %d, want %d", i, got, want)
		}
	}
}

// TestBuildPickerMessagesMultipleMessages confirms that 30 threads spill
// into two messages: the first has the embed and 25 buttons across 5
// rows, the second has only the remaining 5 buttons in one row and no
// embed.
func TestBuildPickerMessagesMultipleMessages(t *testing.T) {
	picker := &threadPickerDef{EmbedTitle: "Title", EmbedDescription: "Desc"}
	threads := make([]threadCacheEntry, 30)
	for i := range threads {
		threads[i] = threadCacheEntry{ThreadID: "t", Label: "L"}
	}
	messages := buildPickerMessages("m", picker, threads, nil)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}

	// First message: embed + 5 rows of 5 = 25 buttons.
	first := messages[0]
	if len(first.Embeds) != 1 {
		t.Errorf("first.Embeds len = %d, want 1", len(first.Embeds))
	}
	if len(first.Components) != 5 {
		t.Errorf("first.Components rows = %d, want 5", len(first.Components))
	}
	totalFirst := 0
	for _, r := range first.Components {
		totalFirst += len(r.(discordgo.ActionsRow).Components)
	}
	if totalFirst != 25 {
		t.Errorf("first message buttons = %d, want 25", totalFirst)
	}

	// Second message: no embed, 1 row of 5 = 5 buttons.
	second := messages[1]
	if len(second.Embeds) != 0 {
		t.Errorf("second.Embeds = %v, want none (only first carries the title)", second.Embeds)
	}
	if len(second.Components) != 1 {
		t.Errorf("second.Components rows = %d, want 1", len(second.Components))
	}
	totalSecond := len(second.Components[0].(discordgo.ActionsRow).Components)
	if totalSecond != 5 {
		t.Errorf("second message buttons = %d, want 5", totalSecond)
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
	c.ensureMaster("g1", "m1")
	c.setPickerMessageIDs("m1", []string{"msg1"})
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
	if !ok || len(m.PickerMessageIDs) != 1 || m.PickerMessageIDs[0] != "msg1" {
		t.Errorf("PickerMessageIDs = %v after round-trip, want [msg1]", m.PickerMessageIDs)
	}
	if len(m.Threads) != 1 || m.Threads[0].Label != "Hundo" {
		t.Errorf("threads = %+v, want one entry with Label=Hundo", m.Threads)
	}
}
