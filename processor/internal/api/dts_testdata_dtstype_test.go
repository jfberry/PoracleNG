package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// dtsTypeFixture is a realistic testdata.json used to exercise the
// ?dtsType= filter: it mirrors the shapes in fallbacks/testdata.json after
// Task 3 moved the pokestop-incident samples to type "incident" — the
// remaining "pokestop" entries are grunt invasions (invasion, giovanni),
// lures (lure, goldlure), and a content-less showcase envelope that carries
// grunt/display_type fields but no lure fields (classified as invasion by
// the payload-shape split, same as the editor's capture-test-data.mjs).
const dtsTypeFixture = `[
  {"type":"pokemon","test":"hundo","location":"x","webhook":{"pokemon_id":1,"encounter_id":"e1"}},
  {"type":"raid","test":"level1","location":"x","webhook":{"pokemon_id":921,"gym_id":"g1"}},
  {"type":"raid","test":"egg1","location":"x","webhook":{"pokemon_id":0,"gym_id":"g1","level":5}},
  {"type":"pokestop","test":"invasion","location":"x","webhook":{"grunt_type":20,"character":20,"display_type":1,"pokestop_id":"p1"}},
  {"type":"pokestop","test":"giovanni","location":"x","webhook":{"incident_grunt_type":44,"pokestop_id":"p2"}},
  {"type":"pokestop","test":"lure","location":"x","webhook":{"lure_id":502,"lure_expiration":123,"pokestop_id":"p3"}},
  {"type":"pokestop","test":"goldlure","location":"x","webhook":{"lure_id":506,"lure_expiration":456,"pokestop_id":"p4"}},
  {"type":"pokestop","test":"showcase","location":"x","webhook":{"grunt_type":0,"character":0,"display_type":9,"pokestop_id":"p5"}},
  {"type":"incident","test":"kecleon","location":"x","webhook":{"grunt_type":0,"display_type":8,"pokestop_id":"p6"}},
  {"type":"monster_changed","test":"ditto-reveal","location":"x","webhook":{"old":{},"new":{}}}
]`

func newDTSTypeTestdataAPI(t *testing.T) func(query string) map[string]any {
	t.Helper()
	r, api := newDTSTestAPI(t)
	fallbackDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fallbackDir, "testdata.json"), []byte(dtsTypeFixture), 0644); err != nil {
		t.Fatal(err)
	}
	RegisterDTSTestdata(api, t.TempDir(), fallbackDir)

	return func(query string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/dts/testdata"+query, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body: %s", query, w.Code, w.Body.String())
		}
		return decodeBody(t, w)
	}
}

func testNames(t *testing.T, entries []any) []string {
	t.Helper()
	names := make([]string, len(entries))
	for i, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry %d is not an object: %v", i, e)
		}
		names[i] = m["test"].(string)
	}
	return names
}

func TestHumaDTSTestdata_DtsTypeInvasion(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=invasion")

	td, ok := got["testdata"].([]any)
	if !ok {
		t.Fatalf("testdata missing or wrong shape: %v", got)
	}
	names := testNames(t, td)
	want := map[string]bool{"invasion": true, "giovanni": true, "showcase": true}
	if len(names) != len(want) {
		t.Fatalf("dtsType=invasion returned %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("dtsType=invasion unexpectedly included %q (lure entry leaked into invasion bucket?)", n)
		}
		if n == "lure" || n == "goldlure" {
			t.Errorf("dtsType=invasion must not include lure entry %q", n)
		}
	}
	for i, e := range td {
		m := e.(map[string]any)
		if m["dtsType"] != "invasion" {
			t.Errorf("testdata[%d].dtsType = %v, want %q", i, m["dtsType"], "invasion")
		}
	}
}

func TestHumaDTSTestdata_DtsTypeLure(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=lure")

	td := got["testdata"].([]any)
	names := testNames(t, td)
	want := map[string]bool{"lure": true, "goldlure": true}
	if len(names) != len(want) {
		t.Fatalf("dtsType=lure returned %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("dtsType=lure unexpectedly included %q", n)
		}
	}
	for i, e := range td {
		m := e.(map[string]any)
		if m["dtsType"] != "lure" {
			t.Errorf("testdata[%d].dtsType = %v, want %q", i, m["dtsType"], "lure")
		}
	}
}

func TestHumaDTSTestdata_DtsTypeIncident(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=incident")

	td := got["testdata"].([]any)
	if len(td) != 1 || td[0].(map[string]any)["test"] != "kecleon" {
		t.Fatalf("dtsType=incident = %v, want the moved kecleon sample only", td)
	}
	if td[0].(map[string]any)["dtsType"] != "incident" {
		t.Errorf("dtsType tag = %v, want incident", td[0].(map[string]any)["dtsType"])
	}
}

func TestHumaDTSTestdata_DtsTypeMonster(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=monster")

	td := got["testdata"].([]any)
	if len(td) != 1 || td[0].(map[string]any)["test"] != "hundo" {
		t.Fatalf("dtsType=monster = %v, want the pokemon sample only", td)
	}
	if td[0].(map[string]any)["type"] != "pokemon" {
		t.Errorf("testdata[0].type = %v, want pokemon (webhook type preserved)", td[0].(map[string]any)["type"])
	}
}

func TestHumaDTSTestdata_DtsTypeMonsterChanged(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=monsterChanged")

	td := got["testdata"].([]any)
	if len(td) != 1 || td[0].(map[string]any)["test"] != "ditto-reveal" {
		t.Fatalf("dtsType=monsterChanged = %v, want the monster_changed sample only", td)
	}
}

func TestHumaDTSTestdata_DtsTypeEggVsRaid(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)

	eggGot := do("?dtsType=egg")
	eggTd := eggGot["testdata"].([]any)
	if len(eggTd) != 1 || eggTd[0].(map[string]any)["test"] != "egg1" {
		t.Fatalf("dtsType=egg = %v, want only the pokemon_id==0 raid sample", eggTd)
	}
	if eggTd[0].(map[string]any)["dtsType"] != "egg" {
		t.Errorf("dtsType tag = %v, want egg", eggTd[0].(map[string]any)["dtsType"])
	}

	raidGot := do("?dtsType=raid")
	raidTd := raidGot["testdata"].([]any)
	if len(raidTd) != 1 || raidTd[0].(map[string]any)["test"] != "level1" {
		t.Fatalf("dtsType=raid = %v, want only the pokemon_id>0 raid sample", raidTd)
	}
}

// TestHumaDTSTestdata_LegacyTypeUnchanged locks in that ?type=<webhookType>
// keeps returning every entry of that webhook type (no invasion/lure or
// raid/egg split) — the pre-existing back-compat behavior.
func TestHumaDTSTestdata_LegacyTypeUnchanged(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?type=pokestop")

	td := got["testdata"].([]any)
	if len(td) != 5 {
		t.Fatalf("type=pokestop returned %d entries, want all 5 pokestop-typed entries: %v", len(td), td)
	}
	for i, e := range td {
		m := e.(map[string]any)
		if _, tagged := m["dtsType"]; tagged {
			t.Errorf("testdata[%d] unexpectedly carries a dtsType tag under legacy ?type= filtering: %v", i, m)
		}
	}
}

// TestHumaDTSTestdata_TypesMap asserts the discoverable DTS-type->source map
// is present in the response so the editor can drop its hardcoded copy.
func TestHumaDTSTestdata_TypesMap(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("")

	types, ok := got["types"].(map[string]any)
	if !ok {
		t.Fatalf("types missing or wrong shape: %v", got)
	}
	for _, name := range []string{"monster", "egg", "invasion", "lure", "incident", "monsterChanged"} {
		entry, ok := types[name].(map[string]any)
		if !ok {
			t.Fatalf("types[%q] missing or wrong shape: %v", name, types[name])
		}
		if _, ok := entry["webhookType"]; !ok {
			t.Errorf("types[%q].webhookType missing", name)
		}
		if _, ok := entry["derived"]; !ok {
			t.Errorf("types[%q].derived missing", name)
		}
	}
	invasion := types["invasion"].(map[string]any)
	if invasion["webhookType"] != "pokestop" {
		t.Errorf("types[invasion].webhookType = %v, want pokestop", invasion["webhookType"])
	}
	incident := types["incident"].(map[string]any)
	if incident["derived"] != true {
		t.Errorf("types[incident].derived = %v, want true", incident["derived"])
	}
}

func TestHumaDTSTestdata_DtsTypeUnknown(t *testing.T) {
	do := newDTSTypeTestdataAPI(t)
	got := do("?dtsType=not-a-real-type")

	td, ok := got["testdata"].([]any)
	if ok && len(td) != 0 {
		t.Fatalf("dtsType=<unknown> testdata = %v, want empty", td)
	}
}
