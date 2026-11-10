package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// v2PutUnchanged PUTs the GET body of uid back and asserts it is reported as
// unchanged (with its uid), with nothing created or updated.
func v2PutUnchanged(t *testing.T, r http.Handler, base string, uid int64, query string) map[string]any {
	t.Helper()
	putBody, _ := json.Marshal(cloneWithoutUID(getOneRule(t, r, base, uid)))
	w := v2DoReq(t, r, http.MethodPut, base+"/"+itoa(uid)+query, string(putBody))
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"created":[]`) || !strings.Contains(w.Body.String(), `"updated":[]`) {
		t.Errorf("created/updated must be empty arrays: %s", w.Body.String())
	}
	body := v2DecodeBody(t, w)
	un := tryRules(body, "unchanged")
	if len(un) != 1 || un[0]["uid"] != float64(uid) {
		t.Fatalf("want 1 unchanged with uid %d, got %s", uid, w.Body.String())
	}
	return body
}

// GET -> PUT of the same rule is unchanged: no write, no reload, no push. The
// two seeded rules differ only in min_iv (an update field), so they are already
// near-duplicates of each other: an unchanged PUT writes nothing and so cannot
// squash them, and is reported unchanged rather than 409 (as PATCH does).
func TestV2Put_GetPutUnchanged_Pokemon(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, _ := v2SeedTwo(t, ms)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	v2PutUnchanged(t, r, v2DryRunPath, u1, "")
	v2AssertUntouched(t, ms, before, pushes)

	body := v2PutUnchanged(t, r, v2DryRunPath, u1, "?dry_run=true")
	if body["dry_run"] != true {
		t.Errorf("dry_run = %v", body["dry_run"])
	}
	v2AssertUntouched(t, ms, before, pushes)
}

// A row stored with legacy alias wildcards (rarity 0, size 0, ...) and a
// legacy spaced override_areas spelling compares by canonical form and key, so
// its own GET -> PUT is unchanged too.
func TestV2Put_GetPutUnchanged_LegacyAliasRow(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	row := db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 1, PokemonID: 0, Form: 0, Costume: 9000,
		MinIV: 0, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		MaxWeight: 9000000, Rarity: 0, MaxRarity: 6, Size: 0, MaxSize: 5,
		PVPRankingBest: 0, PVPRankingWorst: 0, Template: "1",
		OverrideAreas: []string{"gent centrum"},
	}
	uid, err := ms.Insert(&row)
	if err != nil {
		t.Fatal(err)
	}
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	v2PutUnchanged(t, r, v2DryRunPath, uid, "")
	v2AssertUntouched(t, ms, before, pushes)
}

func TestV2Put_GetPutUnchanged_Raid(t *testing.T) {
	r, rs, pushes, restore := newV2RaidTestAPI(t)
	defer restore()
	base := "/api/v2/humans/u1/tracking/raid"
	w := v2DoReq(t, r, http.MethodPost, base, `[{"level":5,"clean":true,"team":"mystic"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))
	*pushes = nil
	before := rs.AllRows()

	v2PutUnchanged(t, r, base, uid, "")
	if !reflect.DeepEqual(rs.AllRows(), before) {
		t.Fatalf("store changed")
	}
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %v", *pushes)
	}
}

// A real change is still an update with previous, written and pushed, and the
// response carries created and unchanged as empty arrays (never null).
func TestV2Put_RealChangeStillUpdated(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1 := v2SeedPokemon(t, ms, v2PokemonRow(90))[0]
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	for _, q := range []string{"?dry_run=true", ""} {
		w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(u1)+q, `{"pokemon_id":25,"min_iv":95}`)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", q, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"created":[]`) || !strings.Contains(w.Body.String(), `"unchanged":[]`) {
			t.Errorf("%s: created/unchanged must be empty arrays: %s", q, w.Body.String())
		}
		up := v2RulesArray(t, v2DecodeBody(t, w), "updated")
		prev, _ := up[0]["previous"].(map[string]any)
		if len(up) != 1 || up[0]["uid"] != float64(u1) || up[0]["min_iv"] != float64(95) || prev == nil || prev["min_iv"] != float64(90) {
			t.Fatalf("%s: updated = %v", q, up)
		}
	}
	if got := v2RowByUID(t, ms, u1).MinIV; got != 95 {
		t.Errorf("stored min_iv = %d, want 95", got)
	}
	if n := atomic.LoadInt32(&v2PokemonTestReloads); n != 1 {
		t.Errorf("reloads = %d, want 1", n)
	}
	if len(*pushes) != 1 {
		t.Errorf("pushes = %v, want 1", *pushes)
	}
}
