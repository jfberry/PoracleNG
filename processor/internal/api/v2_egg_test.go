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

// newV2EggTestAPI wires the strict v2 egg endpoints against mock stores through a
// REAL huma+gin engine. Mirrors newV2RaidTestAPI.
func newV2EggTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.EggTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	eggStore := store.NewMockTrackingStore[db.EggTrackingAPI](
		store.EggGetUID, store.EggSetUID,
	).WithIDScope(func(e *db.EggTrackingAPI) string { return e.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Eggs: eggStore},
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

	RegisterV2TrackingEgg(humaAPI, deps)
	return r, eggStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Egg_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2EggTestAPI(t)
	defer restore()

	body := `[{"level":5,"clean":true},{"level":3,"team":"mystic"}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", body)
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

func TestV2Egg_CreateSingleElementArray_OK(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":3}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := es.AllRows()
	if len(rows) != 1 || rows[0].Level != 3 {
		t.Fatalf("expected 1 stored row level=3, got %+v", rows)
	}
}

// --- level REQUIRED + >= 1 (v1 parity) -------------------------------------

func TestV2Egg_LevelRequiredMissing422(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"team":"any"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing level, got %d: %s", w.Code, w.Body.String())
	}
	if len(es.AllRows()) != 0 {
		t.Fatalf("rule with no level must not be stored: %+v", es.AllRows())
	}
}

func TestV2Egg_LevelBelowOne422(t *testing.T) {
	for _, lvl := range []string{"0", "-3"} {
		r, es, _, restore := newV2EggTestAPI(t)
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":`+lvl+`}]`)
		if w.Code != http.StatusUnprocessableEntity {
			restore()
			t.Fatalf("level %s: expected 422, got %d: %s", lvl, w.Code, w.Body.String())
		}
		if len(es.AllRows()) != 0 {
			restore()
			t.Fatalf("level %s: must not be stored: %+v", lvl, es.AllRows())
		}
		restore()
	}
}

// --- strict rejection: dropped level-array expansion is now a type error ----

func TestV2Egg_RejectsLevelArray422(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	// v1 accepted level as [int,...]; v2 strict ⇒ wrong type ⇒ 422.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":[3,4,5]}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for level array, got %d: %s", w.Code, w.Body.String())
	}
	if len(es.AllRows()) != 0 {
		t.Fatalf("level-array rule must not be stored: %+v", es.AllRows())
	}
}

func TestV2Egg_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Egg_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"exclusive":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string exclusive, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Egg_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Egg_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- team + rsvp enums ------------------------------------------------------

func TestV2Egg_TeamRoundTrip(t *testing.T) {
	cases := map[string]int{"harmony": 0, "mystic": 1, "valor": 2, "instinct": 3, "any": 4}
	for name, want := range cases {
		r, es, _, restore := newV2EggTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"`+name+`"}]`)
		rows := es.AllRows()
		if len(rows) != 1 || rows[0].Team != want {
			restore()
			t.Fatalf("team %s: expected stored %d, got %+v", name, want, rows)
		}
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg", "")
		out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
		// "any" is the wildcard: Part B hides it as null. Other teams are
		// meaningful and read back as their string.
		wantBack := any(name)
		if name == "any" {
			wantBack = nil
		}
		if out[0]["team"] != wantBack {
			restore()
			t.Fatalf("team %s read-back mismatch: got %v want %v", name, out[0]["team"], wantBack)
		}
		restore()
	}
}

func TestV2Egg_BadTeam422(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"purple"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad team, got %d: %s", w.Code, w.Body.String())
	}
	if len(es.AllRows()) != 0 {
		t.Fatalf("invalid team must not be stored: %+v", es.AllRows())
	}
}

func TestV2Egg_RSVPChangesRoundTrip(t *testing.T) {
	cases := map[string]int{"none": 0, "rsvp": 1, "rsvp_only": 2}
	for name, want := range cases {
		r, es, _, restore := newV2EggTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"rsvp_changes":"`+name+`"}]`)
		rows := es.AllRows()
		if len(rows) != 1 || rows[0].RSVPChanges != want {
			restore()
			t.Fatalf("rsvp %s: expected stored %d, got %+v", name, want, rows)
		}
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg", "")
		out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
		// "none" is the wildcard: Part B hides it as null.
		wantBack := any(name)
		if name == "none" {
			wantBack = nil
		}
		if out[0]["rsvp_changes"] != wantBack {
			restore()
			t.Fatalf("rsvp %s read-back mismatch: got %v want %v", name, out[0]["rsvp_changes"], wantBack)
		}
		restore()
	}
}

func TestV2Egg_BadRSVP422(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"rsvp_changes":"loud"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad rsvp_changes, got %d: %s", w.Code, w.Body.String())
	}
	if len(es.AllRows()) != 0 {
		t.Fatalf("invalid rsvp_changes must not be stored: %+v", es.AllRows())
	}
}

// --- defaults + bitmask + gym_id + exclusive -------------------------------

func TestV2Egg_DefaultsOnOmission(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)
	rows := es.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Level != 5 {
		t.Errorf("level: got %d want 5", row.Level)
	}
	if row.Team != 4 {
		t.Errorf("default team: got %d want 4 (any)", row.Team)
	}
	if bool(row.Exclusive) {
		t.Errorf("default exclusive: got %v want false", row.Exclusive)
	}
	if row.RSVPChanges != 0 {
		t.Errorf("default rsvp_changes: got %d want 0", row.RSVPChanges)
	}
	if row.Clean != 0 {
		t.Errorf("default clean: got %d want 0", row.Clean)
	}
	if row.Distance != 0 {
		t.Errorf("default distance: got %d want 0", row.Distance)
	}
	if row.GymID.Valid {
		t.Errorf("gym_id should default to invalid/null, got %q", row.GymID.String)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Egg_GymIDRoundTrip(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"gym_id":"abc"}]`)
	rows := es.AllRows()
	if len(rows) != 1 || !rows[0].GymID.Valid || rows[0].GymID.String != "abc" {
		t.Fatalf("expected stored gym_id abc, got %+v", rows)
	}
	// Empty string normalises to null (any). Distinct team so it inserts a new row.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"valor","gym_id":""}]`)
	for _, row := range es.AllRows() {
		if row.Team == 2 && row.GymID.Valid {
			t.Fatalf("empty gym_id must normalise to null, got %q", row.GymID.String)
		}
	}
}

func TestV2Egg_ExclusiveRoundTrip(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"exclusive":true}]`)
	rows := es.AllRows()
	if len(rows) != 1 || !bool(rows[0].Exclusive) {
		t.Fatalf("expected exclusive stored true, got %+v", rows)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if out[0]["exclusive"] != true {
		t.Fatalf("expected exclusive true on read-back, got %v", out[0]["exclusive"])
	}
}

func TestV2Egg_CleanEditSummaryBitmask(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"clean":true,"edit":true,"summary":true}]`)
	rows := es.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- diff behaviour (team is match key) ------------------------------------

func TestV2Egg_TeamIsMatchKey(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"mystic","distance":100}]`)
	// Same team, different distance ⇒ update (not a second row).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"mystic","distance":200}]`)
	resp := v2DecodeBody(t, w)
	if len(v2RulesArray(t, resp, "updated")) != 1 {
		t.Fatalf("same team different distance should be an update, got %v", resp)
	}
	if len(es.AllRows()) != 1 || es.AllRows()[0].Distance != 200 {
		t.Fatalf("expected single row distance 200, got %+v", es.AllRows())
	}
	// Different team ⇒ new insert.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"valor"}]`)
	if len(es.AllRows()) != 2 {
		t.Fatalf("distinct team should add a row, got %d", len(es.AllRows()))
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Egg_ListShape(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"mystic"},{"level":5,"team":"valor"}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Egg_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/egg/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Egg_DeleteSingle(t *testing.T) {
	r, es, pushes, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/egg/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(es.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Egg_BulkDelete(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"harmony"},{"level":5,"team":"mystic"},{"level":5,"team":"valor"}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/egg?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(es.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(es.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Egg_PutFullReplace_NewUID(t *testing.T) {
	r, es, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5,"team":"mystic","exclusive":true}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted exclusive resets to default false.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/egg/"+itoa(oldUID), `{"level":5,"team":"mystic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := es.AllRows()
	if len(rows) != 1 || bool(rows[0].Exclusive) {
		t.Fatalf("replace did not reset exclusive: %+v", rows)
	}
}

func TestV2Egg_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/egg/"+itoa(uid), `{"level":5}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Egg_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/egg?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Egg_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg?include_descriptions=true", `[{"level":5}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Egg_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg?silent=true", `[{"level":5}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Egg_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2EggTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/egg", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
