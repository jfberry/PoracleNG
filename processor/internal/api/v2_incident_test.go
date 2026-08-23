package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2IncidentTestAPI wires the strict v2 incident endpoints (plus invasion,
// since they share the store and the cross-type tests address both) against mock
// stores through a real huma+gin engine. Reuses newV2InvasionGameData.
func newV2IncidentTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.InvasionTrackingAPI], *[]pushRecord, func()) {
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
	RegisterV2TrackingIncident(humaAPI, deps)
	return r, invStore, pushes, restore
}

// --- display_type required + validation -------------------------------------

func TestV2Incident_DisplayTypeRequired(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"distance":500}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing display_type, got %d: %s", w.Code, w.Body.String())
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("missing display_type must not be stored")
	}
}

func TestV2Incident_UnknownDisplayType422(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":999}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown display_type, got %d: %s", w.Code, w.Body.String())
	}
	if len(is.AllRows()) != 0 {
		t.Fatalf("unknown display_type must not be stored")
	}
}

func TestV2Incident_ValidStoresEventName(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "showcase" || rows[0].Gender != 0 {
		t.Fatalf("expected grunt_type=showcase gender=0, got %+v", rows)
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Incident_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9,"bogus":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Incident_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

// --- round-trip + clean -----------------------------------------------------

func TestV2Incident_DisplayTypeRoundTrip(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if dt, _ := rule["display_type"].(float64); int(dt) != 9 {
		t.Fatalf("expected display_type=9 in response, got %v", rule["display_type"])
	}
}

func TestV2Incident_CleanEditSummaryBitmask(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9,"clean":true,"edit":true,"summary":true}]`)
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- discriminator + cross-type isolation -----------------------------------

func TestV2Incident_DoesNotSeeInvasionRows(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()

	// Create an invasion (non-event) row.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("invasion create: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	invUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// incident list must NOT include the invasion row.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident", "")
	if rules := v2RulesArray(t, v2DecodeBody(t, w), "rules"); len(rules) != 0 {
		t.Fatalf("incident list must not include invasion rows, got %d", len(rules))
	}

	// GET an invasion uid via the incident endpoint → 404.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident/"+itoa(invUID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("invasion uid via incident GET: expected 404, got %d", w.Code)
	}

	// DELETE an invasion uid via the incident endpoint → 404.
	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/incident/"+itoa(invUID), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("invasion uid via incident DELETE: expected 404, got %d", w.Code)
	}
}

func TestV2Incident_CrossTypeIsolation(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()

	// Create one invasion rule AND one incident rule for the same human.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)

	// Both rows exist in the shared store.
	if len(is.AllRows()) != 2 {
		t.Fatalf("expected 2 rows in shared store, got %d", len(is.AllRows()))
	}

	// invasion list → only the invasion (grass).
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/invasion", "")
	invRules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(invRules) != 1 {
		t.Fatalf("invasion list: expected 1, got %d", len(invRules))
	}
	if _, ok := invRules[0]["type_id"]; !ok {
		t.Fatalf("invasion list entry should carry type_id: %v", invRules[0])
	}

	// incident list → only the incident (showcase).
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident", "")
	incRules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(incRules) != 1 {
		t.Fatalf("incident list: expected 1, got %d", len(incRules))
	}
	if dt, _ := incRules[0]["display_type"].(float64); int(dt) != 9 {
		t.Fatalf("incident list display_type: expected 9, got %v", incRules[0]["display_type"])
	}
}

// --- CRUD / ownership / include_descriptions / silent -----------------------

func TestV2Incident_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/incident/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

func TestV2Incident_DeleteSingle(t *testing.T) {
	r, is, pushes, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/incident/"+itoa(uid), "")
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

func TestV2Incident_PutFullReplace_NewUID(t *testing.T) {
	r, is, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9,"distance":500}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/incident/"+itoa(oldUID), `{"display_type":9}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := is.AllRows()
	if len(rows) != 1 || rows[0].GruntType != "showcase" || rows[0].Distance != 0 {
		t.Fatalf("replace did not apply correctly: %+v", rows)
	}
}

func TestV2Incident_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/incident?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Incident_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident?silent=true", `[{"display_type":9}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Incident_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2IncidentTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/incident", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
