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

func v2PokemonRow(minIV int) db.MonsterTrackingAPI {
	return db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 1, PokemonID: 25, MinIV: minIV,
		MaxIV: 100, MaxCP: 9000, MaxLevel: 55, MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		MaxWeight: 9000000, MaxRarity: 6, MaxSize: 5, Costume: 9000,
	}
}

func v2SeedTwo(t *testing.T, ms *store.MockTrackingStore[db.MonsterTrackingAPI]) (int64, int64) {
	t.Helper()
	a, b := v2PokemonRow(90), v2PokemonRow(80)
	u1, err := ms.Insert(&a)
	if err != nil {
		t.Fatal(err)
	}
	u2, err := ms.Insert(&b)
	if err != nil {
		t.Fatal(err)
	}
	return u1, u2
}

func TestV2Put_RefusesNearDuplicateOfOtherRule(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, u2 := v2SeedTwo(t, ms)
	before := ms.AllRows()

	w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(u1)+"?silent=true", `{"pokemon_id":25,"min_iv":80,"distance":500}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d %s", w.Code, w.Body.String())
	}
	if want := "would duplicate rule " + itoa(u2); !strings.Contains(w.Body.String(), want) {
		t.Fatalf("want %q in %s", want, w.Body.String())
	}
	if !reflect.DeepEqual(ms.AllRows(), before) {
		t.Fatalf("rows changed")
	}
}

func TestV2Put_ReportsPrevious(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, _ := v2SeedTwo(t, ms)
	w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(u1)+"?silent=true", `{"pokemon_id":25,"min_iv":95,"max_cp":5000}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	up := v2RulesArray(t, v2DecodeBody(t, w), "updated")
	prev, _ := up[0]["previous"].(map[string]any)
	if prev == nil || prev["min_iv"] != float64(90) || up[0]["min_iv"] != float64(95) {
		t.Fatalf("updated = %v", up)
	}
}

func TestV2Put_DryRunWritesNothing(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, _ := v2SeedTwo(t, ms)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(u1)+"?dry_run=true", `{"pokemon_id":25,"min_iv":95,"max_cp":5000}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if body["dry_run"] != true {
		t.Fatalf("dry_run = %v", body["dry_run"])
	}
	up := v2RulesArray(t, body, "updated")
	if up[0]["uid"] != float64(u1) || up[0]["min_iv"] != float64(95) {
		t.Fatalf("updated = %v", up)
	}
	v2AssertUntouched(t, ms, before, pushes)

	// Same validation as the real call.
	if w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/"+itoa(u1)+"?dry_run=true", `{"pokemon_id":25,"min_iv":80,"distance":500}`); w.Code != http.StatusConflict {
		t.Fatalf("dry-run conflict: %d", w.Code)
	}
	if w := v2DoReq(t, r, http.MethodPut, v2DryRunPath+"/9999?dry_run=true", `{"pokemon_id":25}`); w.Code != http.StatusNotFound {
		t.Fatalf("dry-run 404: %d", w.Code)
	}
}

func v2AssertUntouched(t *testing.T, ms *store.MockTrackingStore[db.MonsterTrackingAPI], before []db.MonsterTrackingAPI, pushes *[]pushRecord) {
	t.Helper()
	if !reflect.DeepEqual(ms.AllRows(), before) {
		t.Fatalf("store changed")
	}
	if n := atomic.LoadInt32(&v2PokemonTestReloads); n != 0 {
		t.Fatalf("reloads = %d", n)
	}
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %v", *pushes)
	}
}

func TestV2Delete_DryRunWritesNothing(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, u2 := v2SeedTwo(t, ms)
	before := ms.AllRows()
	atomic.StoreInt32(&v2PokemonTestReloads, 0)

	w := v2DoReq(t, r, http.MethodDelete, v2DryRunPath+"/"+itoa(u1)+"?dry_run=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	del := v2RulesArray(t, body, "deleted")
	if body["dry_run"] != true || len(del) != 1 || del[0]["uid"] != float64(u1) {
		t.Fatalf("body = %v", body)
	}
	v2AssertUntouched(t, ms, before, pushes)

	w = v2DoReq(t, r, http.MethodDelete, fmt.Sprintf("%s?dry_run=true&uid=%d,%d", v2DryRunPath, u1, u2), "")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body = v2DecodeBody(t, w)
	if body["dry_run"] != true || len(v2RulesArray(t, body, "deleted")) != 2 {
		t.Fatalf("body = %v", body)
	}
	v2AssertUntouched(t, ms, before, pushes)

	if w := v2DoReq(t, r, http.MethodDelete, v2DryRunPath+"/9999?dry_run=true", ""); w.Code != http.StatusNotFound {
		t.Fatalf("dry-run 404: %d", w.Code)
	}
}

func TestV2BulkDelete_QueryFormStillWorks(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, u2 := v2SeedTwo(t, ms)
	w := v2DoReq(t, r, http.MethodDelete, fmt.Sprintf("%s?silent=true&uid=%d,%d", v2DryRunPath, u1, u2), "")
	if w.Code != http.StatusOK || len(ms.AllRows()) != 0 {
		t.Fatalf("%d %s rows=%d", w.Code, w.Body.String(), len(ms.AllRows()))
	}
}

func TestV2BulkDelete_BodyUIDs(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	var uids []string
	for i := 0; i < 300; i++ {
		row := v2PokemonRow(0)
		row.PokemonID = i + 1
		u, err := ms.Insert(&row)
		if err != nil {
			t.Fatal(err)
		}
		uids = append(uids, itoa(u))
	}
	body := `{"uids":[` + strings.Join(uids, ",") + `]}`
	w := v2DoReq(t, r, http.MethodDelete, v2DryRunPath+"?silent=true", body)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if n := len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")); n != 300 {
		t.Fatalf("deleted %d", n)
	}
	if n := len(ms.AllRows()); n != 0 {
		t.Fatalf("rows left %d", n)
	}
}

func TestV2BulkDelete_BodyAndQueryOrNeither422(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	u1, _ := v2SeedTwo(t, ms)
	w := v2DoReq(t, r, http.MethodDelete, v2DryRunPath+"?uid="+itoa(u1), `{"uids":[`+itoa(u1)+`]}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "send uid or a body uids list, not both") {
		t.Fatalf("both: %d %s", w.Code, w.Body.String())
	}
	w = v2DoReq(t, r, http.MethodDelete, v2DryRunPath, "")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "uid or uids is required") {
		t.Fatalf("neither: %d %s", w.Code, w.Body.String())
	}
	w = v2DoReq(t, r, http.MethodDelete, v2DryRunPath, `{"uids":[]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty list: %d", w.Code)
	}
	if len(ms.AllRows()) != 2 {
		t.Fatalf("rows changed")
	}
}
