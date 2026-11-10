package api

import (
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
)

const v2DryRunPath = "/api/v2/humans/u1/tracking/pokemon"

func v2SeedIV90(t *testing.T, r http.Handler) {
	t.Helper()
	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?silent=true", `[{"pokemon_id":25,"min_iv":90}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
}

func TestV2Create_UpdatedCarriesPrevious(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2SeedIV90(t, r)

	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?silent=true&include_descriptions=true", `[{"pokemon_id":25,"min_iv":80}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	up := v2RulesArray(t, body, "updated")
	if len(up) != 1 {
		t.Fatalf("updated = %v", up)
	}
	prev, ok := up[0]["previous"].(map[string]any)
	if !ok {
		t.Fatalf("no previous: %v", up[0])
	}
	if prev["min_iv"] != float64(90) || prev["uid"] != up[0]["uid"] {
		t.Fatalf("previous = %v", prev)
	}
	if _, ok := prev["description"]; !ok {
		t.Fatalf("previous lacks description: %v", prev)
	}
	if _, has := prev["previous"]; has {
		t.Fatalf("previous nested: %v", prev)
	}
	if _, has := body["dry_run"]; has {
		t.Fatalf("dry_run present on a real commit: %v", body)
	}
}

func TestV2Create_DryRunWritesNothing(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2SeedIV90(t, r)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?dry_run=true", `[{"pokemon_id":25,"min_iv":80},{"pokemon_id":150}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if body["dry_run"] != true {
		t.Fatalf("dry_run = %v", body["dry_run"])
	}
	up := v2RulesArray(t, body, "updated")
	if len(up) != 1 {
		t.Fatalf("updated = %v", up)
	}
	prev, _ := up[0]["previous"].(map[string]any)
	if prev == nil || prev["min_iv"] != float64(90) {
		t.Fatalf("previous = %v", up[0]["previous"])
	}
	cr := v2RulesArray(t, body, "created")
	if len(cr) != 1 || cr[0]["uid"] != float64(0) {
		t.Fatalf("created = %v", cr)
	}
	after := ms.AllRows()
	if len(after) != len(before) || !reflect.DeepEqual(after, before) {
		t.Fatalf("store changed: %+v -> %+v", before, after)
	}
	if n := atomic.LoadInt32(&v2PokemonTestReloads); n != 0 {
		t.Fatalf("reloads = %d", n)
	}
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %v", *pushes)
	}
}

func TestV2Create_DryRunValidationMatchesCommit(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?dry_run=true", `[{"pokemon_id":25,"gender":"bogus"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", w.Code, w.Body.String())
	}
}

func TestV2Create_CommitReportsActualReplacement(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2SeedIV90(t, r)

	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?dry_run=true", `[{"pokemon_id":25,"min_iv":80}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// Intervening write: the stored rule becomes IV70, keeping its uid.
	row := ms.AllRows()[0]
	uid := row.UID
	if err := ms.DeleteByUID("u1", uid); err != nil {
		t.Fatal(err)
	}
	row.MinIV = 70
	nu, _ := ms.Insert(&row)
	if err := ms.ReassignUID("u1", nu, uid); err != nil {
		t.Fatal(err)
	}

	w = v2DoReq(t, r, http.MethodPost, v2DryRunPath+"?silent=true", `[{"pokemon_id":25,"min_iv":80}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	up := v2RulesArray(t, v2DecodeBody(t, w), "updated")
	if len(up) != 1 {
		t.Fatalf("updated = %v", up)
	}
	prev, _ := up[0]["previous"].(map[string]any)
	if prev == nil || prev["min_iv"] != float64(70) {
		t.Fatalf("previous = %v, want min_iv 70", up[0]["previous"])
	}
}
