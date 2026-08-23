package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2MaxbattleTestAPI wires the strict v2 maxbattle endpoints against mock
// stores through a REAL huma+gin engine. Mirrors newV2PokemonTestAPI.
func newV2MaxbattleTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.MaxbattleTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	mbStore := store.NewMockTrackingStore[db.MaxbattleTrackingAPI](
		store.MaxbattleGetUID, store.MaxbattleSetUID,
	).WithIDScope(func(m *db.MaxbattleTrackingAPI) string { return m.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Maxbattles: mbStore},
		Config:   &config.Config{},
		RowText: &rowtext.Generator{
			GD:                  gd,
			Translations:        i18n.Load(""),
			DefaultTemplateName: "1",
		},
		Translations: i18n.Load(""),
	}

	pushes := &[]pushRecord{}
	orig := v2SendConfirmation
	v2SendConfirmation = func(_ *TrackingDeps, human *store.HumanLite, message, _ string) {
		*pushes = append(*pushes, pushRecord{target: human.ID, message: message})
	}
	restore := func() { v2SendConfirmation = orig }

	RegisterV2TrackingMaxbattle(humaAPI, deps)
	return r, mbStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Maxbattle_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2MaxbattleTestAPI(t)
	defer restore()

	body := `[{"pokemon_id":150,"gmax":true,"clean":true},{"pokemon_id":9000,"level":6}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	if len(created) != 2 {
		t.Fatalf("expected 2 created, got %d", len(created))
	}
	for _, rule := range created {
		if uidF, ok := rule["uid"].(float64); !ok || uidF <= 0 {
			t.Fatalf("created rule has invalid uid: %v", rule["uid"])
		}
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected 1 push, got %d", len(*pushes))
	}
}

func TestV2Maxbattle_CreateSingleElementArray_OK(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].PokemonID != 150 {
		t.Fatalf("expected 1 stored row for 150, got %+v", rows)
	}
}

// --- track-by-level validation (v1 parity) ---------------------------------

func TestV2Maxbattle_RejectsLevelLessThanOneByLevel(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	// pokemon_id omitted ⇒ defaults to 9000 (track by level); level 0 is invalid.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"level":0}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for level<1 by-level, got %d: %s", w.Code, w.Body.String())
	}
	if len(ms.AllRows()) != 0 {
		t.Fatalf("invalid by-level rule must not be stored: %+v", ms.AllRows())
	}
}

// By-level with no level supplied is "everything" (any tier) ⇒ level 90, NOT a
// 422 (replaces the old "level required when pokemon_id=9000" rule). See
// matching/maxbattle.go:48 — stored 9000/90 is any-boss/any-tier.
func TestV2Maxbattle_ByLevelOmittedLevelStores90(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":9000}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("by-level without a level should be 'everything', got %d: %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].PokemonID != 9000 || rows[0].Level != 90 {
		t.Fatalf("by-level no-level must store 9000/90, got %+v", rows)
	}
}

// Omitting pokemon_id entirely (with a tier) stores the 9000 track-by-level
// sentinel and the supplied tier.
func TestV2Maxbattle_OmittedPokemonIDStoresSentinel(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"level":5}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("omitted pokemon_id with a valid level should be accepted, got %d: %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].PokemonID != 9000 || rows[0].Level != 5 {
		t.Fatalf("omitted pokemon_id must store 9000 (track-by-level) with level=5, got %+v", rows)
	}
}

func TestV2Maxbattle_ByPokemonForcesLevelSentinel(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	// pokemon_id present ⇒ level forced to 9000 regardless of supplied level.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"level":3}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Level != 9000 {
		t.Fatalf("by-pokemon must force level to 9000, got %+v", rows)
	}
}

// Response projection: 9000/90 => null/null; 9000/5 => null/5; 150/9000 => 150/null.
func TestV2Maxbattle_LevelNullProjection(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantPokemon any
		wantLevel   any
	}{
		{"everything", `[{"pokemon_id":9000}]`, nil, nil},
		{"by_level_tier", `[{"level":5}]`, nil, float64(5)},
		{"specific_boss", `[{"pokemon_id":150}]`, float64(150), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _, _, restore := newV2MaxbattleTestAPI(t)
			defer restore()
			v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", c.body)
			w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle", "")
			out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
			if out[0]["pokemon_id"] != c.wantPokemon {
				t.Fatalf("pokemon_id: got %v want %v", out[0]["pokemon_id"], c.wantPokemon)
			}
			if out[0]["level"] != c.wantLevel {
				t.Fatalf("level: got %v want %v", out[0]["level"], c.wantLevel)
			}
		})
	}
}

// Round-trip (GET->PUT->GET identical) for the three derivation cases.
func TestV2Maxbattle_LevelRoundTrip(t *testing.T) {
	cases := map[string]string{
		"everything":    `[{"pokemon_id":9000}]`,
		"specific_boss": `[{"pokemon_id":150}]`,
		"by_level_tier": `[{"level":5}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r, ms, _, restore := newV2MaxbattleTestAPI(t)
			defer restore()
			roundTripRule(t, r, "/api/v2/humans/u1/tracking/maxbattle", body)
			rows := ms.AllRows()
			if len(rows) != 1 {
				t.Fatalf("expected 1 row after round-trip, got %d", len(rows))
			}
		})
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Maxbattle_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Maxbattle_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"gmax":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string gmax, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Maxbattle_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Maxbattle_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults + bitmask + gmax round-trip ----------------------------------

func TestV2Maxbattle_DefaultsOnOmission(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	checks := map[string]struct{ got, want int }{
		"level":     {row.Level, 9000},
		"form":      {row.Form, 0},
		"move":      {row.Move, 9000},
		"gmax":      {row.Gmax, 0},
		"evolution": {row.Evolution, 9000},
		"clean":     {row.Clean, 0},
		"distance":  {row.Distance, 0},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("default %s: got %d want %d", name, c.got, c.want)
		}
	}
	if row.StationID != nil {
		t.Errorf("station_id should default to nil, got %v", *row.StationID)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Maxbattle_GmaxRoundTrip(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"gmax":true}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Gmax != 1 {
		t.Fatalf("expected stored gmax 1, got %+v", rows)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if out[0]["gmax"] != true {
		t.Fatalf("expected gmax true on read-back, got %v", out[0]["gmax"])
	}
}

func TestV2Maxbattle_StationIDRoundTrip(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"station_id":"abc"}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].StationID == nil || *rows[0].StationID != "abc" {
		t.Fatalf("expected stored station_id abc, got %+v", rows)
	}
	// Empty string is normalised to nil (v1 parity).
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":151,"station_id":""}]`)
	for _, row := range ms.AllRows() {
		if row.PokemonID == 151 && row.StationID != nil {
			t.Fatalf("empty station_id must normalise to nil, got %v", *row.StationID)
		}
	}
}

func TestV2Maxbattle_CleanEditSummaryBitmask(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"clean":true,"edit":true,"summary":true}]`)
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- diff behaviour (no match keys) ----------------------------------------

// MaxbattleTrackingAPI carries no diff:"match" tags, so the shared DiffTracking
// treats an EXACT duplicate (totalDiffs==0) as already-present and any field
// difference (totalDiffs>=1, all non-updatable) as a new insert. Distinct rules
// therefore each create a row. (v1's gin handler bypassed the diff and inserted
// even exact duplicates; v2 dedups identical rules through the shared diff path —
// a minor, intentional improvement noted in the report.)
func TestV2Maxbattle_DistinctRulesEachInsert(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":151}]`)
	if len(v2RulesArray(t, v2DecodeBody(t, w), "created")) != 1 {
		t.Fatalf("expected 1 created for a distinct rule")
	}
	if len(ms.AllRows()) != 2 {
		t.Fatalf("expected 2 rows for distinct rules, got %d", len(ms.AllRows()))
	}
}

// Exact-duplicate maxbattle rule reported as unchanged (no second row).
func TestV2Maxbattle_ExactDuplicateUnchanged(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	resp := v2DecodeBody(t, w)
	if len(v2RulesArray(t, resp, "created")) != 0 {
		t.Fatalf("exact duplicate should not be created")
	}
	if len(v2RulesArray(t, resp, "unchanged")) != 1 {
		t.Fatalf("exact duplicate should be reported unchanged")
	}
	if len(ms.AllRows()) != 1 {
		t.Fatalf("exact duplicate must not add a second row, got %d", len(ms.AllRows()))
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Maxbattle_ListShape(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150},{"pokemon_id":151}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Maxbattle_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/maxbattle/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Maxbattle_DeleteSingle(t *testing.T) {
	r, ms, pushes, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/maxbattle/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(ms.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Maxbattle_BulkDelete(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150},{"pokemon_id":151},{"pokemon_id":152}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/maxbattle?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(ms.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(ms.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Maxbattle_PutFullReplace_NewUID(t *testing.T) {
	r, ms, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150,"gmax":true}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted gmax resets to default false.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/maxbattle/"+itoa(oldUID), `{"pokemon_id":150}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := ms.AllRows()
	if len(rows) != 1 || rows[0].Gmax != 0 {
		t.Fatalf("replace did not reset gmax: %+v", rows)
	}
}

func TestV2Maxbattle_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/maxbattle/"+itoa(uid), `{"pokemon_id":150}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Maxbattle_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle", `[{"pokemon_id":150}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/maxbattle?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Maxbattle_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle?include_descriptions=true", `[{"pokemon_id":150}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Maxbattle_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/maxbattle?silent=true", `[{"pokemon_id":150}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Maxbattle_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2MaxbattleTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/maxbattle", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
