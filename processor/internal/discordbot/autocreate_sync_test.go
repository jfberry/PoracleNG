package discordbot

import (
	"path/filepath"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func TestSyncRule_FilterSelectsFences(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "Belgium", Properties: map[string]any{"server": "uk"}},
		{Name: "Bruges", Group: "Belgium", Properties: map[string]any{"server": "ie"}},
		{Name: "Antwerp", Group: "Belgium", Properties: map[string]any{"server": "uk"}},
	}
	rule := config.AutocreateRule{
		Name:     "uk-areas",
		Guild:    "g1",
		Template: "area",
		Filter:   `{{eq server "uk"}}`,
		Params:   []string{"{{group}}", "{{name}}"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 2 {
		t.Fatalf("expected 2 fences after filter, got %d", len(res.toCreate))
	}
	wantNames := map[string]bool{"Gent_centrum": true, "Antwerp": true}
	for _, c := range res.toCreate {
		if !wantNames[c.fence.Name] {
			t.Errorf("unexpected fence %q in create set", c.fence.Name)
		}
	}
}

func TestSyncRule_ClassifiesReusedAndOrphan(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "Belgium"},
		{Name: "Antwerp", Group: "Belgium"},
	}
	state := autocreateRuleState{
		Fences: map[string]*autocreateFenceState{
			"Gent_centrum": {ChannelID: "ch_gent"},
			"Bruges":       {ChannelID: "ch_bru"},
		},
	}
	rule := config.AutocreateRule{
		Name:     "uk-areas",
		Guild:    "g1",
		Template: "area",
		Params:   []string{"{{group}}", "{{name}}"},
	}

	res := classifyFences(rule, fences, state)

	if len(res.toCreate) != 1 || res.toCreate[0].fence.Name != "Antwerp" {
		t.Errorf("toCreate = %+v, want only Antwerp", namesOf(res.toCreate))
	}
	if len(res.toReuse) != 1 || res.toReuse[0].fence.Name != "Gent_centrum" {
		t.Errorf("toReuse = %+v, want only Gent_centrum", namesOf(res.toReuse))
	}
	if len(res.orphans) != 1 || res.orphans[0] != "Bruges" {
		t.Errorf("orphans = %v, want [Bruges]", res.orphans)
	}
}

func TestSyncRule_FilterErrorSkipsFence(t *testing.T) {
	fences := []geofence.Fence{{Name: "Gent_centrum"}}
	rule := config.AutocreateRule{
		Name:   "x",
		Filter: `{{this is broken handlebars`,
		Params: []string{"{{name}}"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 0 || len(res.toReuse) != 0 {
		t.Errorf("broken filter should skip fence; got create=%d reuse=%d", len(res.toCreate), len(res.toReuse))
	}
	if len(res.skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(res.skipped))
	}
	if res.skipped[0].Fence != "Gent_centrum" {
		t.Errorf("skipped fence name: got %q want %q", res.skipped[0].Fence, "Gent_centrum")
	}
	if res.skipped[0].Reason == "" {
		t.Errorf("skipped reason should not be empty")
	}
}

func TestSyncRule_ParamsErrorSkipsFence(t *testing.T) {
	fences := []geofence.Fence{{Name: "Gent_centrum"}}
	rule := config.AutocreateRule{
		Name:   "x",
		Params: []string{"{{this is also broken"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 0 {
		t.Errorf("expected 0 to-create when params render fails")
	}
	if len(res.skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(res.skipped))
	}
	if res.skipped[0].Fence != "Gent_centrum" {
		t.Errorf("skipped fence name: got %q want %q", res.skipped[0].Fence, "Gent_centrum")
	}
	if res.skipped[0].Reason == "" {
		t.Errorf("skipped reason should not be empty")
	}
}

func namesOf(items []syncFenceCandidate) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.fence.Name
	}
	return out
}

func TestReconcile_DropsMissingChannel(t *testing.T) {
	state := &autocreateRuleState{
		GuildID: "g1",
		Categories: []autocreateCategory{
			{Name: "Belgium", ID: "cat-alive"},
		},
		Fences: map[string]*autocreateFenceState{
			"Gent_centrum": {CategoryID: "cat-alive", ChannelID: "ch-deleted", ThreadIDs: map[string]string{"L": "th-alive"}},
			"Antwerp":      {CategoryID: "cat-alive", ChannelID: "ch-alive", ThreadIDs: map[string]string{"L": "th-deleted"}},
		},
	}
	snap := &guildSnapshot{
		channels: map[string]*discordgo.Channel{
			"cat-alive": {ID: "cat-alive"},
			"ch-alive":  {ID: "ch-alive"},
		},
		threads: map[string]bool{"th-alive": true},
	}

	reconcileCacheAgainstLive(state, snap)

	// Channel deleted → fence's channel_id wiped, threads dropped too.
	if state.Fences["Gent_centrum"].ChannelID != "" {
		t.Error("missing channel_id should be cleared")
	}
	if len(state.Fences["Gent_centrum"].ThreadIDs) != 0 {
		t.Error("threads under a deleted channel should be cleared")
	}

	// Channel alive but thread deleted → only the missing thread dropped.
	if state.Fences["Antwerp"].ChannelID != "ch-alive" {
		t.Error("alive channel should remain")
	}
	if _, present := state.Fences["Antwerp"].ThreadIDs["L"]; present {
		t.Error("deleted thread should be dropped")
	}
}

func TestReconcile_DropsMissingCategory(t *testing.T) {
	state := &autocreateRuleState{
		Categories: []autocreateCategory{
			{Name: "DeadCat", ID: "cat-deleted"},
		},
		Fences: map[string]*autocreateFenceState{
			"Foo": {CategoryID: "cat-deleted", ChannelID: "ch-alive"},
		},
	}
	snap := &guildSnapshot{
		channels: map[string]*discordgo.Channel{
			"ch-alive": {ID: "ch-alive"},
		},
	}

	reconcileCacheAgainstLive(state, snap)

	if len(state.Categories) != 0 {
		t.Error("dead category should be removed from state.Categories")
	}
	// Fence's category_id is wiped (channel can stay if it still exists).
	if state.Fences["Foo"].CategoryID != "" {
		t.Error("fence category_id should be cleared when category is gone")
	}
}

// ---------------------------------------------------------------------------
// applyRemovalSafety unit tests
// ---------------------------------------------------------------------------

func TestRemovalSafety_AllowedBelowThreshold(t *testing.T) {
	// 2/20 = 10%, threshold 20% → allowed
	allowed, note := applyRemovalSafety(20, 2, 20, SyncRuleOptions{})
	if !allowed {
		t.Errorf("expected allowed, got blocked: %s", note)
	}
}

func TestRemovalSafety_BlockedAboveThreshold(t *testing.T) {
	// 5/20 = 25%, threshold 20% → blocked
	allowed, note := applyRemovalSafety(20, 5, 20, SyncRuleOptions{})
	if allowed {
		t.Error("expected blocked, got allowed")
	}
	if note == "" {
		t.Error("note should explain why removal was blocked")
	}
}

func TestRemovalSafety_ForceBypassesThreshold(t *testing.T) {
	// 15/20 = 75%, threshold 20%, but force=true → allowed
	allowed, _ := applyRemovalSafety(20, 15, 20, SyncRuleOptions{Force: true})
	if !allowed {
		t.Error("force=true should bypass safety threshold")
	}
}

func TestRemovalSafety_DryRunBypassesThreshold(t *testing.T) {
	// 15/20 = 75%, threshold 20%, but dry-run → allowed
	allowed, _ := applyRemovalSafety(20, 15, 20, SyncRuleOptions{DryRun: true})
	if !allowed {
		t.Error("dry-run should bypass safety threshold")
	}
}

func TestRemovalSafety_DisabledWhenMaxPercentZero(t *testing.T) {
	// maxPercent=0 → check disabled → allowed regardless of ratio
	allowed, _ := applyRemovalSafety(20, 20, 0, SyncRuleOptions{})
	if !allowed {
		t.Error("maxPercent=0 should disable the safety check")
	}
}

func TestRemovalSafety_SkippedForSmallCache(t *testing.T) {
	// cache < 10 entries → check doesn't engage
	allowed, _ := applyRemovalSafety(9, 9, 20, SyncRuleOptions{})
	if !allowed {
		t.Error("cache < 10 entries should skip the safety check")
	}
}

func TestSyncCacheKey_PathFromBaseDir(t *testing.T) {
	got := syncCachePath("/etc/poracle")
	want := filepath.Join("/etc/poracle", "config", ".cache", "autocreate.json")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// A single rendered params element with internal whitespace expands to
// multiple args; a quoted segment stays as one. Mirrors the bot parser so
// fence names like "Gent Centrum" survive when an admin chooses to render
// them as a single element.
func TestSyncRule_ParamsTokenisedAfterRender(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "Belgium"},
	}
	rule := config.AutocreateRule{
		Name:     "uk-areas",
		Guild:    "g1",
		Template: "area",
		Params:   []string{`{{group}} "{{name}}"`},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 1 {
		t.Fatalf("expected 1 fence in toCreate, got %d", len(res.toCreate))
	}
	got := res.toCreate[0].rawArgs
	want := []string{"Belgium", "Gent_centrum"}
	if len(got) != len(want) {
		t.Fatalf("rawArgs = %v, want %v (length differs)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rawArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGuildSnapshot_FindByName(t *testing.T) {
	snap := &guildSnapshot{
		channels: map[string]*discordgo.Channel{
			"cat1": {ID: "cat1", Name: "Belgium", Type: discordgo.ChannelTypeGuildCategory},
			"ch1":  {ID: "ch1", Name: "gent_centrum", Type: discordgo.ChannelTypeGuildText, ParentID: "cat1"},
			"ch2":  {ID: "ch2", Name: "Antwerp", Type: discordgo.ChannelTypeGuildText, ParentID: "cat1"},
			"ch3":  {ID: "ch3", Name: "top-level", Type: discordgo.ChannelTypeGuildText, ParentID: ""},
		},
		categoriesByLowerName: map[string]string{"belgium": "cat1"},
		channelsByParentLowerName: map[string]map[string]string{
			"cat1": {"gent_centrum": "ch1", "antwerp": "ch2"},
			"":     {"top-level": "ch3"},
		},
	}

	if got := snap.findCategory("Belgium"); got != "cat1" {
		t.Errorf("findCategory case-insensitive: got %q want cat1", got)
	}
	if got := snap.findCategory("BELGIUM"); got != "cat1" {
		t.Errorf("findCategory upper-case: got %q want cat1", got)
	}
	if got := snap.findCategory("Nope"); got != "" {
		t.Errorf("findCategory miss: got %q want empty", got)
	}
	if got := snap.findChannel("cat1", "Antwerp"); got != "ch2" {
		t.Errorf("findChannel under category: got %q want ch2", got)
	}
	if got := snap.findChannel("", "top-level"); got != "ch3" {
		t.Errorf("findChannel top-level: got %q want ch3", got)
	}
	if got := snap.findChannel("cat1", "missing"); got != "" {
		t.Errorf("findChannel miss: got %q want empty", got)
	}
	if !snap.channelExists("ch1") || snap.channelExists("nope") {
		t.Errorf("channelExists wrong")
	}

	// Nil-safe.
	var nilSnap *guildSnapshot
	if nilSnap.findCategory("x") != "" || nilSnap.findChannel("p", "n") != "" || nilSnap.channelExists("x") || nilSnap.threadExists("x") {
		t.Errorf("nil snapshot should return zero values")
	}
}
