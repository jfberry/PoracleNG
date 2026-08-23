package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2FortTestAPI wires the strict v2 fort endpoints against mock stores through
// a REAL huma+gin engine. Mirrors newV2MaxbattleTestAPI.
func newV2FortTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.FortTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	fortStore := store.NewMockTrackingStore[db.FortTrackingAPI](
		store.FortGetUID, store.FortSetUID,
	).WithIDScope(func(f *db.FortTrackingAPI) string { return f.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Forts: fortStore},
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

	RegisterV2TrackingFort(humaAPI, deps)
	return r, fortStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Fort_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2FortTestAPI(t)
	defer restore()

	body := `[{"fort_type":"pokestop"},{"fort_type":"gym","change_types":["new"]}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	if len(created) != 2 {
		t.Fatalf("expected 2 created, got %d", len(created))
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected 1 push, got %d", len(*pushes))
	}
}

func TestV2Fort_CreateSingleElementArray_OK(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].FortType != "gym" {
		t.Fatalf("expected 1 stored row fort_type=gym, got %+v", rows)
	}
}

// --- fort_type enum (string-stored) + station rejection --------------------

func TestV2Fort_FortTypeDefaultEverythingOnOmission(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].FortType != "everything" {
		t.Fatalf("omitted fort_type must default to 'everything', got %+v", rows)
	}
}

func TestV2Fort_RejectsStationFortType(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	// The bot accepts `station`; the API does not (signed-off decision #1).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"station"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for station fort_type, got %d: %s", w.Code, w.Body.String())
	}
	if len(fs.AllRows()) != 0 {
		t.Fatalf("station fort_type must not be stored: %+v", fs.AllRows())
	}
}

func TestV2Fort_RejectsBadFortType(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"bogus"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad fort_type, got %d: %s", w.Code, w.Body.String())
	}
	if len(fs.AllRows()) != 0 {
		t.Fatalf("bad fort_type must not be stored: %+v", fs.AllRows())
	}
}

func TestV2Fort_FortTypeRoundTrip(t *testing.T) {
	for _, ft := range []string{"pokestop", "gym", "everything"} {
		r, fs, _, restore := newV2FortTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"`+ft+`"}]`)
		rows := fs.AllRows()
		if len(rows) != 1 || rows[0].FortType != ft {
			restore()
			t.Fatalf("fort_type %s: expected stored string, got %+v", ft, rows)
		}
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort", "")
		out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
		// "everything" is the wildcard/default: Part B hides it as null.
		wantBack := any(ft)
		if ft == "everything" {
			wantBack = nil
		}
		if out[0]["fort_type"] != wantBack {
			restore()
			t.Fatalf("fort_type %s read-back mismatch: got %v want %v", ft, out[0]["fort_type"], wantBack)
		}
		restore()
	}
}

// --- include_empty default TRUE (KEY test) ---------------------------------

func TestV2Fort_IncludeEmptyDefaultsTrueOnOmission(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	// Omitting include_empty MUST store true (DB column default; deliberate
	// behaviour change from v1's gin handler which defaulted false).
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	rows := fs.AllRows()
	if len(rows) != 1 || !bool(rows[0].IncludeEmpty) {
		t.Fatalf("omitted include_empty must default TRUE, got %+v", rows)
	}
}

func TestV2Fort_IncludeEmptyExplicitFalse(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","include_empty":false}]`)
	rows := fs.AllRows()
	if len(rows) != 1 || bool(rows[0].IncludeEmpty) {
		t.Fatalf("explicit include_empty=false must store false, got %+v", rows)
	}
}

// --- change_types validation -----------------------------------------------

func TestV2Fort_ChangeTypesGoodListStored(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","change_types":["location","new"]}]`)
	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].ChangeTypes != `["location","new"]` {
		t.Fatalf("expected stored change_types JSON array, got %+v", rows)
	}
	// Read-back decodes to a []string.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	ct, ok := out[0]["change_types"].([]any)
	if !ok || !reflect.DeepEqual(ct, []any{"location", "new"}) {
		t.Fatalf("expected change_types [location new] on read-back, got %v", out[0]["change_types"])
	}
}

func TestV2Fort_ChangeTypesBadValue422(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","change_types":["location","photo"]}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad change_types value, got %d: %s", w.Code, w.Body.String())
	}
	if len(fs.AllRows()) != 0 {
		t.Fatalf("invalid change_types must not be stored: %+v", fs.AllRows())
	}
}

func TestV2Fort_ChangeTypesEmptyStoresBracketArray(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].ChangeTypes != "[]" {
		t.Fatalf("omitted change_types must store '[]', got %+v", rows)
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Fort_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Fort_RejectsCleanField(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	// fort has no clean column; clean is not a known field ⇒ strict 422.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","clean":true}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 rejecting clean on fort, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Fort_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"include_empty":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string include_empty, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Fort_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Fort_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults ---------------------------------------------------------------

func TestV2Fort_DefaultsOnOmission(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{}]`)
	rows := fs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.FortType != "everything" {
		t.Errorf("default fort_type: got %q want everything", row.FortType)
	}
	if !bool(row.IncludeEmpty) {
		t.Errorf("default include_empty: got false want true")
	}
	if row.ChangeTypes != "[]" {
		t.Errorf("default change_types: got %q want []", row.ChangeTypes)
	}
	if row.Distance != 0 {
		t.Errorf("default distance: got %d want 0", row.Distance)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

// --- diff behaviour (fort_type is match key) -------------------------------

func TestV2Fort_FortTypeIsMatchKey(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","distance":100}]`)
	// Same fort_type, different distance ⇒ update (not a second row).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","distance":200}]`)
	resp := v2DecodeBody(t, w)
	if len(v2RulesArray(t, resp, "updated")) != 1 {
		t.Fatalf("same fort_type different distance should be an update, got %v", resp)
	}
	if len(fs.AllRows()) != 1 || fs.AllRows()[0].Distance != 200 {
		t.Fatalf("expected single row distance 200, got %+v", fs.AllRows())
	}
	// Different fort_type ⇒ new insert.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"pokestop"}]`)
	if len(fs.AllRows()) != 2 {
		t.Fatalf("distinct fort_type should add a row, got %d", len(fs.AllRows()))
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Fort_ListShape(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"},{"fort_type":"pokestop"}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Fort_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/fort/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Fort_DeleteSingle(t *testing.T) {
	r, fs, pushes, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/fort/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(fs.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Fort_BulkDelete(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"},{"fort_type":"pokestop"},{"fort_type":"everything"}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/fort?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(fs.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(fs.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Fort_PutFullReplace_NewUID(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym","change_types":["new"]}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted change_types resets to [], include_empty back to true.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/fort/"+itoa(oldUID), `{"fort_type":"gym"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].ChangeTypes != "[]" || !bool(rows[0].IncludeEmpty) {
		t.Fatalf("replace did not reset to defaults: %+v", rows)
	}
}

func TestV2Fort_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/fort/"+itoa(uid), `{"fort_type":"gym"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Fort_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort", `[{"fort_type":"gym"}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/fort?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Fort_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort?include_descriptions=true", `[{"fort_type":"gym"}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Fort_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/fort?silent=true", `[{"fort_type":"gym"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Fort_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2FortTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/fort", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}

// TestV2Fort_RoundTrip is the Part B fixed-point check for the ENUM-DEFAULT case:
// a fort rule with fort_type=gym (meaningful) + change_types set + include_empty
// at its default (true, hidden as null) + default common fields GETs back with
// the defaults as null and survives a PUT of that body unchanged.
func TestV2Fort_RoundTrip(t *testing.T) {
	r, fs, _, restore := newV2FortTestAPI(t)
	defer restore()

	basePath := "/api/v2/humans/u1/tracking/fort"
	got := roundTripRule(t, r, basePath, `[{"fort_type":"gym","change_types":["name","description"],"distance":250}]`)

	if got["fort_type"] != "gym" {
		t.Fatalf("meaningful fort_type must be shown, got %v", got["fort_type"])
	}
	// include_empty defaults TRUE → hidden as null in the response.
	if v, present := got["include_empty"]; !present || v != nil {
		t.Fatalf("include_empty at default (true) must be present-but-null, got present=%v value=%v", present, v)
	}
	if got["change_types"] == nil {
		t.Fatalf("set change_types must be shown, got nil")
	}

	rows := fs.AllRows()
	if len(rows) != 1 || rows[0].FortType != "gym" || rows[0].Distance != 250 {
		t.Fatalf("round-trip drifted the rule: %+v", rows)
	}
	// include_empty round-trips as TRUE (null in body → default true on PUT).
	if !bool(rows[0].IncludeEmpty) {
		t.Fatalf("include_empty must remain TRUE across round-trip (null→default), got false")
	}
	if rows[0].ChangeTypes != `["name","description"]` {
		t.Fatalf("change_types drifted across round-trip: %q", rows[0].ChangeTypes)
	}
}
