package discordbot

import (
	"path/filepath"
	"testing"

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

func TestSyncCacheKey_PathFromBaseDir(t *testing.T) {
	got := syncCachePath("/etc/poracle")
	want := filepath.Join("/etc/poracle", "config", ".cache", "autocreate.json")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
