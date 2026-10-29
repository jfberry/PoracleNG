package api

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func v2PatchJSON(uids []int64, set string) string {
	parts := make([]string, len(uids))
	for i, u := range uids {
		parts[i] = itoa(u)
	}
	return fmt.Sprintf(`{"uids":[%s],"set":%s}`, strings.Join(parts, ","), set)
}

func v2SeedPokemon(t *testing.T, ms *store.MockTrackingStore[db.MonsterTrackingAPI], rows ...db.MonsterTrackingAPI) []int64 {
	t.Helper()
	uids := make([]int64, len(rows))
	for i := range rows {
		uid, err := ms.Insert(&rows[i])
		if err != nil {
			t.Fatal(err)
		}
		uids[i] = uid
	}
	return uids
}

func v2RowByUID(t *testing.T, ms *store.MockTrackingStore[db.MonsterTrackingAPI], uid int64) db.MonsterTrackingAPI {
	t.Helper()
	for _, r := range ms.AllRows() {
		if r.UID == uid {
			return r
		}
	}
	t.Fatalf("no stored row with uid %d", uid)
	return db.MonsterTrackingAPI{}
}

func TestV2Patch_SetsDistanceKeepsUIDAndOtherFields(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	// min_iv is an updatable field, so the rules also differ in max_iv: rules
	// that differ only in min_iv would be one-field updates of each other.
	a, b, c := v2PokemonRow(90), v2PokemonRow(80), v2PokemonRow(70)
	a.MaxIV, b.MaxIV, c.MaxIV = 100, 95, 90
	b.Distance = 100
	uids := v2SeedPokemon(t, ms, a, b, c)

	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath, v2PatchJSON(uids, `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	up := v2RulesArray(t, body, "updated")
	if len(up) != 3 || len(v2RulesArray(t, body, "unchanged")) != 0 || len(v2RulesArray(t, body, "refused")) != 0 {
		t.Fatalf("body = %v", body)
	}
	wantIV := []float64{90, 80, 70}
	wantMaxIV := []any{nil, float64(95), float64(90)} // 100 is the wildcard
	wantPrevDist := []any{nil, float64(100), nil}
	for i, u := range up {
		if u["uid"] != float64(uids[i]) || u["min_iv"] != wantIV[i] || u["max_iv"] != wantMaxIV[i] || u["distance"] != float64(500) {
			t.Fatalf("updated[%d] = %v", i, u)
		}
		prev, _ := u["previous"].(map[string]any)
		if prev == nil || prev["distance"] != wantPrevDist[i] || prev["min_iv"] != wantIV[i] {
			t.Fatalf("updated[%d].previous = %v", i, u["previous"])
		}
	}
	for i, uid := range uids {
		row := v2RowByUID(t, ms, uid)
		if row.Distance != 500 || row.MinIV != int(wantIV[i]) {
			t.Fatalf("stored %d = %+v", uid, row)
		}
	}
	if len(ms.AllRows()) != 3 {
		t.Fatalf("rows = %d", len(ms.AllRows()))
	}
	if n := atomic.LoadInt32(&v2PokemonTestReloads); n != 1 {
		t.Fatalf("reloads = %d", n)
	}
	if len(*pushes) != 1 {
		t.Fatalf("pushes = %v", *pushes)
	}
}

func TestV2Patch_SilentSuppressesPush(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	uids := v2SeedPokemon(t, ms, v2PokemonRow(90))
	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath+"?silent=true", v2PatchJSON(uids, `{"template":"2"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %v", *pushes)
	}
	if v2RowByUID(t, ms, uids[0]).Template != "2" {
		t.Fatalf("template not set")
	}
}

func TestV2Patch_PerRuleRefusals(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	withArea := v2PokemonRow(95)
	withArea.OverrideAreas = []string{"alpha"}
	other := v2PokemonRow(60)
	other.ID = "u2"
	near1, near2 := v2PokemonRow(50), v2PokemonRow(50)
	near1.Distance, near2.Distance = 100, 200
	uids := v2SeedPokemon(t, ms, withArea, other, near2, near1)
	areaUID, otherUID, near2UID, near1UID := uids[0], uids[1], uids[2], uids[3]

	// near1 is listed before near2 in uids, so near1 wins.
	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath+"?silent=true",
		v2PatchJSON([]int64{areaUID, otherUID, near1UID, near2UID}, `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	up := v2RulesArray(t, body, "updated")
	if len(up) != 1 || up[0]["uid"] != float64(near1UID) {
		t.Fatalf("updated = %v", up)
	}
	ref := v2RulesArray(t, body, "refused")
	if len(ref) != 3 {
		t.Fatalf("refused = %v", ref)
	}
	if ref[0]["uid"] != float64(areaUID) || ref[0]["reason"] != "invalid" ||
		!strings.Contains(ref[0]["detail"].(string), "override_areas") {
		t.Fatalf("refused[0] = %v", ref[0])
	}
	if ref[1]["uid"] != float64(otherUID) || ref[1]["reason"] != "not_found" {
		t.Fatalf("refused[1] = %v", ref[1])
	}
	if ref[2]["uid"] != float64(near2UID) || ref[2]["reason"] != "conflict" ||
		ref[2]["conflicts_with"] != float64(near1UID) || ref[2]["detail"] == "" {
		t.Fatalf("refused[2] = %v", ref[2])
	}
	if _, ok := ref[0]["conflicts_with"]; ok {
		t.Fatalf("conflicts_with on a non-conflict refusal: %v", ref[0])
	}

	if got := v2RowByUID(t, ms, areaUID); got.Distance != 0 {
		t.Fatalf("invalid rule written: %+v", got)
	}
	if got := v2RowByUID(t, ms, otherUID); got.Distance != 0 {
		t.Fatalf("other human's rule written: %+v", got)
	}
	if got := v2RowByUID(t, ms, near2UID); got.Distance != 200 {
		t.Fatalf("conflicting rule written: %+v", got)
	}
	if got := v2RowByUID(t, ms, near1UID); got.Distance != 500 {
		t.Fatalf("winner not written: %+v", got)
	}
}

func TestV2Patch_DuplicateUIDsReportedOnce(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	uids := v2SeedPokemon(t, ms, v2PokemonRow(90))
	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath+"?silent=true",
		v2PatchJSON([]int64{uids[0], uids[0]}, `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if len(v2RulesArray(t, body, "updated")) != 1 || len(v2RulesArray(t, body, "refused")) != 0 ||
		len(v2RulesArray(t, body, "unchanged")) != 0 {
		t.Fatalf("body = %v", body)
	}
}

func TestV2Patch_IncidentUIDIsNotFoundForInvasion(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	inc := db.InvasionTrackingAPI{ID: "u1", ProfileNo: 1, GruntType: "showcase"}
	uid, err := is.Insert(&inc)
	if err != nil {
		t.Fatal(err)
	}
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/tracking/invasion?silent=true",
		v2PatchJSON([]int64{uid}, `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	ref := v2RulesArray(t, v2DecodeBody(t, w), "refused")
	if len(ref) != 1 || ref[0]["reason"] != "not_found" {
		t.Fatalf("refused = %v", ref)
	}
}

func TestV2Patch_UnsupportedFieldIs422(t *testing.T) {
	rr, raids, _, restoreRaid := newV2RaidTestAPI(t)
	defer restoreRaid()
	raid := db.RaidTrackingAPI{ID: "u1", ProfileNo: 1, PokemonID: 9000, Level: 5, Team: 4, Exclusive: false}
	raidUID, err := raids.Insert(&raid)
	if err != nil {
		t.Fatal(err)
	}
	before := raids.AllRows()
	w := v2DoReq(t, rr, http.MethodPatch, "/api/v2/humans/u1/tracking/raid", v2PatchJSON([]int64{raidUID}, `{"summary":true}`))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "summary") {
		t.Fatalf("raid summary: %d %s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(raids.AllRows(), before) {
		t.Fatalf("raid rows changed")
	}

	gr, _, _, restoreGym := newV2GymTestAPI(t)
	defer restoreGym()
	w = v2DoReq(t, gr, http.MethodPatch, "/api/v2/humans/u1/tracking/gym", `{"uids":[1],"set":{"edit":true}}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "edit") {
		t.Fatalf("gym edit: %d %s", w.Code, w.Body.String())
	}

	pr, _, _, restorePokemon := newV2PokemonTestAPI(t)
	defer restorePokemon()
	for name, body := range map[string]string{
		"empty set":     `{"uids":[1],"set":{}}`,
		"unknown field": `{"uids":[1],"set":{"min_iv":90}}`,
		"empty uids":    `{"uids":[],"set":{"distance":5}}`,
		"missing set":   `{"uids":[1]}`,
	} {
		if w := v2DoReq(t, pr, http.MethodPatch, v2DryRunPath, body); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestV2Patch_FortHasNoClean(t *testing.T) {
	var probe v2FortRule
	on := true
	if err := applyV2PatchSet(&probe, v2PatchSet{Clean: &on}); err == nil {
		t.Fatalf("clean applied to fort")
	}
	d := 7
	if err := applyV2PatchSet(&probe, v2PatchSet{Distance: &d}); err != nil || probe.Distance == nil || *probe.Distance != 7 {
		t.Fatalf("distance: %v %+v", err, probe)
	}
}

func TestV2Patch_DryRunWritesNothing(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	near1, near2 := v2PokemonRow(50), v2PokemonRow(50)
	near1.Distance, near2.Distance = 100, 200
	distinct := v2PokemonRow(90)
	distinct.MaxIV = 95
	uids := v2SeedPokemon(t, ms, distinct, near1, near2)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath+"?dry_run=true", v2PatchJSON(append(uids, 9999), `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if body["dry_run"] != true {
		t.Fatalf("dry_run = %v", body["dry_run"])
	}
	up := v2RulesArray(t, body, "updated")
	if len(up) != 2 || up[0]["uid"] != float64(uids[0]) || up[0]["distance"] != float64(500) ||
		up[1]["uid"] != float64(uids[1]) || up[0]["previous"] == nil {
		t.Fatalf("updated = %v", up)
	}
	ref := v2RulesArray(t, body, "refused")
	if len(ref) != 2 || ref[0]["reason"] != "conflict" || ref[1]["reason"] != "not_found" {
		t.Fatalf("refused = %v", ref)
	}
	v2AssertUntouched(t, ms, before, pushes)
}

func TestV2Patch_UnchangedWhenAlreadySet(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	templated := v2PokemonRow(90)
	templated.Template = "x"
	legacy := v2PokemonRow(80)
	legacy.OverrideAreas = []string{"gent centrum"}
	uids := v2SeedPokemon(t, ms, templated, legacy)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath, v2PatchJSON(uids[:1], `{"template":"x"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	un := v2RulesArray(t, body, "unchanged")
	if len(un) != 1 || un[0]["uid"] != float64(uids[0]) || len(v2RulesArray(t, body, "updated")) != 0 {
		t.Fatalf("body = %v", body)
	}

	w = v2DoReq(t, r, http.MethodPatch, v2DryRunPath, v2PatchJSON(uids[1:], `{"distance":0,"clean":false}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body = v2DecodeBody(t, w)
	un = v2RulesArray(t, body, "unchanged")
	if len(un) != 1 || un[0]["uid"] != float64(uids[1]) || len(v2RulesArray(t, body, "updated")) != 0 ||
		len(v2RulesArray(t, body, "refused")) != 0 {
		t.Fatalf("legacy spelling body = %v", body)
	}
	v2AssertUntouched(t, ms, before, pushes)
}

// v2EditUnsupported must name exactly the types whose Translate refuses edit,
// since PATCH rejects edit up front from that list.
func TestV2Patch_EditUnsupportedMatchesTranslate(t *testing.T) {
	deps := &TrackingDeps{}
	on := true
	isEditErr := func(f func() error) (got bool) {
		defer func() {
			if recover() != nil {
				got = false
			}
		}()
		err := f()
		return err != nil && strings.Contains(err.Error(), "edit is not supported")
	}
	oc := overrideContext{}
	checks := map[string]func() error{
		"pokemon": func() error {
			_, err := translateV2Pokemon(deps, "u1", 1, oc, &v2PokemonRule{Edit: &on})
			return err
		},
		"raid": func() error { _, err := translateV2Raid(deps, "u1", 1, oc, &v2RaidRule{Edit: &on}); return err },
		"egg":  func() error { _, err := translateV2Egg(deps, "u1", 1, oc, &v2EggRule{Edit: &on}); return err },
		"quest": func() error {
			_, err := translateV2Quest(deps, "u1", 1, oc, &v2QuestRule{Edit: &on})
			return err
		},
		"invasion": func() error {
			_, err := translateV2Invasion(deps, "u1", 1, oc, &v2InvasionRule{Edit: &on})
			return err
		},
		"incident": func() error {
			_, err := translateV2Incident(deps, "u1", 1, oc, &v2IncidentRule{Edit: &on})
			return err
		},
		"lure": func() error { _, err := translateV2Lure(deps, "u1", 1, oc, &v2LureRule{Edit: &on}); return err },
		"nest": func() error { _, err := translateV2Nest(deps, "u1", 1, oc, &v2NestRule{Edit: &on}); return err },
		"gym":  func() error { _, err := translateV2Gym(deps, "u1", 1, oc, &v2GymRule{Edit: &on}); return err },
		"maxbattle": func() error {
			_, err := translateV2Maxbattle(deps, "u1", 1, oc, &v2MaxbattleRule{Edit: &on})
			return err
		},
		"tappable": func() error {
			_, err := translateV2Tappable(deps, "u1", 1, oc, &v2TappableRule{RewardType: 4, Edit: &on})
			return err
		},
	}
	for name, f := range checks {
		if got := isEditErr(f); got != v2EditUnsupported[name] {
			t.Errorf("%s: Translate rejects edit = %v, v2EditUnsupported = %v", name, got, v2EditUnsupported[name])
		}
	}
}
