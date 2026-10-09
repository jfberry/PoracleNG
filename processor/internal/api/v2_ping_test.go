package api

import (
	"net/http"
	"testing"
)

func TestV2Pokemon_PingRoundTrip(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"ping":"<@&123>"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	rows, _ := ms.SelectByIDProfile("u1", 1)
	if len(rows) != 1 || rows[0].Ping != "<@&123>" {
		t.Fatalf("ping not stored: %+v", rows)
	}
	rule := getOneRule(t, r, "/api/v2/humans/u1/tracking/pokemon", uid)
	if rule["ping"] != "<@&123>" {
		t.Fatalf("ping not returned: %v", rule)
	}
}

func TestV2Pokemon_PingNullWhenEmpty(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	rule := getOneRule(t, r, "/api/v2/humans/u1/tracking/pokemon", uid)
	if v, ok := rule["ping"]; !ok || v != nil {
		t.Fatalf("expected present-but-null ping, got %v (present=%v)", v, ok)
	}
}

// Re-posting a rule with the same ping must diff as unchanged (before this
// change the stored ping was always "" so it could never match a pinged rule).
func TestV2Pokemon_PingSurvivesRoundTrip(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"ping":"@here"}]`)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"ping":"@here"}]`)
	body := v2DecodeBody(t, w)
	if len(v2RulesArray(t, body, "unchanged")) != 1 || len(tryRules(body, "created")) != 0 {
		t.Fatalf("expected the identical rule to be unchanged: %v", body)
	}
}
