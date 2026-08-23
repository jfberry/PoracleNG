package api

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// --- harness ----------------------------------------------------------------

// newV2ProfilesTestAPI wires the strict v2 profiles endpoints against a mock
// human store. It seeds human u1 with a single default profile (profile_no 1).
// Returns the engine, the mock store, and a reload counter.
func newV2ProfilesTestAPI(t *testing.T) (*gin.Engine, *store.MockHumanStore, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true,
		Language: "en", CurrentProfileNo: 1,
	})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Name: "Default"})

	mgr := state.NewManager()
	mgr.Set(&state.State{})

	var reloads int32
	deps := &TrackingDeps{
		Humans:   humans,
		StateMgr: mgr,
		Config:   &config.Config{},
		ReloadFunc: func() {
			atomic.AddInt32(&reloads, 1)
		},
	}

	RegisterV2Profiles(humaAPI, deps)
	return r, humans, &reloads
}

// profileByNo returns the seeded profile with the given profile_no, or nil.
func profileByNo(m *store.MockHumanStore, id string, no int) *store.Profile {
	profiles, _ := m.GetProfiles(id)
	for i := range profiles {
		if profiles[i].ProfileNo == no {
			return &profiles[i]
		}
	}
	return nil
}

// --- list -------------------------------------------------------------------

func TestV2Profiles_List_OK(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/profiles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	profiles, ok := body["profiles"].([]any)
	if !ok || len(profiles) != 1 {
		t.Fatalf("expected one profile, got %v", body)
	}
	p := profiles[0].(map[string]any)
	if p["name"] != "Default" {
		t.Fatalf("expected Default profile, got %v", p)
	}
	// active_hours must be a JSON ARRAY (the strict v2 shape), never a string.
	// A profile with no schedule serialises to [].
	ah, ok := p["active_hours"].([]any)
	if !ok {
		t.Fatalf("active_hours should be a JSON array, got %T (%v)", p["active_hours"], p["active_hours"])
	}
	if len(ah) != 0 {
		t.Fatalf("expected empty active_hours array for unscheduled profile, got %v", ah)
	}
}

// TestV2Profiles_List_ActiveHoursArray_RoundTrip sets a schedule via PATCH and
// reads it back via the list endpoint, asserting active_hours comes back as a
// typed JSON ARRAY (not the legacy raw JSON string) with the entry fields
// intact — covering both a single-fire and a range (step>0) entry.
func TestV2Profiles_List_ActiveHoursArray_RoundTrip(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)

	// PATCH a schedule onto profile_no 1: one single-fire + one range entry.
	patch := `{"active_hours":[` +
		`{"day":3,"hours":12,"mins":15},` +
		`{"day":5,"hours":8,"mins":0,"step":2,"end_hours":18,"end_mins":30}` +
		`]}`
	wp := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1", patch)
	if wp.Code != http.StatusOK {
		t.Fatalf("PATCH expected 200, got %d: %s", wp.Code, wp.Body.String())
	}

	// Read it back through the list endpoint.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/profiles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	profiles := body["profiles"].([]any)
	p := profiles[0].(map[string]any)

	ah, ok := p["active_hours"].([]any)
	if !ok {
		t.Fatalf("active_hours should be a JSON array, got %T (%v)", p["active_hours"], p["active_hours"])
	}
	if len(ah) != 2 {
		t.Fatalf("expected 2 active_hours entries, got %d: %v", len(ah), ah)
	}

	// Single-fire entry: no end_hours/end_mins.
	e0 := ah[0].(map[string]any)
	if e0["day"] != float64(3) || e0["hours"] != float64(12) || e0["mins"] != float64(15) {
		t.Fatalf("entry 0 mismatch: %v", e0)
	}
	if _, present := e0["end_hours"]; present {
		t.Fatalf("single-fire entry should omit end_hours, got %v", e0)
	}

	// Range entry: end_hours/end_mins present.
	e1 := ah[1].(map[string]any)
	if e1["day"] != float64(5) || e1["step"] != float64(2) ||
		e1["end_hours"] != float64(18) || e1["end_mins"] != float64(30) {
		t.Fatalf("entry 1 (range) mismatch: %v", e1)
	}
}

func TestV2Profiles_List_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/profiles", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- switch -----------------------------------------------------------------

func TestV2Profiles_Switch_OK(t *testing.T) {
	r, humans, reloads := newV2ProfilesTestAPI(t)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Second"})

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profile", `{"profile_no":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body)
	}
	stored, _ := humans.Get("u1")
	if stored.CurrentProfileNo != 2 {
		t.Fatalf("expected active profile 2, got %d", stored.CurrentProfileNo)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Profiles_Switch_Nonexistent(t *testing.T) {
	r, _, reloads := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profile", `{"profile_no":99}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent profile, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("nonexistent profile must not reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Profiles_Switch_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/profile", `{"profile_no":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- add --------------------------------------------------------------------

func TestV2Profiles_Add_OK_WithActiveHours(t *testing.T) {
	r, humans, reloads := newV2ProfilesTestAPI(t)
	// A single-fire entry (Sun 08:30) plus a range entry (Mon 09:00 -> 17:00 step 2h).
	body := `{"name":"Work","active_hours":[` +
		`{"day":0,"hours":8,"mins":30},` +
		`{"day":1,"hours":9,"mins":0,"step":2,"end_hours":17,"end_mins":0}` +
		`]}`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profiles", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
	// New profile (profile_no 2) created; its active_hours string round-trips
	// through db.ParseActiveHours.
	p := profileByNo(humans, "u1", 2)
	if p == nil || p.Name != "Work" {
		t.Fatalf("expected Work profile created, got %+v", p)
	}
	entries, err := db.ParseActiveHours(p.ActiveHours)
	if err != nil {
		t.Fatalf("stored active_hours does not parse: %v (raw=%q)", err, p.ActiveHours)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 parsed entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Day != 0 || entries[0].Hours != 8 || entries[0].Mins != 30 || entries[0].IsRange() {
		t.Fatalf("single-fire entry not round-tripped: %+v", entries[0])
	}
	if !entries[1].IsRange() || entries[1].Step != 2 || entries[1].EndHours != 17 || entries[1].EndMins != 0 {
		t.Fatalf("range entry not round-tripped: %+v", entries[1])
	}
}

func TestV2Profiles_Add_OK_NoActiveHours(t *testing.T) {
	r, humans, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profiles", `{"name":"Simple"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	p := profileByNo(humans, "u1", 2)
	if p == nil {
		t.Fatalf("expected new profile created")
	}
	entries, err := db.ParseActiveHours(p.ActiveHours)
	if err != nil {
		t.Fatalf("active_hours should parse cleanly: %v (raw=%q)", err, p.ActiveHours)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no schedule, got %+v", entries)
	}
}

func TestV2Profiles_Add_EmptyName_422(t *testing.T) {
	r, _, reloads := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profiles", `{"name":""}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty name, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("invalid add must not reload")
	}
}

func TestV2Profiles_Add_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/profiles", `{"name":"X"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- update active_hours ----------------------------------------------------

func TestV2Profiles_Update_OK(t *testing.T) {
	r, humans, reloads := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":3,"hours":12,"mins":15}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
	p := profileByNo(humans, "u1", 1)
	entries, err := db.ParseActiveHours(p.ActiveHours)
	if err != nil {
		t.Fatalf("stored active_hours does not parse: %v (raw=%q)", err, p.ActiveHours)
	}
	if len(entries) != 1 || entries[0].Day != 3 || entries[0].Hours != 12 || entries[0].Mins != 15 {
		t.Fatalf("active_hours not stored correctly: %+v", entries)
	}
}

func TestV2Profiles_Update_Clears(t *testing.T) {
	r, humans, _ := newV2ProfilesTestAPI(t)
	// First set something.
	v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":3,"hours":12,"mins":15}]}`)
	// Now clear with an empty array.
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	p := profileByNo(humans, "u1", 1)
	entries, err := db.ParseActiveHours(p.ActiveHours)
	if err != nil {
		t.Fatalf("active_hours should parse cleanly: %v (raw=%q)", err, p.ActiveHours)
	}
	if len(entries) != 0 {
		t.Fatalf("expected cleared schedule, got %+v", entries)
	}
}

func TestV2Profiles_Update_BoundsViolation_Day(t *testing.T) {
	r, _, reloads := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":7,"hours":1,"mins":0}]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for day=7, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("invalid update must not reload")
	}
}

func TestV2Profiles_Update_BoundsViolation_Hours(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":1,"hours":24,"mins":0}]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for hours=24, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Profiles_Update_StepMissingEnd_422(t *testing.T) {
	r, _, reloads := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":1,"hours":9,"mins":0,"step":2}]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for step>0 missing end_*, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("invalid update must not reload")
	}
}

func TestV2Profiles_Update_CrossMidnight_422(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	// end (08:00) earlier than start (20:00) ⇒ cross-midnight, rejected.
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/profiles/1",
		`{"active_hours":[{"day":1,"hours":20,"mins":0,"step":2,"end_hours":8,"end_mins":0}]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for cross-midnight range, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Profiles_Update_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/nope/profiles/1",
		`{"active_hours":[]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- delete -----------------------------------------------------------------

func TestV2Profiles_Delete_OK(t *testing.T) {
	r, humans, reloads := newV2ProfilesTestAPI(t)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Second"})

	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/profiles/2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
	if profileByNo(humans, "u1", 2) != nil {
		t.Fatalf("profile 2 should be deleted")
	}
	if profileByNo(humans, "u1", 1) == nil {
		t.Fatalf("default profile should remain")
	}
}

func TestV2Profiles_Delete_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/nope/profiles/1", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- copy -------------------------------------------------------------------

func TestV2Profiles_Copy_OK(t *testing.T) {
	r, humans, reloads := newV2ProfilesTestAPI(t)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Second"})

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profiles/2/copy", `{"from_profile":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasCall(humans, "CopyProfile") {
		t.Fatalf("expected CopyProfile call, got %v", humans.Calls)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Profiles_Copy_UnknownHuman(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/profiles/2/copy", `{"from_profile":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- strict input rejection -------------------------------------------------

func TestV2Profiles_StrictUnknownField_422(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profiles",
		`{"name":"X","bogus":true}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Profiles_StrictUnknownQuery_422(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/profiles?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Profiles_Switch_MissingRequired_422(t *testing.T) {
	r, _, _ := newV2ProfilesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/profile", `{}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing profile_no, got %d: %s", w.Code, w.Body.String())
	}
}
