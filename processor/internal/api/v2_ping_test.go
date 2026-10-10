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

// An explicit null ping is accepted and stored as "" (the column is NOT NULL),
// the same as omitting it, and reads back as null.
func TestV2Pokemon_PingExplicitNullStoredEmpty(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"ping":null}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	rows, _ := ms.SelectByIDProfile("u1", 1)
	if len(rows) != 1 || rows[0].Ping != "" {
		t.Fatalf("expected ping stored as \"\", got %+v", rows)
	}
	rule := getOneRule(t, r, "/api/v2/humans/u1/tracking/pokemon", uid)
	if v, ok := rule["ping"]; !ok || v != nil {
		t.Fatalf("expected ping null on read, got %v (present=%v)", v, ok)
	}

	// PUT with null clears a previously set ping.
	put := func(body string) {
		t.Helper()
		rows, _ := ms.SelectByIDProfile("u1", 1)
		if len(rows) != 1 {
			t.Fatalf("expected one rule before PUT, got %d", len(rows))
		}
		w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/pokemon/"+itoa(rows[0].UID), body)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s: expected 200, got %d: %s", body, w.Code, w.Body.String())
		}
	}
	put(`{"pokemon_id":25,"ping":"<@&1>"}`)
	if rows, _ = ms.SelectByIDProfile("u1", 1); len(rows) != 1 || rows[0].Ping != "<@&1>" {
		t.Fatalf("expected PUT to set ping, got %+v", rows)
	}
	put(`{"pokemon_id":25,"ping":null}`)
	if rows, _ = ms.SelectByIDProfile("u1", 1); len(rows) != 1 || rows[0].Ping != "" {
		t.Fatalf("expected PUT null to clear ping, got %+v", rows)
	}
}
