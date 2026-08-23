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

// newV2InvasionGameData builds a real-ish GameData with the type / grunt /
// pokestop-event entries the invasion + incident translation paths need.
//
//   - Types: 11 = "Grass", 3 = "Water" (so type_id translates to a name and
//     reverse-maps for ToRule).
//   - Grunts: id 5 = a Grass grunt (TypeID 11, female) → grunt_type "grass",
//     gender 2.
//   - PokestopEvent: 9 = "Showcase" (the incident discriminator value; an
//     event name that is NOT a pokemon type name).
func newV2InvasionGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Types: map[int]*gamedata.TypeInfo{
			11: {TypeID: 11, Name: "Grass"},
			3:  {TypeID: 3, Name: "Water"},
		},
		Grunts: map[int]*gamedata.Grunt{
			5: {ID: 5, TypeID: 11, Gender: 2, Template: "CHARACTER_GRASS_GRUNT_FEMALE"},
		},
		Util: &gamedata.UtilData{
			PokestopEvent: map[int]gamedata.EventInfo{
				9: {Name: "Showcase"},
			},
		},
	}
}

// newV2InvasionTestAPI wires the strict v2 invasion endpoints against mock stores
// through a real huma+gin engine.
func newV2InvasionTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.InvasionTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	invStore := store.NewMockTrackingStore[db.InvasionTrackingAPI](
		store.InvasionGetUID, store.InvasionSetUID,
	).WithIDScope(func(i *db.InvasionTrackingAPI) string { return i.ID })

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Invasions: invStore},
		Config:   &config.Config{},
		RowText: &rowtext.Generator{
			GD:                  newV2InvasionGameData(),
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

	RegisterV2TrackingInvasion(humaAPI, deps)
	return r, invStore, pushes, restore
}

// --- mode translation -------------------------------------------------------

func TestV2Invasion_TypeIDWithGender(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"gender":"female"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "grass" || rows[0].Gender != 2 {
		t.Fatalf("expected grunt_type=grass gender=2, got %+v", rows)
	}
}

func TestV2Invasion_TypeIDDefaultGender(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":3}]`)
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "water" || rows[0].Gender != 0 {
		t.Fatalf("expected grunt_type=water gender=0 (any), got %+v", rows)
	}
}

func TestV2Invasion_GruntID(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	// grunt 5 is a Grass(11) female(2) grunt → grunt_type "grass", gender 2.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"grunt_id":5}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "grass" || rows[0].Gender != 2 {
		t.Fatalf("expected grunt_type=grass gender=2 (grunt's own), got %+v", rows)
	}
}

func TestV2Invasion_Everything(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"everything":true}]`)
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "everything" || rows[0].Gender != 0 {
		t.Fatalf("expected grunt_type=everything gender=0, got %+v", rows)
	}
}

func TestV2Invasion_Boss(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"boss":true}]`)
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "boss" || rows[0].Gender != 0 {
		t.Fatalf("expected grunt_type=boss gender=0, got %+v", rows)
	}
}

// --- exactly-one-mode + gender placement ------------------------------------

func TestV2Invasion_NoModeSet422(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"distance":500}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for no mode set, got %d: %s", w.Code, w.Body.String())
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("no-mode rule must not be stored: %+v", is.AllRows())
	}
}

func TestV2Invasion_TwoModesSet422(t *testing.T) {
	cases := []string{
		`[{"type_id":11,"everything":true}]`,
		`[{"type_id":11,"grunt_id":5}]`,
		`[{"everything":true,"boss":true}]`,
		`[{"grunt_id":5,"boss":true}]`,
	}
	for _, body := range cases {
		r, is, _, restore := newV2InvasionTestAPI(t)
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", body)
		if w.Code != http.StatusUnprocessableEntity {
			restore()
			t.Fatalf("body %s: expected 422 for two modes, got %d: %s", body, w.Code, w.Body.String())
		}
		if len(is.AllRows()) != 0 {
			restore()
			t.Fatalf("body %s: two-mode rule must not be stored", body)
		}
		restore()
	}
}

func TestV2Invasion_GenderWithNonTypeID422(t *testing.T) {
	cases := []string{
		`[{"grunt_id":5,"gender":"male"}]`,
		`[{"everything":true,"gender":"male"}]`,
		`[{"boss":true,"gender":"female"}]`,
	}
	for _, body := range cases {
		r, is, _, restore := newV2InvasionTestAPI(t)
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", body)
		if w.Code != http.StatusUnprocessableEntity {
			restore()
			t.Fatalf("body %s: expected 422 for gender with non-type_id mode, got %d: %s", body, w.Code, w.Body.String())
		}
		if len(is.AllRows()) != 0 {
			restore()
			t.Fatalf("body %s: must not be stored", body)
		}
		restore()
	}
}

func TestV2Invasion_UnknownTypeID422(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":999}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown type_id, got %d: %s", w.Code, w.Body.String())
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("unknown type_id must not be stored")
	}
}

func TestV2Invasion_UnknownGruntID422(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"grunt_id":777}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown grunt_id, got %d: %s", w.Code, w.Body.String())
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("unknown grunt_id must not be stored")
	}
}

func TestV2Invasion_RejectsBadGender(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	// genderless is NOT valid for invasion (only any|male|female).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"gender":"genderless"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for genderless (not in invasion enum), got %d: %s", w.Code, w.Body.String())
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Invasion_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"bogus":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Invasion_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/invasion?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Invasion_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d", w.Code)
	}
}

// --- round-trip -------------------------------------------------------------

func TestV2Invasion_TypeIDRoundTrip(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"gender":"female"}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if tid, _ := rule["type_id"].(float64); int(tid) != 11 {
		t.Fatalf("expected type_id=11 in response, got %v", rule["type_id"])
	}
	if g, _ := rule["gender"].(string); g != "female" {
		t.Fatalf("expected gender=female in response, got %v", rule["gender"])
	}
}

func TestV2Invasion_EverythingRoundTrip(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"everything":true}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if e, _ := rule["everything"].(bool); !e {
		t.Fatalf("expected everything=true in response, got %v", rule["everything"])
	}
	if _, ok := rule["type_id"]; ok {
		t.Fatalf("everything rule should not carry type_id: %v", rule)
	}
}

func TestV2Invasion_BossRoundTrip(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"boss":true}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if b, _ := rule["boss"].(bool); !b {
		t.Fatalf("expected boss=true in response, got %v", rule["boss"])
	}
}

// --- clean/edit/summary -----------------------------------------------------

func TestV2Invasion_CleanEditSummaryBitmask(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"clean":true,"edit":true,"summary":true}]`)
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- CRUD / ownership / include_descriptions / silent -----------------------

func TestV2Invasion_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/invasion/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/invasion/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

func TestV2Invasion_DeleteSingle(t *testing.T) {
	r, is, pushes, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/invasion/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Invasion_BulkDelete(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11},{"type_id":3},{"everything":true}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/invasion?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(is.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(is.AllRows()))
	}
}

func TestV2Invasion_PutFullReplace_NewUID(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11,"distance":500}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/invasion/"+itoa(oldUID), `{"type_id":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "water" || rows[0].Distance != 0 {
		t.Fatalf("replace did not apply correctly: %+v", rows)
	}
}

func TestV2Invasion_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/invasion?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Invasion_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion?silent=true", `[{"type_id":11}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Invasion_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2InvasionTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/invasion", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}

// TestV2Invasion_RoundTrip_TypeIDMode is the Part B fixed-point check for the
// invasion MODE case: a type_id rule (with male gender + default common fields)
// GETs back with the active mode present (type_id), gender shown (male is not the
// wildcard), and the inactive modes (everything/boss/grunt_id) absent; PUTting
// that body back leaves the stored rule unchanged.
func TestV2Invasion_RoundTrip_TypeIDMode(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	// type_id 11 = grass; male gender is meaningful (not the 'any' wildcard).
	basePath := "/api/v2/humans/u1/tracking/invasion"
	got := roundTripRule(t, r, basePath, `[{"type_id":11,"gender":"male","distance":300}]`)

	// Active mode present; gender shown; inactive modes absent.
	if got["type_id"] == nil {
		t.Fatalf("active mode type_id must be present, got %v", got)
	}
	if got["gender"] != "male" {
		t.Fatalf("meaningful gender must be shown, got %v", got["gender"])
	}
	for _, k := range []string{"everything", "boss", "grunt_id"} {
		if v, present := got[k]; present && v != nil {
			t.Fatalf("inactive mode %s must be absent/null, got %v", k, v)
		}
	}

	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "grass" || rows[0].Gender != 1 || rows[0].Distance != 300 {
		t.Fatalf("round-trip drifted the rule: %+v", rows)
	}
}

// TestV2Invasion_RoundTrip_EverythingMode checks the catch-all mode: everything
// stays present, gender is null (not type_id mode), other modes absent, and the
// rule round-trips unchanged.
func TestV2Invasion_RoundTrip_EverythingMode(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	basePath := "/api/v2/humans/u1/tracking/invasion"
	got := roundTripRule(t, r, basePath, `[{"everything":true,"clean":true}]`)

	if got["everything"] != true {
		t.Fatalf("active mode everything must be present (true), got %v", got["everything"])
	}
	if v, present := got["type_id"]; present && v != nil {
		t.Fatalf("type_id must be absent in everything mode, got %v", v)
	}
	if got["gender"] != nil {
		t.Fatalf("gender must be null outside type_id mode, got %v", got["gender"])
	}

	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "everything" || !db.IsClean(rows[0].Clean) {
		t.Fatalf("round-trip drifted the everything-mode rule: %+v", rows)
	}
}

// A PUT whose body is an exact duplicate of a DIFFERENT rule must 409
// before anything is deleted. On databases carrying the legacy invasion
// unique key this exact flow used to delete the addressed rule and then
// fail the insert with a duplicate-key error — deterministic data loss.
func TestV2Invasion_PutDuplicateOfOtherRule_Conflict(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	// Rule A: grass grunts. Rule B: water grunts.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion",
		`[{"type_id":11},{"type_id":3}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	if len(created) != 2 {
		t.Fatalf("expected 2 created rules, got %d", len(created))
	}
	uidB := int64(created[1]["uid"].(float64))

	// PUT B with a body identical to A.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/invasion/"+itoa(uidB), `{"type_id":11}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate-of-other-rule PUT, got %d: %s", w.Code, w.Body.String())
	}

	// Nothing may have been deleted: both rules intact.
	if rows := is.AllRows(); len(rows) != 2 {
		t.Fatalf("expected both rules to survive the rejected PUT, got %d: %+v", len(rows), rows)
	}
}

// A PUT that keeps the addressed rule's own identity (tweaking only
// updatable fields) must NOT conflict with itself.
func TestV2Invasion_PutSelfReplace_NoConflict(t *testing.T) {
	r, is, _, restore := newV2InvasionTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/invasion/"+itoa(uid), `{"type_id":11,"distance":750}`)
	if w.Code != http.StatusOK {
		t.Fatalf("self-identity PUT should succeed, got %d: %s", w.Code, w.Body.String())
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].Distance != 750 {
		t.Fatalf("replace did not apply: %+v", rows)
	}
}
