package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// v2RoundTripCase is one tracking type's entry in the exhaustive null
// round-trip table: how to build its test API, its minimal valid body and the
// Req struct whose nullable fields must read back as null.
type v2RoundTripCase struct {
	name string
	api  func(t *testing.T) (http.Handler, func())
	body string
	req  any
}

// wrapV2API adapts a newV2<Type>TestAPI constructor (which returns a typed mock
// store and a push log the round-trip does not need) to the table's shape.
func wrapV2API[S any](f func(t *testing.T) (*gin.Engine, S, *[]pushRecord, func())) func(t *testing.T) (http.Handler, func()) {
	return func(t *testing.T) (http.Handler, func()) {
		r, _, _, restore := f(t)
		return r, restore
	}
}

func v2RoundTripCases() []v2RoundTripCase {
	return []v2RoundTripCase{
		{"pokemon", wrapV2API(newV2PokemonTestAPI), `{}`, v2PokemonRule{}},
		{"raid", wrapV2API(newV2RaidTestAPI), `{}`, v2RaidRule{}},
		{"egg", wrapV2API(newV2EggTestAPI), `{"level":5}`, v2EggRule{}},
		{"quest", wrapV2API(newV2QuestTestAPI), `{"reward_type":3}`, v2QuestRule{}},
		{"invasion", wrapV2API(newV2InvasionTestAPI), `{}`, v2InvasionRule{}},
		{"incident", wrapV2API(newV2IncidentTestAPI), `{"display_type":9}`, v2IncidentRule{}},
		{"lure", wrapV2API(newV2LureTestAPI), `{"lure_id":501}`, v2LureRule{}},
		{"nest", wrapV2API(newV2NestTestAPI), `{}`, v2NestRule{}},
		{"gym", wrapV2API(newV2GymTestAPI), `{"team":"any"}`, v2GymRule{}},
		{"fort", wrapV2API(newV2FortTestAPI), `{}`, v2FortRule{}},
		{"maxbattle", wrapV2API(newV2MaxbattleTestAPI), `{}`, v2MaxbattleRule{}},
		{"tappable", wrapV2API(newV2TappableTestAPI), `{"reward_type":4}`, v2TappableRule{}},
	}
}

// nullableJSONNames lists the JSON names of every field in the Req struct that
// is tagged nullable:"true" or is a slice (slices are nullable by encoding).
func nullableJSONNames(t *testing.T, req any) []string {
	t.Helper()
	typ := reflect.TypeOf(req)
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if f.Tag.Get("nullable") == "true" || f.Type.Kind() == reflect.Slice {
			names = append(names, name)
		}
	}
	return names
}

// TestV2_EveryNullableFieldRoundTripsNull proves, for all 12 tracking types,
// that a rule written with only its required fields reads back with every
// nullable field as null, and that the read body is a fixed point of both PUT
// and POST (unchanged, rule byte-identical). A field failing here has a stored
// wildcard the read path does not recognise.
func TestV2_EveryNullableFieldRoundTripsNull(t *testing.T) {
	for _, tc := range v2RoundTripCases() {
		t.Run(tc.name, func(t *testing.T) {
			r, restore := tc.api(t)
			defer restore()
			base := "/api/v2/humans/u1/tracking/" + tc.name

			// 1. POST the minimal body.
			w := v2DoReq(t, r, http.MethodPost, base, "["+tc.body+"]")
			if w.Code != http.StatusOK {
				t.Fatalf("create: %d %s", w.Code, w.Body.String())
			}
			uid := singleCreatedUID(t, v2DecodeBody(t, w))

			// 2. Every nullable field the body omitted reads null.
			var sent map[string]any
			if err := json.Unmarshal([]byte(tc.body), &sent); err != nil {
				t.Fatal(err)
			}
			got := getOneRule(t, r, base, uid)
			for _, name := range nullableJSONNames(t, tc.req) {
				if _, ok := sent[name]; ok {
					continue
				}
				v, present := got[name]
				if !present || v != nil {
					t.Errorf("%s.%s = %v (present=%v), want null", tc.name, name, v, present)
				}
			}

			// 3. PUT the GET result back (minus uid): unchanged, byte-identical.
			putBody, _ := json.Marshal(cloneWithoutUID(got))
			w = v2DoReq(t, r, http.MethodPut, base+"/"+itoa(uid), string(putBody))
			if w.Code != http.StatusOK {
				t.Fatalf("put: %d %s", w.Code, w.Body.String())
			}
			putResp := v2DecodeBody(t, w)
			if n := len(tryRules(putResp, "unchanged")); n != 1 || len(tryRules(putResp, "updated")) != 0 {
				t.Fatalf("put: want 1 unchanged and 0 updated, got %s", w.Body.String())
			}
			after, _ := json.Marshal(cloneWithoutUID(getOneRule(t, r, base, uid)))
			if string(after) != string(putBody) {
				t.Errorf("PUT changed the rule:\n  before: %s\n  after:  %s", putBody, after)
			}

			// 4. POST the GET result back: unchanged.
			w = v2DoReq(t, r, http.MethodPost, base, "["+string(putBody)+"]")
			if w.Code != http.StatusOK {
				t.Fatalf("re-post: %d %s", w.Code, w.Body.String())
			}
			body := v2DecodeBody(t, w)
			if n := len(tryRules(body, "unchanged")); n != 1 {
				t.Errorf("re-post: want 1 unchanged, got %d: %s", n, w.Body.String())
			}
		})
	}
}

// Legacy pokemon rows holding the alias wildcards (0 for rarity, size and the
// pvp ranks) must read as null, and POSTing that read back must be unchanged.
func TestV2_LegacyPokemonAliasRowsRoundTripNull(t *testing.T) {
	r, monsterStore, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	monsterStore.Insert(&db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 1, PokemonID: 25,
		MaxIV: 100, MaxCP: 9000, MaxLevel: 55, MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		MaxWeight: 9000000, MaxRarity: 6, MaxSize: 5, Costume: 9000, MinIV: -1,
		Rarity: 0, Size: 0, PVPRankingBest: 0, PVPRankingWorst: 0,
	})
	base := "/api/v2/humans/u1/tracking/pokemon"

	w := v2DoReq(t, r, http.MethodGet, base, "")
	rules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	for _, k := range []string{"rarity", "size", "pvp_ranking_best", "pvp_ranking_worst"} {
		if v, ok := rules[0][k]; !ok || v != nil {
			t.Errorf("%s = %v, want null", k, v)
		}
	}
	body, _ := json.Marshal(cloneWithoutUID(rules[0]))
	w = v2DoReq(t, r, http.MethodPost, base, "["+string(body)+"]")
	if w.Code != http.StatusOK {
		t.Fatalf("re-post: %d %s", w.Code, w.Body.String())
	}
	if n := len(tryRules(v2DecodeBody(t, w), "unchanged")); n != 1 {
		t.Errorf("re-post: want 1 unchanged, got %d: %s", n, w.Body.String())
	}
}
