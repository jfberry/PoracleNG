package api

import (
	"encoding/json"
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

// snapshotGameData combines the monster fixtures the pokemon rowtext needs with
// the type / pokestop-event fixtures the invasion + incident translation paths
// need (reusing newV2InvasionGameData's shape).
func snapshotGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Types: map[int]*gamedata.TypeInfo{
			11: {TypeID: 11, Name: "Grass"},
			3:  {TypeID: 3, Name: "Water"},
		},
		Grunts: map[int]*gamedata.Grunt{
			5: {ID: 5, TypeID: 11, Gender: 2, Template: "CHARACTER_GRASS_GRUNT_FEMALE"},
		},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{
				1: {Emoji: "M"},
				2: {Emoji: "F"},
			},
			PokestopEvent: map[int]gamedata.EventInfo{
				9: {Name: "Showcase"},
			},
		},
	}
}

// newV2SnapshotTestAPI wires pokemon + egg + invasion + incident + the snapshot
// endpoint against shared mock stores. It seeds:
//   - human u1 with two profiles (1 active, 2)
//   - one saved location and a default lat/lon
//   - a quest summary schedule
//
// It returns the engine plus the stores so a test can seed rows directly. The
// snapshot endpoint is registered LAST so the provider registry is populated.
func newV2SnapshotTestAPI(t *testing.T) (*gin.Engine, *TrackingDeps, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true,
		Language: "en", CurrentProfileNo: 1, Latitude: 51.5, Longitude: -0.1,
	})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.SeedProfile(store.Profile{UID: 1, ID: "u1", ProfileNo: 1, Name: "default"})
	humans.SeedProfile(store.Profile{UID: 2, ID: "u1", ProfileNo: 2, Name: "second"})
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "home", Latitude: 1.0, Longitude: 2.0}); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	summaries := store.NewMockSummaryScheduleStore()
	summaries.Seed(store.SummarySchedule{ID: "u1", AlertType: "quest", ActiveHours: "[]"})

	monsterStore := store.NewMockTrackingStore[db.MonsterTrackingAPI](
		store.MonsterGetUID, store.MonsterSetUID,
	).WithIDScope(func(m *db.MonsterTrackingAPI) string { return m.ID }).
		WithProfileScope(func(m *db.MonsterTrackingAPI) int { return m.ProfileNo })
	eggStore := store.NewMockTrackingStore[db.EggTrackingAPI](
		store.EggGetUID, store.EggSetUID,
	).WithIDScope(func(e *db.EggTrackingAPI) string { return e.ID })
	invStore := store.NewMockTrackingStore[db.InvasionTrackingAPI](
		store.InvasionGetUID, store.InvasionSetUID,
	).WithIDScope(func(i *db.InvasionTrackingAPI) string { return i.ID })

	deps := &TrackingDeps{
		Humans: humans,
		Tracking: &store.TrackingStores{
			Monsters:  monsterStore,
			Eggs:      eggStore,
			Invasions: invStore,
		},
		Config: &config.Config{},
		RowText: &rowtext.Generator{
			GD:                  snapshotGameData(),
			Translations:        i18n.Load(""),
			DefaultTemplateName: "1",
		},
		Translations: i18n.Load(""),
		Summaries:    summaries,
	}

	orig := v2SendConfirmation
	v2SendConfirmation = func(_ *TrackingDeps, _ *store.HumanLite, _, _ string) {}
	restore := func() { v2SendConfirmation = orig }

	RegisterV2TrackingPokemon(humaAPI, deps)
	RegisterV2TrackingEgg(humaAPI, deps)
	RegisterV2TrackingInvasion(humaAPI, deps)
	RegisterV2TrackingIncident(humaAPI, deps)
	// Snapshot LAST so the provider registry is fully populated.
	RegisterV2TrackingSnapshot(humaAPI, deps)

	return r, deps, restore
}

// trackingMap extracts the "tracking" object from a decoded snapshot body.
func trackingMap(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	tr, ok := body["tracking"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot missing tracking object: %v", body)
	}
	return tr
}

// trackingRules pulls the rule array for one type out of the tracking map.
func trackingRules(t *testing.T, body map[string]any, typeName string) []map[string]any {
	t.Helper()
	raw, ok := trackingMap(t, body)[typeName]
	if !ok {
		t.Fatalf("tracking map missing %q: %v", typeName, body["tracking"])
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("tracking.%s is not an array: %T", typeName, raw)
	}
	out := make([]map[string]any, len(arr))
	for i, e := range arr {
		out[i] = e.(map[string]any)
	}
	return out
}

// seedSnapshotRules POSTs one rule of each seeded type to profile 1.
func seedSnapshotProfile1(t *testing.T, r *gin.Engine) {
	t.Helper()
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":149,"min_iv":95}]`)
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/egg", `[{"level":5}]`)
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/invasion", `[{"type_id":11}]`)
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/incident", `[{"display_type":9}]`)
}

func mustOK(t *testing.T, r *gin.Engine, method, path, body string) *map[string]any {
	t.Helper()
	w := v2DoReq(t, r, method, path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: expected 200, got %d: %s", method, path, w.Code, w.Body.String())
	}
	m := v2DecodeBody(t, w)
	return &m
}

// --- the snapshot shape -----------------------------------------------------

func TestV2Snapshot_TypeKeysAndInvasionIncidentSplit(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	seedSnapshotProfile1(t, r)

	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")

	// Every registered type has a key (empty slice for unseeded ones).
	tr := trackingMap(t, body)
	for _, typ := range []string{"pokemon", "egg", "invasion", "incident"} {
		if _, ok := tr[typ]; !ok {
			t.Fatalf("tracking map missing key %q: %v", typ, tr)
		}
	}

	if got := len(trackingRules(t, body, "pokemon")); got != 1 {
		t.Fatalf("expected 1 pokemon rule, got %d", got)
	}
	if got := len(trackingRules(t, body, "egg")); got != 1 {
		t.Fatalf("expected 1 egg rule, got %d", got)
	}

	// The split: the type_id=11 rule lands under "invasion" only; the
	// display_type=9 (Showcase event) rule lands under "incident" only.
	inv := trackingRules(t, body, "invasion")
	inc := trackingRules(t, body, "incident")
	if len(inv) != 1 || len(inc) != 1 {
		t.Fatalf("expected 1 invasion + 1 incident, got %d + %d", len(inv), len(inc))
	}
	if tid, _ := inv[0]["type_id"].(float64); int(tid) != 11 {
		t.Fatalf("invasion rule should carry type_id=11, got %v", inv[0])
	}
	if _, ok := inv[0]["display_type"]; ok {
		t.Fatalf("invasion rule must not carry display_type: %v", inv[0])
	}
	if dt, _ := inc[0]["display_type"].(float64); int(dt) != 9 {
		t.Fatalf("incident rule should carry display_type=9, got %v", inc[0])
	}
}

func TestV2Snapshot_SubObjectsPresent(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	seedSnapshotProfile1(t, r)

	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")

	human, ok := body["human"].(map[string]any)
	if !ok || human["id"] != "u1" {
		t.Fatalf("human sub-object missing or wrong id: %v", body["human"])
	}

	profiles, ok := body["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %v", body["profiles"])
	}

	locations, ok := body["locations"].(map[string]any)
	if !ok {
		t.Fatalf("locations sub-object missing: %v", body["locations"])
	}
	named, _ := locations["named"].([]any)
	if len(named) != 1 {
		t.Fatalf("expected 1 named location, got %v", locations["named"])
	}
	def, ok := locations["default"].(map[string]any)
	if !ok || def["latitude"].(float64) != 51.5 {
		t.Fatalf("default location missing or wrong: %v", locations["default"])
	}

	summaries, ok := body["summaries"].([]any)
	if !ok || len(summaries) != 1 {
		t.Fatalf("expected 1 summary schedule, got %v", body["summaries"])
	}
	if s := summaries[0].(map[string]any); s["alert_type"] != "quest" {
		t.Fatalf("expected quest summary, got %v", s)
	}
}

// --- include_descriptions ---------------------------------------------------

func TestV2Snapshot_IncludeDescriptions(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	seedSnapshotProfile1(t, r)

	// Without the flag: no description on any rule.
	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")
	for _, typ := range []string{"pokemon", "egg", "invasion", "incident"} {
		for _, rule := range trackingRules(t, body, typ) {
			if _, ok := rule["description"]; ok {
				t.Fatalf("%s rule should NOT carry description without the flag: %v", typ, rule)
			}
		}
	}

	// With the flag: every rule carries a non-empty description.
	body = *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking?include_descriptions=true", "")
	for _, typ := range []string{"pokemon", "egg", "invasion", "incident"} {
		for _, rule := range trackingRules(t, body, typ) {
			if d, ok := rule["description"].(string); !ok || d == "" {
				t.Fatalf("%s rule should carry a non-empty description: %v", typ, rule)
			}
		}
	}
}

// --- all_profiles -----------------------------------------------------------

func TestV2Snapshot_AllProfilesSpansProfilesWithProfileNo(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()

	// Profile 1: one pokemon rule. Profile 2: a different pokemon rule.
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":149}]`)
	mustOK(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?profile=2", `[{"pokemon_id":150}]`)

	// Single-profile (default = active profile 1) sees only profile 1's rule,
	// and that rule carries NO profile_no.
	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking", "")
	single := trackingRules(t, body, "pokemon")
	if len(single) != 1 {
		t.Fatalf("single-profile: expected 1 pokemon rule, got %d", len(single))
	}
	if _, ok := single[0]["profile_no"]; ok {
		t.Fatalf("single-profile rule must NOT carry profile_no: %v", single[0])
	}

	// all_profiles spans BOTH profiles; each rule carries its profile_no.
	body = *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking?all_profiles=true", "")
	all := trackingRules(t, body, "pokemon")
	if len(all) != 2 {
		t.Fatalf("all_profiles: expected 2 pokemon rules, got %d", len(all))
	}
	seen := map[int]int{} // profile_no -> pokemon_id
	for _, rule := range all {
		pn, ok := rule["profile_no"].(float64)
		if !ok {
			t.Fatalf("all_profiles rule must carry profile_no: %v", rule)
		}
		seen[int(pn)] = int(rule["pokemon_id"].(float64))
	}
	if seen[1] != 149 || seen[2] != 150 {
		t.Fatalf("all_profiles profile_no mapping wrong: %v", seen)
	}
}

// --- envelope parity with the per-type list ---------------------------------

func TestV2Snapshot_EnvelopeParityWithPerTypeList(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	seedSnapshotProfile1(t, r)

	for _, typ := range []string{"pokemon", "egg", "invasion", "incident"} {
		// Per-type list (with descriptions, to exercise the full envelope).
		listBody := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/"+typ+"?include_descriptions=true", "")
		listRules := v2RulesArray(t, listBody, "rules")

		// Snapshot rules for the same type (also with descriptions).
		snapBody := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u1/tracking?include_descriptions=true", "")
		snapRules := trackingRules(t, snapBody, typ)

		if len(listRules) != len(snapRules) {
			t.Fatalf("%s: list has %d rules, snapshot has %d", typ, len(listRules), len(snapRules))
		}
		// Compare canonical JSON of each rule (single profile → no profile_no in
		// either, so they must be byte-identical after canonicalisation).
		for i := range listRules {
			a := canonicalJSON(t, listRules[i])
			b := canonicalJSON(t, snapRules[i])
			if a != b {
				t.Fatalf("%s rule %d differs:\n list: %s\n snap: %s", typ, i, a, b)
			}
		}
	}
}

func canonicalJSON(t *testing.T, v map[string]any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// --- strict / ownership -----------------------------------------------------

func TestV2Snapshot_RejectsUnknownQueryParam(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Snapshot_UnknownHuman404(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Snapshot_EmptyHumanHasEmptyTypeArrays(t *testing.T) {
	r, _, restore := newV2SnapshotTestAPI(t)
	defer restore()
	// u2 has no tracking; every type key must still be present as an empty array.
	body := *mustOK(t, r, http.MethodGet, "/api/v2/humans/u2/tracking", "")
	for _, typ := range []string{"pokemon", "egg", "invasion", "incident"} {
		rules := trackingRules(t, body, typ)
		if len(rules) != 0 {
			t.Fatalf("u2 %s should be empty, got %d", typ, len(rules))
		}
	}
}
