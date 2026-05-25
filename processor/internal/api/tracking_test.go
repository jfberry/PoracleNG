package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestGinContext creates a gin.Context backed by the given recorder.
func newTestGinContext(w *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	return c
}

// --- IntBool JSON serialization tests ---

func TestIntBoolMarshal(t *testing.T) {
	tests := []struct {
		val  db.IntBool
		want string
	}{
		{db.IntBool(false), "0"},
		{db.IntBool(true), "1"},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.val)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tt.want {
			t.Errorf("Marshal(%v) = %s, want %s", tt.val, b, tt.want)
		}
	}
}

func TestIntBoolUnmarshal(t *testing.T) {
	tests := []struct {
		input string
		want  db.IntBool
	}{
		{"true", true},
		{"false", false},
		{"0", false},
		{"1", true},
		{"2", true},  // any non-zero integer is truthy
		{"-1", true}, // negative int is truthy
		{"null", false},
	}
	for _, tt := range tests {
		var got db.IntBool
		err := json.Unmarshal([]byte(tt.input), &got)
		if err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Unmarshal(%s) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIntBoolInStruct(t *testing.T) {
	// Simulate the exact JSON a PoracleWeb client would receive
	type LureRow struct {
		UID      int64      `json:"uid"`
		Clean    db.IntBool `json:"clean"`
		Distance int        `json:"distance"`
		LureID   int        `json:"lure_id"`
	}

	row := LureRow{UID: 1, Clean: true, Distance: 500, LureID: 501}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}

	// Must contain "clean":1, not "clean":true
	s := string(b)
	if !strings.Contains(s, `"clean":1`) {
		t.Errorf("expected clean:1 in JSON, got %s", s)
	}

	// And unmarshal with integer input (as a client would POST)
	input := `{"uid":2,"clean":1,"distance":300,"lure_id":502}`
	var parsed LureRow
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Clean != true {
		t.Errorf("expected clean=true from input 1, got %v", parsed.Clean)
	}

	// Also accept boolean input
	input2 := `{"uid":3,"clean":true,"distance":100,"lure_id":0}`
	var parsed2 LureRow
	if err := json.Unmarshal([]byte(input2), &parsed2); err != nil {
		t.Fatal(err)
	}
	if parsed2.Clean != true {
		t.Errorf("expected clean=true from input true, got %v", parsed2.Clean)
	}
}

// --- diffTracking tests ---

func TestDiffTrackingDuplicate(t *testing.T) {
	a := &db.LureTrackingAPI{UID: 1, ID: "u1", ProfileNo: 1, Ping: "", Clean: 0, Distance: 500, Template: "1", LureID: 501}
	b := &db.LureTrackingAPI{UID: 0, ID: "u1", ProfileNo: 1, Ping: "", Clean: 0, Distance: 500, Template: "1", LureID: 501}

	noMatch, isDup, _, isUpdate := DiffTracking(a, b)
	if noMatch {
		t.Error("expected match")
	}
	if !isDup {
		t.Error("expected duplicate")
	}
	if isUpdate {
		t.Error("should not be update")
	}
}

func TestDiffTrackingNoMatch(t *testing.T) {
	a := &db.LureTrackingAPI{UID: 1, ID: "u1", ProfileNo: 1, LureID: 501}
	b := &db.LureTrackingAPI{UID: 0, ID: "u1", ProfileNo: 1, LureID: 502} // different match key

	noMatch, _, _, _ := DiffTracking(a, b)
	if !noMatch {
		t.Error("expected noMatch for different lure_id")
	}
}

func TestDiffTrackingSingleUpdate(t *testing.T) {
	// Exactly one updatable field differs → update
	a := &db.LureTrackingAPI{UID: 5, ID: "u1", ProfileNo: 1, Clean: 0, Distance: 500, Template: "1", LureID: 501}
	b := &db.LureTrackingAPI{UID: 0, ID: "u1", ProfileNo: 1, Clean: 0, Distance: 1000, Template: "1", LureID: 501}

	noMatch, isDup, uid, isUpdate := DiffTracking(a, b)
	if noMatch || isDup {
		t.Error("expected neither noMatch nor duplicate")
	}
	if !isUpdate {
		t.Error("expected update since only distance differs (diff:\"update\")")
	}
	if uid != 5 {
		t.Errorf("expected uid=5, got %d", uid)
	}
}

func TestDiffTrackingMultiUpdateIsInsert(t *testing.T) {
	// Multiple updatable fields differ → new insert (not update)
	a := &db.LureTrackingAPI{UID: 5, ID: "u1", ProfileNo: 1, Clean: 0, Distance: 500, Template: "1", LureID: 501}
	b := &db.LureTrackingAPI{UID: 0, ID: "u1", ProfileNo: 1, Clean: 1, Distance: 1000, Template: "2", LureID: 501}

	noMatch, isDup, _, isUpdate := DiffTracking(a, b)
	if noMatch || isDup {
		t.Error("expected neither noMatch nor duplicate")
	}
	if isUpdate {
		t.Error("expected new insert when multiple updatable fields differ")
	}
}

func TestDiffTrackingNewInsert(t *testing.T) {
	// Ping has no diff tag → non-updatable. Differ on ping → new insert.
	a := &db.LureTrackingAPI{UID: 5, ID: "u1", ProfileNo: 1, Ping: "role1", LureID: 501}
	b := &db.LureTrackingAPI{UID: 0, ID: "u1", ProfileNo: 1, Ping: "role2", LureID: 501}

	noMatch, isDup, _, isUpdate := DiffTracking(a, b)
	if noMatch || isDup || isUpdate {
		t.Error("expected new insert when non-updatable field differs")
	}
}

// --- JSON response format tests ---

func TestTrackingJSONOK(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestGinContext(w)
	trackingJSONOK(c, map[string]any{"count": 3})

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
	if resp["count"] != float64(3) {
		t.Errorf("expected count=3, got %v", resp["count"])
	}
}

func TestTrackingJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestGinContext(w)
	trackingJSONError(c, http.StatusNotFound, "User not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "error" {
		t.Errorf("expected status=error, got %v", resp["status"])
	}
	if resp["message"] != "User not found" {
		t.Errorf("expected message='User not found', got %v", resp["message"])
	}
}

func TestTrackingJSONOKNilData(t *testing.T) {
	w := httptest.NewRecorder()
	c := newTestGinContext(w)
	trackingJSONOK(c, nil)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
}

// --- isTruthy / isFalsy ---

func TestIsTruthy(t *testing.T) {
	for _, s := range []string{"true", "True", "TRUE", "1", "yes", "Yes"} {
		if !isTruthy(s) {
			t.Errorf("expected isTruthy(%q) = true", s)
		}
	}
	for _, s := range []string{"false", "0", "no", "", "maybe"} {
		if isTruthy(s) {
			t.Errorf("expected isTruthy(%q) = false", s)
		}
	}
}

// --- Handler error paths (no DB needed) ---

func TestHandlerMissingID(t *testing.T) {
	deps := &TrackingDeps{} // nil DB — should fail before DB call
	handler := HandleGetLure(deps)

	r := gin.New()
	// Register without :id param so gin provides empty string
	r.GET("/api/tracking/lure/", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/tracking/lure/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing id, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "error" {
		t.Errorf("expected error status, got %v", resp["status"])
	}
}

// TestPathParamCapturesEncodedSlashes proves that percent-encoded slashes in a
// path param (e.g. webhook URLs used as human IDs) round-trip as a single
// captured value rather than fanning out across path segments. This requires
// UseRawPath + UnescapePathValues on the engine.
func TestPathParamCapturesEncodedSlashes(t *testing.T) {
	r := gin.New()
	r.UseRawPath = true
	r.UnescapePathValues = true

	var captured string
	r.GET("/api/humans/:id", func(c *gin.Context) {
		captured = c.Param("id")
		c.Status(http.StatusOK)
	})

	// Fake webhook-style id — structure mirrors Discord's webhook URL so the
	// test exercises the same encoded-slash routing case without a real token.
	webhookID := "https://discord.com/api/webhooks/000000000000000000/REDACTED-PLACEHOLDER-TOKEN-FOR-TEST-FIXTURE-ONLY"
	encoded := url.PathEscape(webhookID)
	// PathEscape leaves ':' and '-' alone but encodes '/' as %2F — matching
	// what PHP's rawurlencode produces for these characters.
	req := httptest.NewRequest(http.MethodGet, "/api/humans/"+encoded, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (captured=%q)", w.Code, captured)
	}
	if captured != webhookID {
		t.Errorf("expected id %q, got %q", webhookID, captured)
	}
}

func TestDeleteInvalidUID(t *testing.T) {
	deps := &TrackingDeps{}
	handler := HandleDeleteLure(deps)

	r := gin.New()
	r.DELETE("/api/tracking/lure/:id/byUid/:uid", handler)

	req := httptest.NewRequest(http.MethodDelete, "/api/tracking/lure/user1/byUid/notanumber", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid uid, got %d", w.Code)
	}
}

func TestCreateLureInvalidBody(t *testing.T) {
	deps := &TrackingDeps{}
	handler := HandleCreateLure(deps)

	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/tracking/lure/:id", handler)

	req := httptest.NewRequest(http.MethodPost, "/api/tracking/lure/user1", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail at lookupHuman (nil DB panics) — recovery middleware returns 500
	if w.Code == http.StatusOK {
		t.Error("expected error for invalid body, got 200")
	}
}

// --- Full struct JSON round-trip (backwards compat) ---

func TestEggTrackingAPIJSON(t *testing.T) {
	egg := db.EggTrackingAPI{
		UID:       42,
		ID:        "discord:123",
		ProfileNo: 1,
		Clean:     1,
		Exclusive: false,
		Distance:  1000,
		Template:  "1",
		Team:      2,
		Level:     5,
	}

	b, err := json.Marshal(egg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	// Booleans must be integers
	if !strings.Contains(s, `"clean":1`) {
		t.Errorf("clean should be 1, got %s", s)
	}
	if !strings.Contains(s, `"exclusive":0`) {
		t.Errorf("exclusive should be 0, got %s", s)
	}

	// Round-trip with integer input
	input := `{"uid":99,"id":"t:1","profile_no":1,"clean":1,"exclusive":1,"distance":500,"template":"2","team":0,"level":3}`
	var parsed db.EggTrackingAPI
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Clean != 1 || !bool(parsed.Exclusive) {
		t.Errorf("expected clean=1, exclusive=true from integer 1")
	}
}

func TestGymTrackingAPIJSON(t *testing.T) {
	gym := db.GymTrackingAPI{
		Clean:         1,
		SlotChanges:   false,
		BattleChanges: true,
	}

	b, _ := json.Marshal(gym)
	s := string(b)

	if !strings.Contains(s, `"clean":1`) {
		t.Errorf("clean should be 1, got %s", s)
	}
	if !strings.Contains(s, `"slot_changes":0`) {
		t.Errorf("slot_changes should be 0, got %s", s)
	}
	if !strings.Contains(s, `"battle_changes":1`) {
		t.Errorf("battle_changes should be 1, got %s", s)
	}
}

func TestQuestTrackingAPIJSON(t *testing.T) {
	quest := db.QuestTrackingAPI{
		Clean: 1,
		Shiny: false,
	}

	b, _ := json.Marshal(quest)
	s := string(b)

	if !strings.Contains(s, `"clean":1`) {
		t.Errorf("clean should be 1, got %s", s)
	}
	if !strings.Contains(s, `"shiny":0`) {
		t.Errorf("shiny should be 0, got %s", s)
	}
}

// --- Override validation tests ---

// newTestTrackingDeps builds a minimal TrackingDeps suitable for override-validation tests.
// deps.Tracking is intentionally nil: validation failures return before it is accessed.
func newTestTrackingDeps(t *testing.T) (*TrackingDeps, *store.MockHumanStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mock := store.NewMockHumanStore()
	mock.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})
	deps := &TrackingDeps{
		Humans:       mock,
		Config:       &config.Config{},
		RowText:      &rowtext.Generator{DefaultTemplateName: "1"},
		Translations: i18n.NewBundle(),
		// Tracking intentionally nil: override validation rejects before SelectByIDProfile.
	}
	return deps, mock
}

func TestTrackingAPI_RejectsLocationWithoutDistance(t *testing.T) {
	deps, _ := newTestTrackingDeps(t)

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":0,"override_location_label":"Home"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "requires distance") {
		t.Fatalf("expected 400/requires-distance, got %d / %s", w.Code, w.Body.String())
	}
}

func TestTrackingAPI_RejectsUnknownLocation(t *testing.T) {
	deps, _ := newTestTrackingDeps(t)

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":500,"override_location_label":"Nope"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown location label") {
		t.Fatalf("expected 400/unknown-location, got %d / %s", w.Code, w.Body.String())
	}
}

// TestTrackingAPI_RejectsAreaWithDistance verifies that combining override_areas
// and a non-zero distance is rejected (they are mutually exclusive modes).
func TestTrackingAPI_RejectsAreaWithDistance(t *testing.T) {
	deps, _ := newTestTrackingDeps(t)

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":500,"override_areas":["london"]}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "mutually exclusive") {
		t.Fatalf("expected 400/mutually-exclusive, got %d / %s", w.Code, w.Body.String())
	}
}

// TestTrackingAPI_RejectsLocationWithArea verifies that combining
// override_location_label and override_areas is rejected.
func TestTrackingAPI_RejectsLocationWithArea(t *testing.T) {
	deps, mock := newTestTrackingDeps(t)
	// Seed the location so label validation passes; the area+location conflict
	// is checked first, so it would still reject, but seeding keeps the test
	// independent of validation ordering.
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 1, Longitude: 2})

	r := gin.New()
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	body := `[{"pokemon_id":25,"distance":500,"override_location_label":"Home","override_areas":["london"]}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u1", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "mutually exclusive") {
		t.Fatalf("expected 400/mutually-exclusive, got %d / %s", w.Code, w.Body.String())
	}
}

// TestTrackingAPI_RejectsAreaNotPermitted verifies that when a target user has
// limited community membership, override_areas referencing an out-of-scope area
// is rejected. This test would have caught the privilege-escalation bug in Fix C
// where the API treated every caller as admin regardless of community membership.
func TestTrackingAPI_RejectsAreaNotPermitted(t *testing.T) {
	mock := store.NewMockHumanStore()
	// User u2 belongs to community "teamcity" — only "london" is in that community.
	mock.AddHuman(&store.Human{
		ID:                  "u2",
		Type:                "discord:user",
		Name:                "LimitedUser",
		Enabled:             true,
		Language:            "en",
		CurrentProfileNo:    1,
		CommunityMembership: []string{"teamcity"},
	})

	// Build area_security config with "teamcity" community that only has "london".
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{
			Name:         "teamcity",
			AllowedAreas: []string{"london"},
		},
	}

	// Build AreaLogic with two fences: london (in teamcity) and berlin (not in teamcity).
	fences := []geofence.Fence{
		{Name: "london", UserSelectable: true},
		{Name: "berlin", UserSelectable: true},
	}
	areaLogic := bot.NewAreaLogic(fences, cfg)

	deps := &TrackingDeps{
		Humans:       mock,
		Config:       cfg,
		RowText:      &rowtext.Generator{DefaultTemplateName: "1"},
		Translations: i18n.NewBundle(),
		AreaLogic:    areaLogic,
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/tracking/pokemon/:id", HandleCreateMonster(deps))

	// u2 requests override_areas:["berlin"] — not in their community's allowed areas.
	body := `[{"pokemon_id":25,"override_areas":["berlin"]}]`
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u2", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not permitted") {
		t.Fatalf("expected 400/not-permitted, got %d / %s", w.Code, w.Body.String())
	}

	// Confirm that "london" (in their community) is accepted through validation
	// (will fail later at nil Tracking store, but with a different error — not a permission error).
	body2 := `[{"pokemon_id":25,"override_areas":["london"]}]`
	req2 := httptest.NewRequest(http.MethodPost, "/api/tracking/pokemon/u2", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	// Must NOT be rejected as "not permitted" — london is in the user's community.
	if w2.Code == http.StatusBadRequest && strings.Contains(w2.Body.String(), "not permitted") {
		t.Fatalf("london should be permitted for teamcity user, got %d / %s", w2.Code, w2.Body.String())
	}
}
