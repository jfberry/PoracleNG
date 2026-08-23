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

// newV2NestTestAPI wires the strict v2 nest endpoints against mock stores through
// a REAL huma+gin engine. Mirrors newV2PokemonTestAPI.
func newV2NestTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.NestTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	nestStore := store.NewMockTrackingStore[db.NestTrackingAPI](
		store.NestGetUID, store.NestSetUID,
	).WithIDScope(func(n *db.NestTrackingAPI) string { return n.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Nests: nestStore},
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

	RegisterV2TrackingNest(humaAPI, deps)
	return r, nestStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Nest_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2NestTestAPI(t)
	defer restore()

	body := `[{"pokemon_id":149,"min_spawn_avg":5,"clean":true},{"pokemon_id":384,"form":1}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", body)
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

func TestV2Nest_CreateSingleElementArray_OK(t *testing.T) {
	r, ns, _, restore := newV2NestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := ns.AllRows()
	if len(rows) != 1 || rows[0].PokemonID != 25 {
		t.Fatalf("expected 1 stored row for pikachu, got %+v", rows)
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Nest_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Nest_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string pokemon_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Nest_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/nest?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Nest_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults + bitmask -----------------------------------------------------

func TestV2Nest_DefaultsOnOmission(t *testing.T) {
	r, ns, _, restore := newV2NestTestAPI(t)
	defer restore()

	// Empty rule object: every field omitted.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{}]`)
	rows := ns.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.PokemonID != 0 || row.Form != 0 || row.MinSpawnAvg != 0 || row.Clean != 0 || row.Distance != 0 {
		t.Fatalf("unexpected defaults: %+v", row)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Nest_CleanEditSummaryBitmask(t *testing.T) {
	r, ns, _, restore := newV2NestTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25,"clean":true,"edit":true,"summary":true}]`)
	rows := ns.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Nest_ListShape(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25},{"pokemon_id":26}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/nest", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestV2Nest_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/nest/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/nest/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Nest_DeleteSingle(t *testing.T) {
	r, ns, pushes, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/nest/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(ns.AllRows()) != 0 {
		t.Fatalf("row not deleted: %+v", ns.AllRows())
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Nest_BulkDelete(t *testing.T) {
	r, ns, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25},{"pokemon_id":26},{"pokemon_id":27}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/nest?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(ns.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(ns.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Nest_PutFullReplace_NewUID(t *testing.T) {
	r, ns, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25,"min_spawn_avg":5}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted min_spawn_avg resets to default 0.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/nest/"+itoa(oldUID), `{"pokemon_id":25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	updated := v2RulesArray(t, v2DecodeBody(t, w), "updated")
	newUID := int64(updated[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid; got same %d", newUID)
	}
	rows := ns.AllRows()
	if len(rows) != 1 || rows[0].MinSpawnAvg != 0 {
		t.Fatalf("replace did not reset omitted field: %+v", rows)
	}
}

func TestV2Nest_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/nest/"+itoa(uid), `{"pokemon_id":25}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Nest_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest", `[{"pokemon_id":25}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/nest", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/nest?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Nest_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest?include_descriptions=true", `[{"pokemon_id":25}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Nest_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/nest?silent=true", `[{"pokemon_id":25}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Nest_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2NestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/nest", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
