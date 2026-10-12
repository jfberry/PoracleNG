package api

import (
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
