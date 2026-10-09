package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func mustJSONArray(t *testing.T, rule map[string]any) string {
	t.Helper()
	b, err := json.Marshal([]map[string]any{rule})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A row stored with the alias wildcards (0 instead of 1/4096/-1) must diff as
// UNCHANGED against its own GET → POST round trip, and as UPDATED (not a new
// rule) when only distance changes.
func TestV2Pokemon_AliasRowRoundTripsUnchanged(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	row := db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 1, PokemonID: 0, Form: 0, Costume: 9000,
		MinIV: 0, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		MaxWeight: 9000000, Rarity: 0, MaxRarity: 6, Size: 0, MaxSize: 5,
		PVPRankingBest: 0, PVPRankingWorst: 0, Template: "1",
	}
	if _, err := ms.Insert(&row); err != nil {
		t.Fatal(err)
	}
	list := v2DecodeBody(t, v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", ""))
	rules := v2RulesArray(t, list, "rules")
	same := cloneWithoutUID(rules[0])

	body := v2DecodeBody(t, v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", mustJSONArray(t, same)))
	if len(tryRules(body, "created")) != 0 || len(tryRules(body, "unchanged")) != 1 {
		t.Fatalf("unchanged round trip should be unchanged, got %v", body)
	}

	same["distance"] = 500
	body = v2DecodeBody(t, v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", mustJSONArray(t, same)))
	if len(tryRules(body, "created")) != 0 || len(tryRules(body, "updated")) != 1 {
		t.Fatalf("distance-only change should be an update, got %v", body)
	}
	rows, _ := ms.SelectByIDProfile("u1", 1)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one rule after update, got %d", len(rows))
	}
}

// pvp_ranking_worst 0 is a wildcard only with no league. With a league set the
// matcher drops every rank above it, so the rule matches nothing; reading it as
// null, or canonicalising it to 4096 for the diff, would make an edit rewrite a
// dead rule as "any rank".
func TestV2Pokemon_PVPWorstZeroAliasOnlyWithoutLeague(t *testing.T) {
	for _, tc := range []struct {
		name          string
		league, worst int
		wantRead      *int
		wantCanonical int
	}{
		{"no league, 0 is the wildcard", 0, 0, nil, 4096},
		{"no league, 4096 is the wildcard", 0, 4096, nil, 4096},
		{"league, 0 matches nothing", 1500, 0, new(0), 0},
		{"league, explicit rank", 1500, 100, new(100), 100},
		{"league, 4096 is the wildcard", 1500, 4096, nil, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := db.MonsterTrackingAPI{PVPRankingLeague: tc.league, PVPRankingWorst: tc.worst, PVPRankingBest: 1}
			got := pokemonRowToRule(&row).PVPRankingWorst
			if (got == nil) != (tc.wantRead == nil) || (got != nil && *got != *tc.wantRead) {
				t.Errorf("read pvp_ranking_worst = %v, want %v", derefOrNil(got), derefOrNil(tc.wantRead))
			}
			canonicalizePokemonRow(&row)
			if row.PVPRankingWorst != tc.wantCanonical {
				t.Errorf("canonical pvp_ranking_worst = %d, want %d", row.PVPRankingWorst, tc.wantCanonical)
			}
		})
	}
}

func derefOrNil(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
