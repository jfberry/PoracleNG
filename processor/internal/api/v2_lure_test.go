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

// newV2LureTestAPI wires the strict v2 lure endpoints against mock stores through
// a REAL huma+gin engine. Mirrors newV2PokemonTestAPI.
func newV2LureTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.LureTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	lureStore := store.NewMockTrackingStore[db.LureTrackingAPI](
		store.LureGetUID, store.LureSetUID,
	).WithIDScope(func(l *db.LureTrackingAPI) string { return l.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Lures: lureStore},
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

	RegisterV2TrackingLure(humaAPI, deps)
	return r, lureStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Lure_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2LureTestAPI(t)
	defer restore()

	body := `[{"lure_id":501,"clean":true},{"lure_id":502}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", body)
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

func TestV2Lure_CreateSingleElementArray_OK(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":0}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := ls.AllRows()
	if len(rows) != 1 || rows[0].LureID != 0 {
		t.Fatalf("expected 1 stored row with lure_id 0, got %+v", rows)
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Lure_RejectsMissingLureID(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"clean":true}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing lure_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Lure_RejectsBadLureID(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":999}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for out-of-set lure_id, got %d: %s", w.Code, w.Body.String())
	}
	if len(ls.AllRows()) != 0 {
		t.Fatalf("bad lure_id must not be stored: %+v", ls.AllRows())
	}
}

func TestV2Lure_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Lure_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string lure_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Lure_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/lure?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Lure_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults + bitmask -----------------------------------------------------

func TestV2Lure_DefaultsOnOmission(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501}]`)
	rows := ls.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Clean != 0 || row.Distance != 0 || row.Template != "" {
		t.Fatalf("unexpected defaults: %+v", row)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Lure_CleanEditSummaryBitmask(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501,"clean":true,"edit":true,"summary":true}]`)
	rows := ls.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Lure_ListShape(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501},{"lure_id":502}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/lure", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Lure_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/lure/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/lure/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Lure_DeleteSingle(t *testing.T) {
	r, ls, pushes, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/lure/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(ls.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Lure_BulkDelete(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501},{"lure_id":502},{"lure_id":503}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/lure?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(ls.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(ls.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Lure_PutFullReplace_NewUID(t *testing.T) {
	r, ls, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501,"distance":500}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted distance resets to default 0.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/lure/"+itoa(oldUID), `{"lure_id":502}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := ls.AllRows()
	if len(rows) != 1 || rows[0].LureID != 502 || rows[0].Distance != 0 {
		t.Fatalf("replace did not apply correctly: %+v", rows)
	}
}

func TestV2Lure_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/lure/"+itoa(uid), `{"lure_id":501}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Lure_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure", `[{"lure_id":501}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/lure", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/lure?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Lure_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure?include_descriptions=true", `[{"lure_id":501}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Lure_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/lure?silent=true", `[{"lure_id":501}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Lure_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2LureTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/lure", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
