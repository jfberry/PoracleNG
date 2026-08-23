package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// --- test harness ---------------------------------------------------------

// pushRecord captures a confirmation push (overriding v2SendConfirmation).
type pushRecord struct {
	target  string
	message string
}

// newV2PokemonTestAPI wires the strict v2 pokemon endpoints against mock stores
// through a REAL huma+gin engine. It returns the engine, the monster store
// (id-scoped), and a pointer to the slice that records confirmation pushes.
// The returned restore func resets the v2SendConfirmation seam.
func newV2PokemonTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.MonsterTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	monsterStore := store.NewMockTrackingStore[db.MonsterTrackingAPI](
		store.MonsterGetUID, store.MonsterSetUID,
	).WithIDScope(func(m *db.MonsterTrackingAPI) string { return m.ID })

	// Minimal non-nil GameData so MonsterRowText doesn't panic on GetMonster /
	// gender-emoji lookups. Empty maps are enough — rowtext tolerates misses.
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{
				1: {Emoji: "M"},
				2: {Emoji: "F"},
			},
		},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Monsters: monsterStore},
		Config:   &config.Config{},
		RowText: &rowtext.Generator{
			GD:                  gd,
			Translations:        i18n.Load(""),
			DefaultTemplateName: "1",
		},
		Translations: i18n.Load(""),
		// Dispatcher nil; pushes go through the v2SendConfirmation seam below.
	}

	pushes := &[]pushRecord{}
	orig := v2SendConfirmation
	v2SendConfirmation = func(_ *TrackingDeps, human *store.HumanLite, message, _ string) {
		*pushes = append(*pushes, pushRecord{target: human.ID, message: message})
	}
	restore := func() { v2SendConfirmation = orig }

	RegisterV2TrackingPokemon(humaAPI, deps)
	return r, monsterStore, pushes, restore
}

func v2DoReq(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func v2DecodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v\nraw: %s", err, w.Body.String())
	}
	return m
}

func v2RulesArray(t *testing.T, body map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := body[key]
	if !ok {
		t.Fatalf("response missing %q: %v", key, body)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("%q is not an array: %T", key, raw)
	}
	out := make([]map[string]any, len(arr))
	for i, e := range arr {
		out[i] = e.(map[string]any)
	}
	return out
}

// --- create (single + array) ----------------------------------------------

func TestV2Pokemon_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()

	body := `[{"pokemon_id":149,"min_iv":95,"gender":"female","clean":true},{"pokemon_id":384,"pvp_ranking_league":1500}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := v2DecodeBody(t, w)
	created := v2RulesArray(t, resp, "created")
	if len(created) != 2 {
		t.Fatalf("expected 2 created, got %d: %v", len(created), resp)
	}
	for _, rule := range created {
		if _, ok := rule["uid"]; !ok {
			t.Fatalf("created rule missing uid: %v", rule)
		}
		if uidF, ok := rule["uid"].(float64); !ok || uidF <= 0 {
			t.Fatalf("created rule has invalid uid: %v", rule["uid"])
		}
	}
	// Confirmation push fired (not silent).
	if len(*pushes) != 1 {
		t.Fatalf("expected 1 push, got %d", len(*pushes))
	}
}

func TestV2Pokemon_CreateSingleElementArray_OK(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].PokemonID != 25 {
		t.Fatalf("expected 1 stored row for pikachu, got %+v", rows)
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Pokemon_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"min_iv":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string min_iv, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_RejectsMissingPokemonID(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"min_iv":95}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing pokemon_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_RejectsBadGender(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"gender":"alien"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad gender, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- gender round-trip + bitmask + defaults --------------------------------

func TestV2Pokemon_GenderRoundTrip(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"gender":"female"}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Gender != 2 {
		t.Fatalf("expected stored gender 2 (female), got %+v", rows)
	}
	// Read it back: response gender should be "female".
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if out[0]["gender"] != "female" {
		t.Fatalf("expected gender female on read-back, got %v", out[0]["gender"])
	}
}

// TestV2Pokemon_PVPRankingEvolution covers the temp-evolution (mega) PVP
// discriminator (0=base, 1=mega, 2=X, 3=Y) exposed over develop's existing
// pvp_ranking_evolution column: a non-zero value stores + reads back; the
// default 0 stores 0 and is hidden (null) on read.
func TestV2Pokemon_PVPRankingEvolution(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	// Explicit Mega X (2) stores 2 and reads back 2.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon",
		`[{"pokemon_id":6,"pvp_ranking_league":1500,"pvp_ranking_evolution":2}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].PVPRankingEvolution != 2 {
		t.Fatalf("expected stored pvp_ranking_evolution 2 (Mega X), got %+v", rows)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if got := out[0]["pvp_ranking_evolution"]; got != float64(2) {
		t.Fatalf("expected pvp_ranking_evolution 2 on read-back, got %v", got)
	}

	// Omitted → stored 0 (base form) → hidden (null) on read.
	r2, ms2, _, restore2 := newV2PokemonTestAPI(t)
	defer restore2()
	v2DoReq(t, r2, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	if rows := ms2.AllRows(); len(rows) != 1 || rows[0].PVPRankingEvolution != 0 {
		t.Fatalf("expected stored pvp_ranking_evolution 0 (base) on omission, got %+v", rows)
	}
	w2 := v2DoReq(t, r2, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	out2 := v2RulesArray(t, v2DecodeBody(t, w2), "rules")
	if got, ok := out2[0]["pvp_ranking_evolution"]; ok && got != nil {
		t.Fatalf("expected pvp_ranking_evolution null/absent on base-form read-back, got %v", got)
	}
}

func TestV2Pokemon_CleanEditSummaryBitmask(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"clean":true,"edit":true,"summary":true}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7 (clean+edit+summary), got %+v", rows)
	}

	// Only edit → bit 2.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":26,"edit":true}]`)
	for _, row := range ms.AllRows() {
		if row.PokemonID == 26 && row.Clean != 2 {
			t.Fatalf("expected edit-only clean bitmask 2, got %d", row.Clean)
		}
	}
}

func TestV2Pokemon_DefaultsOnOmission(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	// Every wildcard/sentinel field is asserted here so the omit-to-wildcard
	// contract (requirement #1: omitting a filter matches "any") is locked.
	checks := map[string]struct{ got, want int }{
		"form":       {row.Form, 0},
		"min_iv":     {row.MinIV, -1},  // -1 = no lower bound
		"max_iv":     {row.MaxIV, 100}, // 100 = the IV ceiling
		"min_cp":     {row.MinCP, 0},
		"max_cp":     {row.MaxCP, 9000}, // 9000 = CP cap sentinel
		"min_level":  {row.MinLevel, 0},
		"max_level":  {row.MaxLevel, 55}, // 55 = level ceiling
		"atk":        {row.ATK, 0},
		"def":        {row.DEF, 0},
		"sta":        {row.STA, 0},
		"max_atk":    {row.MaxATK, 15}, // 15 = IV ceiling
		"max_def":    {row.MaxDEF, 15},
		"max_sta":    {row.MaxSTA, 15},
		"min_weight": {row.MinWeight, 0},
		"max_weight": {row.MaxWeight, 9000000}, // 9000000 = no upper weight
		"min_time":   {row.MinTime, 0},
		"rarity":     {row.Rarity, -1}, // -1 = any
		"max_rarity": {row.MaxRarity, 6},
		"size":       {row.Size, -1}, // -1 = any
		"max_size":   {row.MaxSize, 5},
		"pvp_league": {row.PVPRankingLeague, 0}, // 0 = none/IV mode
		"pvp_best":   {row.PVPRankingBest, 1},
		"pvp_worst":  {row.PVPRankingWorst, 4096}, // 4096 = no upper rank limit
		"pvp_min_cp": {row.PVPRankingMinCP, 0},
		"pvp_cap":    {row.PVPRankingCap, 0}, // 0 = league default
		"gender":     {row.Gender, 0},
		"clean":      {row.Clean, 0},
		"distance":   {row.Distance, 0},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("default %s: got %d want %d", name, c.got, c.want)
		}
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

// TestV2Pokemon_OmittedPointerStaysNil verifies decision #2: with NO `default:`
// tag, huma leaves an omitted optional pointer nil at the handler boundary (so
// the handler's valueOr applies the documented default). This is the empirical
// check that we are NOT relying on huma auto-populating pointers.
func TestV2Pokemon_OmittedPointerStaysNil(t *testing.T) {
	var rule v2PokemonRule
	if err := json.Unmarshal([]byte(`{"pokemon_id":25}`), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.MaxCP != nil {
		t.Fatalf("expected MaxCP nil on omission, got %v", *rule.MaxCP)
	}
	if rule.Gender != nil {
		t.Fatalf("expected Gender nil on omission, got %v", *rule.Gender)
	}
	// valueOr applies the documented default.
	if got := valueOr(rule.MaxCP, 9000); got != 9000 {
		t.Fatalf("valueOr default mismatch: %d", got)
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Pokemon_ListShape(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25},{"pokemon_id":26}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestV2Pokemon_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	// u1 creates a rule.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	uid := int64(created[0]["uid"].(float64))

	// u1 can fetch it.
	pathOwn := "/api/v2/humans/u1/tracking/pokemon/" + itoa(uid)
	w = v2DoReq(t, r, http.MethodGet, pathOwn, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// u2 cannot fetch u1's uid → 404.
	pathOther := "/api/v2/humans/u2/tracking/pokemon/" + itoa(uid)
	w = v2DoReq(t, r, http.MethodGet, pathOther, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Pokemon_DeleteSingle(t *testing.T) {
	r, ms, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/pokemon/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	deleted := v2RulesArray(t, v2DecodeBody(t, w), "deleted")
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted, got %d", len(deleted))
	}
	if len(ms.AllRows()) != 0 {
		t.Fatalf("row not deleted: %+v", ms.AllRows())
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Pokemon_DeleteCrossHuman404(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u2/tracking/pokemon/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting another human's uid, got %d: %s", w.Code, w.Body.String())
	}
	if len(ms.AllRows()) != 1 {
		t.Fatalf("u1's row must survive a cross-human delete: %+v", ms.AllRows())
	}
}

func TestV2Pokemon_BulkDelete(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25},{"pokemon_id":26},{"pokemon_id":27}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/pokemon?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	deleted := v2RulesArray(t, v2DecodeBody(t, w), "deleted")
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted, got %d", len(deleted))
	}
	if len(ms.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(ms.AllRows()))
	}
}

func TestV2Pokemon_BulkDelete_BadUID(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/pokemon?uid=1,x,3", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for non-int uid token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_BulkDelete_MissingUID(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/pokemon", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing uid, got %d: %s", w.Code, w.Body.String())
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Pokemon_PutFullReplace_NewUID(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"min_iv":90,"max_cp":1500}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted max_cp must reset to default 9000.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/pokemon/"+itoa(oldUID), `{"pokemon_id":25,"min_iv":50}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	updated := v2RulesArray(t, v2DecodeBody(t, w), "updated")
	if len(updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(updated))
	}
	newUID := int64(updated[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid; got same %d", newUID)
	}
	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row after replace, got %d", len(rows))
	}
	if rows[0].MinIV != 50 || rows[0].MaxCP != 9000 {
		t.Fatalf("replace did not reset omitted fields: min_iv=%d max_cp=%d", rows[0].MinIV, rows[0].MaxCP)
	}
}

// roundTripRule is the shared GET→PUT→GET fixed-point check for Part B (#138):
// a client GETs a rule (wildcards projected to null), PUTs that exact body back
// (PUT is full-replace: null/omitted → documented default), and GETs again. The
// returned rule object must be byte-identical after the round-trip — proving null
// in the response ⇔ omit in the request ⇔ engine wildcard. uid lives in the path,
// not the body, so it is stripped before the PUT and excluded from the compare
// (PUT mints a new uid by design).
// It returns the rule object as GET first reported it (wildcards as null), so
// callers can additionally assert the response shape.
func roundTripRule(t *testing.T, r http.Handler, basePath, createBody string) map[string]any {
	t.Helper()

	// Create.
	w := v2DoReq(t, r, http.MethodPost, basePath, createBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	uid := singleCreatedUID(t, v2DecodeBody(t, w))

	// GET the rule back.
	got1 := getOneRule(t, r, basePath, uid)

	// PUT the exact returned body back (strip uid; it is the path param).
	putBody := cloneWithoutUID(got1)
	pb, _ := json.Marshal(putBody)
	w = v2DoReq(t, r, http.MethodPut, basePath+"/"+itoa(uid), string(pb))
	if w.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))

	// GET again and compare (minus uid).
	got2 := getOneRule(t, r, basePath, newUID)
	a := cloneWithoutUID(got1)
	b := cloneWithoutUID(got2)
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(ab) != string(bb) {
		t.Fatalf("round-trip changed the rule:\n  before: %s\n  after:  %s", ab, bb)
	}
	return got1
}

// singleCreatedUID extracts the uid of the one created/updated rule from a
// create/put response (the rule may land in created or updated depending on the
// type's diff semantics).
func singleCreatedUID(t *testing.T, body map[string]any) int64 {
	t.Helper()
	for _, key := range []string{"created", "updated"} {
		if rules := tryRules(body, key); len(rules) == 1 {
			return int64(rules[0]["uid"].(float64))
		}
	}
	t.Fatalf("expected exactly one created/updated rule, got: %v", body)
	return 0
}

// getOneRule lists the type's rules and returns the one with the given uid.
func getOneRule(t *testing.T, r http.Handler, basePath string, uid int64) map[string]any {
	t.Helper()
	w := v2DoReq(t, r, http.MethodGet, basePath, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, rule := range v2RulesArray(t, v2DecodeBody(t, w), "rules") {
		if int64(rule["uid"].(float64)) == uid {
			return rule
		}
	}
	t.Fatalf("rule uid %d not found in list", uid)
	return nil
}

func cloneWithoutUID(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "uid" {
			continue
		}
		out[k] = v
	}
	return out
}

func tryRules(body map[string]any, key string) []map[string]any {
	raw, ok := body[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, len(arr))
	for i, e := range arr {
		out[i] = e.(map[string]any)
	}
	return out
}

// TestV2Pokemon_RoundTrip is the Part B fixed-point check: a rule with a mix of
// set + default fields GETs back with the defaults as null, and PUTting that body
// back leaves the stored rule unchanged.
func TestV2Pokemon_RoundTrip(t *testing.T) {
	r, ms, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	roundTripRule(t, r, "/api/v2/humans/u1/tracking/pokemon",
		`[{"pokemon_id":149,"min_iv":95,"gender":"female","clean":true,"distance":500}]`)

	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row after round-trip, got %d", len(rows))
	}
	row := rows[0]
	// Meaningful fields survived; defaults stayed at their wildcard.
	if row.PokemonID != 149 || row.MinIV != 95 || row.Gender != 2 || row.Distance != 500 || !db.IsClean(row.Clean) {
		t.Fatalf("set fields not preserved across round-trip: %+v", row)
	}
	if row.MaxIV != 100 || row.MaxCP != 9000 || row.PVPRankingWorst != 4096 {
		t.Fatalf("default fields drifted across round-trip: %+v", row)
	}
}

// TestV2Pokemon_DefaultRuleResponseIsNull asserts the wildcard fields are emitted
// as JSON null (present, not dropped) and the required field carries its value.
func TestV2Pokemon_DefaultRuleResponseIsNull(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":149}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]

	if rule["pokemon_id"] != float64(149) {
		t.Fatalf("required pokemon_id must carry its value, got %v", rule["pokemon_id"])
	}
	for _, k := range []string{"min_iv", "max_iv", "max_cp", "pvp_ranking_worst", "max_weight", "gender", "clean", "distance", "template", "override_areas"} {
		v, present := rule[k]
		if !present {
			t.Errorf("%s must be PRESENT (as null), but key is absent", k)
		}
		if v != nil {
			t.Errorf("%s must be null at its wildcard, got %v", k, v)
		}
	}
}

func TestV2Pokemon_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/pokemon/"+itoa(uid), `{"pokemon_id":25}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d: %s", w.Code, w.Body.String())
	}
}

// --- include_descriptions (reads AND mutations) ----------------------------

func TestV2Pokemon_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25}]`)

	// Without the flag: no description.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	// With the flag: description present.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/pokemon?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Pokemon_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?include_descriptions=true", `[{"pokemon_id":25}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	// The assembled prefixed message is NEVER in the HTTP body.
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

// --- silent -----------------------------------------------------------------

func TestV2Pokemon_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true", `[{"pokemon_id":25}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

// --- human-not-found --------------------------------------------------------

func TestV2Pokemon_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/pokemon", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d: %s", w.Code, w.Body.String())
	}
}

// --- v2RuleEnvelope schema sanity (SchemaProvider) -------------------------

func TestV2RuleEnvelope_SchemaIncludesUID(t *testing.T) {
	reg := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	s := v2RuleEnvelope[v2PokemonRule]{}.Schema(reg)
	if s.Type != huma.TypeObject {
		t.Fatalf("expected object schema, got %q", s.Type)
	}
	if _, ok := s.Properties["uid"]; !ok {
		t.Fatalf("envelope schema missing uid property")
	}
	if _, ok := s.Properties["pokemon_id"]; !ok {
		t.Fatalf("envelope schema missing pokemon_id property (Req flatten)")
	}
}

// itoa is a tiny helper to keep paths readable.
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// pvp_ranking_league documents a closed value set (0|500|1500|2500) but used
// to accept any integer — a league like 42 was persisted yet indexed under a
// key the PVP matcher never traverses, so the rule could never alert.
func TestV2Pokemon_RejectsInvalidPVPLeague(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon",
		`[{"pokemon_id":25,"pvp_ranking_league":42,"pvp_ranking_worst":10}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for league 42, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Pokemon_AcceptsValidPVPLeagues(t *testing.T) {
	r, _, _, restore := newV2PokemonTestAPI(t)
	defer restore()
	for _, league := range []int{500, 1500, 2500} {
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon",
			fmt.Sprintf(`[{"pokemon_id":25,"pvp_ranking_league":%d,"pvp_ranking_worst":10}]`, league))
		if w.Code != http.StatusOK {
			t.Fatalf("league %d should be accepted, got %d: %s", league, w.Code, w.Body.String())
		}
	}
}
