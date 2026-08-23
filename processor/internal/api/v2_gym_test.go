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

// newV2GymTestAPI wires the strict v2 gym endpoints against mock stores through a
// REAL huma+gin engine. Mirrors newV2MaxbattleTestAPI.
func newV2GymTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.GymTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	gymStore := store.NewMockTrackingStore[db.GymTrackingAPI](
		store.GymGetUID, store.GymSetUID,
	).WithIDScope(func(g *db.GymTrackingAPI) string { return g.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Gyms: gymStore},
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

	RegisterV2TrackingGym(humaAPI, deps)
	return r, gymStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Gym_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2GymTestAPI(t)
	defer restore()

	body := `[{"team":"mystic","clean":true},{"team":"valor","slot_changes":true}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", body)
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

func TestV2Gym_CreateSingleElementArray_OK(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"instinct"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := gs.AllRows()
	if len(rows) != 1 || rows[0].Team != 3 {
		t.Fatalf("expected 1 stored row team=3, got %+v", rows)
	}
}

// --- team required + enum (v1 parity) --------------------------------------

func TestV2Gym_TeamRequiredMissing422(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"clean":true}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing team, got %d: %s", w.Code, w.Body.String())
	}
	if len(gs.AllRows()) != 0 {
		t.Fatalf("rule with no team must not be stored: %+v", gs.AllRows())
	}
}

func TestV2Gym_BadTeam422(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"purple"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad team, got %d: %s", w.Code, w.Body.String())
	}
	if len(gs.AllRows()) != 0 {
		t.Fatalf("invalid team must not be stored: %+v", gs.AllRows())
	}
}

func TestV2Gym_TeamRoundTrip(t *testing.T) {
	cases := map[string]int{"harmony": 0, "mystic": 1, "valor": 2, "instinct": 3, "any": 4}
	for name, want := range cases {
		r, gs, _, restore := newV2GymTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"`+name+`"}]`)
		rows := gs.AllRows()
		if len(rows) != 1 || rows[0].Team != want {
			restore()
			t.Fatalf("team %s: expected stored %d, got %+v", name, want, rows)
		}
		// String→int→string read-back.
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym", "")
		out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
		if out[0]["team"] != name {
			restore()
			t.Fatalf("team %s read-back mismatch: got %v", name, out[0]["team"])
		}
		restore()
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Gym_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any","bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Gym_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any","slot_changes":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string slot_changes, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Gym_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Gym_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults + bitmask + gym_id -------------------------------------------

func TestV2Gym_DefaultsOnOmission(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any"}]`)
	rows := gs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if bool(row.SlotChanges) {
		t.Errorf("default slot_changes: got %v want false", row.SlotChanges)
	}
	if bool(row.BattleChanges) {
		t.Errorf("default battle_changes: got %v want false", row.BattleChanges)
	}
	if row.Clean != 0 {
		t.Errorf("default clean: got %d want 0", row.Clean)
	}
	if row.Distance != 0 {
		t.Errorf("default distance: got %d want 0", row.Distance)
	}
	if row.GymID != nil {
		t.Errorf("gym_id should default to nil, got %v", *row.GymID)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Gym_GymIDRoundTrip(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any","gym_id":"abc"}]`)
	rows := gs.AllRows()
	if len(rows) != 1 || rows[0].GymID == nil || *rows[0].GymID != "abc" {
		t.Fatalf("expected stored gym_id abc, got %+v", rows)
	}
	// Empty string normalises to nil (any). Use a distinct team so it inserts a
	// new row rather than matching the existing gym_id=abc rule.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"valor","gym_id":""}]`)
	for _, row := range gs.AllRows() {
		if row.Team == 2 && row.GymID != nil {
			t.Fatalf("empty gym_id must normalise to nil, got %v", *row.GymID)
		}
	}
}

func TestV2Gym_CleanEditSummaryBitmask(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any","clean":true,"edit":true,"summary":true}]`)
	rows := gs.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

func TestV2Gym_SlotBattleRoundTrip(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any","slot_changes":true,"battle_changes":true}]`)
	rows := gs.AllRows()
	if len(rows) != 1 || !bool(rows[0].SlotChanges) || !bool(rows[0].BattleChanges) {
		t.Fatalf("expected slot/battle stored as true, got %+v", rows)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if out[0]["slot_changes"] != true || out[0]["battle_changes"] != true {
		t.Fatalf("expected slot/battle true on read-back, got %v / %v", out[0]["slot_changes"], out[0]["battle_changes"])
	}
}

// --- diff behaviour (team is match key) ------------------------------------

func TestV2Gym_TeamIsMatchKey(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic","distance":100}]`)
	// Same team, different distance ⇒ update (not a second row).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic","distance":200}]`)
	resp := v2DecodeBody(t, w)
	if len(v2RulesArray(t, resp, "updated")) != 1 {
		t.Fatalf("same team different distance should be an update, got %v", resp)
	}
	if len(gs.AllRows()) != 1 || gs.AllRows()[0].Distance != 200 {
		t.Fatalf("expected single row distance 200, got %+v", gs.AllRows())
	}
	// Different team ⇒ new insert.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"valor"}]`)
	if len(gs.AllRows()) != 2 {
		t.Fatalf("distinct team should add a row, got %d", len(gs.AllRows()))
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Gym_ListShape(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic"},{"team":"valor"}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Gym_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/gym/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Gym_DeleteSingle(t *testing.T) {
	r, gs, pushes, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/gym/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(gs.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Gym_BulkDelete(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"harmony"},{"team":"mystic"},{"team":"valor"}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/gym?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(gs.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(gs.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Gym_PutFullReplace_NewUID(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic","slot_changes":true}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted slot_changes resets to default false.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/gym/"+itoa(oldUID), `{"team":"mystic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := gs.AllRows()
	if len(rows) != 1 || bool(rows[0].SlotChanges) {
		t.Fatalf("replace did not reset slot_changes: %+v", rows)
	}
}

func TestV2Gym_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/gym/"+itoa(uid), `{"team":"any"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Gym_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"any"}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Gym_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym?include_descriptions=true", `[{"team":"any"}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Gym_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym?silent=true", `[{"team":"any"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Gym_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/gym", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
