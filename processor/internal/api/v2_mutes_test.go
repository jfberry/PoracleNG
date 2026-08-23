package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2MutesTestAPI wires the strict v2 mutes endpoints against a mock human
// store, a fresh in-memory mute store, and an AreaLogic with one fence
// ("london") so area-name validation is exercised.
func newV2MutesTestAPI(t *testing.T) (*gin.Engine, *mute.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true,
		Language: "en", CurrentProfileNo: 1,
	})

	cfg := &config.Config{}
	fences := []geofence.Fence{{Name: "london", UserSelectable: true}}
	mutes := mute.NewStore()
	deps := &TrackingDeps{
		Humans:    humans,
		Config:    cfg,
		AreaLogic: bot.NewAreaLogic(fences, cfg),
		Mutes:     mutes,
	}

	RegisterV2Mutes(humaAPI, deps)
	return r, mutes
}

// --- create -------------------------------------------------------------------

func TestV2Mutes_Create_Fresh(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes",
		`{"scope":"gym","value":"gym123","duration_min":120}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	if body["replaced"] != false {
		t.Fatalf("expected replaced=false on fresh mute, got %v", body)
	}
	item, ok := body["mute"].(map[string]any)
	if !ok {
		t.Fatalf("missing mute item: %v", body)
	}
	if item["scope"] != "gym" || item["value"] != "gym123" {
		t.Fatalf("unexpected item: %v", item)
	}
	now := time.Now().Unix()
	exp, _ := item["expires_at"].(float64)
	if int64(exp) < now+119*60 || int64(exp) > now+121*60 {
		t.Fatalf("expires_at not ~120min out: %v (now %d)", item["expires_at"], now)
	}
	if rem, _ := item["remaining_secs"].(float64); rem <= 0 || rem > 120*60 {
		t.Fatalf("remaining_secs out of range: %v", item["remaining_secs"])
	}
	// The store actually filters alerts for it.
	if !mutes.Match("u1", mute.Event{GymID: "gym123"}, now) {
		t.Fatalf("store does not match created mute")
	}
}

func TestV2Mutes_Create_DefaultDuration(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"pokemon","value":"25"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	item := v2DecodeBody(t, w)["mute"].(map[string]any)
	now := time.Now().Unix()
	exp := int64(item["expires_at"].(float64))
	if exp < now+59*60 || exp > now+61*60 {
		t.Fatalf("default duration not ~60min: expires_at %d (now %d)", exp, now)
	}
}

func TestV2Mutes_Create_Replace(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	for range 2 {
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"gym","value":"g1"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	// Second create of the same (scope,value) reports replaced and does not duplicate.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"gym","value":"g1"}`)
	if body := v2DecodeBody(t, w); body["replaced"] != true {
		t.Fatalf("expected replaced=true, got %v", body)
	}
	lw := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/mutes", "")
	if items := v2DecodeBody(t, lw)["mutes"].([]any); len(items) != 1 {
		t.Fatalf("expected 1 entry after re-mute, got %d", len(items))
	}
}

func TestV2Mutes_Create_Everything_NoValue(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"everything"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	item := v2DecodeBody(t, w)["mute"].(map[string]any)
	if v, present := item["value"]; !present || v != nil {
		t.Fatalf("expected value present-but-null for everything, got %v", item)
	}
	if !mutes.Match("u1", mute.Event{GymID: "anything"}, time.Now().Unix()) {
		t.Fatalf("everything mute does not match")
	}
}

func TestV2Mutes_Create_Area_CanonicalisesName(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"area","value":"LONDON"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !mutes.Match("u1", mute.Event{Area: []string{"london"}}, time.Now().Unix()) {
		t.Fatalf("area mute does not match canonical name")
	}
}

func TestV2Mutes_Create_Validation(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	cases := []struct {
		name string
		body string
	}{
		{"bad scope", `{"scope":"weather","value":"x"}`},
		{"missing value", `{"scope":"gym"}`},
		{"everything with value", `{"scope":"everything","value":"x"}`},
		{"pokemon non-numeric", `{"scope":"pokemon","value":"pikachu"}`},
		{"tracking non-numeric", `{"scope":"tracking","value":"abc"}`},
		{"unknown area", `{"scope":"area","value":"atlantis"}`},
		{"duration too small", `{"scope":"gym","value":"g1","duration_min":0}`},
		{"duration too large", `{"scope":"gym","value":"g1","duration_min":10081}`},
		{"unknown field", `{"scope":"gym","value":"g1","bogus":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", tc.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestV2Mutes_Create_UnknownHuman404(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/ghost/mutes", `{"scope":"gym","value":"g1"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- list ---------------------------------------------------------------------

func TestV2Mutes_List_FiltersExpired(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	now := time.Now().Unix()
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "live", ExpiresAt: now + 600})
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "dead", ExpiresAt: now - 600})

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/mutes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	items := v2DecodeBody(t, w)["mutes"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 active mute, got %d: %v", len(items), items)
	}
	if items[0].(map[string]any)["value"] != "live" {
		t.Fatalf("expected the live entry, got %v", items[0])
	}
}

func TestV2Mutes_List_EmptyArray(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/mutes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if items, ok := v2DecodeBody(t, w)["mutes"].([]any); !ok || len(items) != 0 {
		t.Fatalf("expected empty mutes array, got %s", w.Body.String())
	}
}

func TestV2Mutes_List_UnknownHuman404(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/ghost/mutes", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- delete -------------------------------------------------------------------

func TestV2Mutes_DeleteOne(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	now := time.Now().Unix()
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "g1", ExpiresAt: now + 600})
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopePokemon, ScopeValue: "25", ExpiresAt: now + 600})

	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes?scope=gym&value=g1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	deleted := v2DecodeBody(t, w)["deleted"].([]any)
	if len(deleted) != 1 || deleted[0].(map[string]any)["value"] != "g1" {
		t.Fatalf("expected deleted gym g1, got %v", deleted)
	}
	if mutes.Match("u1", mute.Event{GymID: "g1"}, now) {
		t.Fatalf("gym mute survived delete")
	}
	if !mutes.Match("u1", mute.Event{PokemonID: 25}, now) {
		t.Fatalf("pokemon mute should be untouched")
	}
}

func TestV2Mutes_DeleteEverythingScope_NoValue(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeEverything, ExpiresAt: time.Now().Unix() + 600})
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes?scope=everything", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Mutes_DeleteAll(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	now := time.Now().Unix()
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "g1", ExpiresAt: now + 600})
	mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopePokemon, ScopeValue: "25", ExpiresAt: now + 600})

	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if deleted := v2DecodeBody(t, w)["deleted"].([]any); len(deleted) != 2 {
		t.Fatalf("expected 2 deleted, got %v", deleted)
	}
	if len(mutes.List("u1")) != 0 {
		t.Fatalf("entries survived delete-all")
	}
}

func TestV2Mutes_DeleteAll_EmptyOK(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty delete-all, got %d: %s", w.Code, w.Body.String())
	}
	if deleted := v2DecodeBody(t, w)["deleted"].([]any); len(deleted) != 0 {
		t.Fatalf("expected empty deleted array, got %v", deleted)
	}
}

func TestV2Mutes_Delete_Validation(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	for _, q := range []string{
		"?scope=gym",                // value required for gym
		"?value=g1",                 // value without scope
		"?scope=weather&value=x",    // unknown scope
		"?scope=everything&value=x", // everything takes no value
	} {
		t.Run(q, func(t *testing.T) {
			w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes"+q, "")
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422 for %q, got %d: %s", q, w.Code, w.Body.String())
			}
		})
	}
}

func TestV2Mutes_Delete_Miss404(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/mutes?scope=gym&value=nothere", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing mute, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Mutes_RejectUnknownQuery(t *testing.T) {
	r, _ := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/mutes?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

// --- snapshot inclusion ---------------------------------------------------------

func TestV2Snapshot_IncludesMutes(t *testing.T) {
	r, deps, restore := newV2SnapshotTestAPI(t)
	defer restore()
	deps.Mutes = mute.NewStore()
	now := time.Now().Unix()
	deps.Mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "g1", ExpiresAt: now + 600})
	deps.Mutes.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeGym, ScopeValue: "dead", ExpiresAt: now - 600}) // expired

	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")
	items, ok := body["mutes"].([]any)
	if !ok {
		t.Fatalf("snapshot missing mutes array: %v", body["mutes"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 active mute in snapshot, got %d: %v", len(items), items)
	}
	if items[0].(map[string]any)["value"] != "g1" {
		t.Fatalf("unexpected snapshot mute: %v", items[0])
	}
}

func TestV2Snapshot_MutesEmptyArray(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")
	if items, ok := body["mutes"].([]any); !ok || len(items) != 0 {
		t.Fatalf("expected empty mutes array in snapshot, got %v", body["mutes"])
	}
}

// --- area validation via StateMgr fallback --------------------------------------

// Production wiring has no AreaLogic on TrackingDeps; area names must still be
// validated (and canonicalised) against the live state's fences.
func TestV2Mutes_Create_Area_StateMgrFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, CurrentProfileNo: 1})

	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{{Name: "london", UserSelectable: true}}})

	mutes := mute.NewStore()
	deps := &TrackingDeps{Humans: humans, Config: &config.Config{}, StateMgr: mgr, Mutes: mutes}
	RegisterV2Mutes(humaAPI, deps)

	// Unknown area rejected.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"area","value":"atlantis"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown area, got %d: %s", w.Code, w.Body.String())
	}
	// Known area accepted, canonicalised case-insensitively.
	w = v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"area","value":"LONDON"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for known area, got %d: %s", w.Code, w.Body.String())
	}
	if !mutes.Match("u1", mute.Event{Area: []string{"london"}}, time.Now().Unix()) {
		t.Fatalf("area mute does not match after StateMgr validation")
	}
}

// Numeric mute values must be canonicalised before storage: "025" passes
// Atoi validation but the matcher compares ScopeValue against the
// canonical decimal string ("25"), so a verbatim-stored "025" creates a
// mute that never suppresses anything.
func TestV2Mutes_Create_CanonicalisesNumericValue(t *testing.T) {
	r, mutes := newV2MutesTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes",
		`{"scope":"pokemon","value":"025"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	item := v2DecodeBody(t, w)["mute"].(map[string]any)
	if item["value"] != "25" {
		t.Fatalf("expected canonical value \"25\", got %v", item["value"])
	}
	if !mutes.Match("u1", mute.Event{PokemonID: 25}, time.Now().Unix()) {
		t.Fatalf("store does not match pokemon 25 — value stored verbatim instead of canonicalised")
	}
}
