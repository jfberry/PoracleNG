package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestV2Pokemon_PersonalBooleans(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"personal_shiny":true}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Personal != 1 {
		t.Fatalf("want Personal 1, got %+v", rows)
	}
	uid := rows[0].UID
	g := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	rule := v2RulesArray(t, v2DecodeBody(t, g), "rules")[0]
	if rule["personal_shiny"] != true {
		t.Fatalf("personal_shiny = %v", rule["personal_shiny"])
	}
	if v, ok := rule["personal_background"]; !ok || v != nil {
		t.Fatalf("personal_background want null, got %v (present=%v)", v, ok)
	}

	// PUT without either field: full replace clears.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/pokemon/"+itoa(uid), `{"pokemon_id":25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	rows = ms.AllRows()
	if len(rows) != 1 || rows[0].Personal != 0 {
		t.Fatalf("want Personal 0 after PUT, got %+v", rows)
	}
}

func TestV2Pokemon_PersonalBoth(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"personal_background":true,"personal_shiny":true}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Personal != 3 {
		t.Fatalf("want Personal 3, got %+v", rows)
	}
}

func TestV2Pokemon_PersonalRawFieldRejected(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"personal":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", w.Code, w.Body.String())
	}
}

// PATCH rebuilds each rule through pokemonRowToRule → translateV2Pokemon, so
// the personal bits must survive a delivery-settings-only patch.
func TestV2Patch_KeepsPersonal(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	row := v2PokemonRow(90)
	row.Personal = 3
	uids := v2SeedPokemon(t, ms, row)

	w := v2DoReq(t, r, http.MethodPatch, v2DryRunPath, v2PatchJSON(uids, `{"distance":500}`))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := v2RowByUID(t, ms, uids[0])
	if got.Personal != 3 || got.Distance != 500 {
		t.Fatalf("stored = %+v", got)
	}
}

// A PUT of the rule exactly as read back is unchanged with personal bits set.
func TestV2Put_PersonalReadBodyIsUnchanged(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2DryRunPath, `[{"pokemon_id":25,"personal_shiny":true,"personal_background":true}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	got := getOneRule(t, r, v2DryRunPath, uid)
	if got["personal_shiny"] != true || got["personal_background"] != true {
		t.Fatalf("read = %v", got)
	}
	put, err := json.Marshal(cloneWithoutUID(got))
	if err != nil {
		t.Fatal(err)
	}
	w = v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(uid), string(put))
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if res := v2DecodeBody(t, w); len(tryRules(res, "unchanged")) != 1 {
		t.Fatalf("put body = %v", res)
	}
	if rows := ms.AllRows(); len(rows) != 1 || rows[0].Personal != 3 || rows[0].UID != uid {
		t.Fatalf("rows = %+v", rows)
	}
}
