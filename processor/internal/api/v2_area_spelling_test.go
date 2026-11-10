package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func overrideAreasOf(t *testing.T, rule map[string]any) []string {
	t.Helper()
	raw, _ := rule["override_areas"].([]any)
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i], _ = v.(string)
	}
	return out
}

// TestV2_OverrideAreas_WriteCanonicalReadCanonical: v2 stores and shows the
// fence's own spelling (lowercased, underscores intact), whatever spelling the
// client sent or the row was stored with, and a legacy spaced row round-trips.
func TestV2_OverrideAreas_WriteCanonicalReadCanonical(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	base := "/api/v2/humans/u1/tracking/pokemon"
	want := []string{"gent_centrum"}

	// 1. POST a spaced, mixed-case name: stored and read back as the fence name.
	w := v2DoReq(t, r, http.MethodPost, base+"?silent=true", `[{"pokemon_id":25,"override_areas":["Gent Centrum"]}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	if rows := ms.AllRows(); len(rows) != 1 || !reflect.DeepEqual(rows[0].OverrideAreas, want) {
		t.Fatalf("stored override_areas = %+v, want %v", rows, want)
	}
	if got := overrideAreasOf(t, getOneRule(t, r, base, uid)); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET override_areas = %v, want %v", got, want)
	}

	// 2. A legacy row stored spaced reads back as the fence name. (Fresh API:
	// the row from step 1 would otherwise be a near-duplicate of it.)
	r, ms, _, restore2 := newV2PokemonTestAPI(t)
	defer restore2()
	legacy := v2PokemonRow(77)
	legacy.OverrideAreas = []string{"gent centrum"}
	legacyUID, err := ms.Insert(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	got := getOneRule(t, r, base, legacyUID)
	if a := overrideAreasOf(t, got); !reflect.DeepEqual(a, want) {
		t.Fatalf("legacy GET override_areas = %v, want %v", a, want)
	}

	// 3. POSTing that read back is unchanged (the diff compares by key).
	body, _ := json.Marshal(cloneWithoutUID(got))
	w = v2DoReq(t, r, http.MethodPost, base+"?silent=true", "["+string(body)+"]")
	if w.Code != http.StatusOK {
		t.Fatalf("re-post: %d %s", w.Code, w.Body.String())
	}
	if n := len(tryRules(v2DecodeBody(t, w), "unchanged")); n != 1 {
		t.Fatalf("re-post of legacy row: want 1 unchanged, got %d: %s", n, w.Body.String())
	}

	// 4. PUTting it back is unchanged (compared by key): nothing is rewritten.
	w = v2DoReq(t, r, http.MethodPut, base+"/"+itoa(legacyUID)+"?silent=true", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if n := len(tryRules(v2DecodeBody(t, w), "unchanged")); n != 1 {
		t.Fatalf("put of legacy row: want 1 unchanged, got %d: %s", n, w.Body.String())
	}
	if row := v2RowByUID(t, ms, legacyUID); !reflect.DeepEqual(row.OverrideAreas, []string{"gent centrum"}) {
		t.Fatalf("unchanged PUT rewrote override_areas: %v", row.OverrideAreas)
	}

	// 5. A PUT that changes the rule stores the canonical spelling.
	var changed map[string]any
	_ = json.Unmarshal(body, &changed)
	changed["min_iv"] = 78
	body, _ = json.Marshal(changed)
	w = v2DoReq(t, r, http.MethodPut, base+"/"+itoa(legacyUID)+"?silent=true", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if row := v2RowByUID(t, ms, legacyUID); !reflect.DeepEqual(row.OverrideAreas, want) {
		t.Fatalf("PUT stored override_areas = %v, want %v", row.OverrideAreas, want)
	}
}

func TestCanonicalOverrideAreas(t *testing.T) {
	fences := []geofence.Fence{humanSquareFence("alpha", 10, 10, 1), humanSquareFence("Gent_Centrum", 50, 50, 1)}
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences})
	deps := &TrackingDeps{StateMgr: mgr}
	if got := canonicalOverrideAreas(deps, nil); got != nil {
		t.Errorf("nil -> %v, want nil", got)
	}
	if got := canonicalOverrideAreas(deps, []string{}); got != nil {
		t.Errorf("empty -> %v, want nil", got)
	}
	got := canonicalOverrideAreas(deps, []string{"Gent Centrum", "ALPHA", "gent_centrum", "Unknown_Place"})
	if want := []string{"gent_centrum", "alpha", "unknown_place"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (canonical, deduped by key, first-seen order)", got, want)
	}
}
