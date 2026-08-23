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

// newV2RaidTestAPI wires the strict v2 raid endpoints against mock stores through
// a REAL huma+gin engine. Mirrors newV2GymTestAPI.
func newV2RaidTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.RaidTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	raidStore := store.NewMockTrackingStore[db.RaidTrackingAPI](
		store.RaidGetUID, store.RaidSetUID,
	).WithIDScope(func(rr *db.RaidTrackingAPI) string { return rr.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Raids: raidStore},
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

	RegisterV2TrackingRaid(humaAPI, deps)
	return r, raidStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Raid_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2RaidTestAPI(t)
	defer restore()

	body := `[{"level":5,"clean":true},{"pokemon_id":150,"team":"mystic"}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", body)
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

func TestV2Raid_CreateSingleElementArray_OK(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":3}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := rs.AllRows()
	if len(rows) != 1 || rows[0].Level != 3 {
		t.Fatalf("expected 1 stored row level=3, got %+v", rows)
	}
}

// --- strict rejection: dropped expansions are now unknown fields -----------

func TestV2Raid_RejectsPokemonForm422(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	// v1 accepted pokemon_form expansion; v2 strict ⇒ unknown field ⇒ 422.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"pokemon_form":[{"pokemon_id":150,"form":0}]}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for pokemon_form, got %d: %s", w.Code, w.Body.String())
	}
	if len(rs.AllRows()) != 0 {
		t.Fatalf("pokemon_form rule must not be stored: %+v", rs.AllRows())
	}
}

func TestV2Raid_RejectsLevelArray422(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	// v1 accepted level as [int,...]; v2 strict ⇒ wrong type ⇒ 422.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":[3,4,5]}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for level array, got %d: %s", w.Code, w.Body.String())
	}
	if len(rs.AllRows()) != 0 {
		t.Fatalf("level-array rule must not be stored: %+v", rs.AllRows())
	}
}

func TestV2Raid_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Raid_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"exclusive":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string exclusive, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Raid_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Raid_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- team + rsvp enums ------------------------------------------------------

func TestV2Raid_TeamRoundTrip(t *testing.T) {
	cases := map[string]int{"harmony": 0, "mystic": 1, "valor": 2, "instinct": 3, "any": 4}
	for name, want := range cases {
		r, rs, _, restore := newV2RaidTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"team":"`+name+`"}]`)
		rows := rs.AllRows()
		if len(rows) != 1 || rows[0].Team != want {
			restore()
			t.Fatalf("team %s: expected stored %d, got %+v", name, want, rows)
		}
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
		out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
		// "any" is the wildcard: Part B hides it as null.
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

func TestV2Raid_BadTeam422(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"team":"purple"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad team, got %d: %s", w.Code, w.Body.String())
	}
	if len(rs.AllRows()) != 0 {
		t.Fatalf("invalid team must not be stored: %+v", rs.AllRows())
	}
}

func TestV2Raid_RSVPChangesRoundTrip(t *testing.T) {
	cases := map[string]int{"none": 0, "rsvp": 1, "rsvp_only": 2}
	for name, want := range cases {
		r, rs, _, restore := newV2RaidTestAPI(t)
		v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"rsvp_changes":"`+name+`"}]`)
		rows := rs.AllRows()
		if len(rows) != 1 || rows[0].RSVPChanges != want {
			restore()
			t.Fatalf("rsvp %s: expected stored %d, got %+v", name, want, rows)
		}
		w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
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

func TestV2Raid_BadRSVP422(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"rsvp_changes":"loud"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad rsvp_changes, got %d: %s", w.Code, w.Body.String())
	}
	if len(rs.AllRows()) != 0 {
		t.Fatalf("invalid rsvp_changes must not be stored: %+v", rs.AllRows())
	}
}

// --- defaults + bitmask + gym_id + exclusive -------------------------------

func TestV2Raid_DefaultsOnOmission(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5}]`)
	rows := rs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.PokemonID != 9000 {
		t.Errorf("default pokemon_id: got %d want 9000", row.PokemonID)
	}
	if row.Form != 0 {
		t.Errorf("default form: got %d want 0", row.Form)
	}
	if row.Team != 4 {
		t.Errorf("default team: got %d want 4 (any)", row.Team)
	}
	if bool(row.Exclusive) {
		t.Errorf("default exclusive: got %v want false", row.Exclusive)
	}
	if row.Move != 9000 {
		t.Errorf("default move: got %d want 9000", row.Move)
	}
	if row.Evolution != 9000 {
		t.Errorf("default evolution: got %d want 9000", row.Evolution)
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

// raid pokemon_id is optional (defaults 9000); level is also optional (9000).
func TestV2Raid_LevelOptional(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"pokemon_id":150}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (level optional), got %d: %s", w.Code, w.Body.String())
	}
	rows := rs.AllRows()
	if len(rows) != 1 || rows[0].Level != 9000 || rows[0].PokemonID != 150 {
		t.Fatalf("expected level=9000 pokemon=150, got %+v", rows)
	}
}

// Level sentinel derivation from pokemon_id (see matching/raid.go:65). Stored
// 9000/90 is what the matcher treats as any-boss/any-tier ("everything").
func TestV2Raid_LevelSentinelDerivation(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantPokemon  int
		wantLevel    int
		wantHTTPCode int
	}{
		// Omit both => everything: any boss (9000), any tier (90).
		{"everything", `[{}]`, 9000, 90, http.StatusOK},
		// Specific boss (with a level supplied) ignores level => 9000 placeholder.
		{"specific_boss_level_ignored", `[{"pokemon_id":149,"level":3}]`, 149, 9000, http.StatusOK},
		// Specific boss, no level => 9000 placeholder.
		{"specific_boss_no_level", `[{"pokemon_id":149}]`, 149, 9000, http.StatusOK},
		// By-level tier 5.
		{"by_level_tier", `[{"level":5}]`, 9000, 5, http.StatusOK},
		// By-level explicit level 0 => 422.
		{"by_level_zero_422", `[{"level":0}]`, 0, 0, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, rs, _, restore := newV2RaidTestAPI(t)
			defer restore()
			w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", c.body)
			if w.Code != c.wantHTTPCode {
				t.Fatalf("expected %d, got %d: %s", c.wantHTTPCode, w.Code, w.Body.String())
			}
			if c.wantHTTPCode != http.StatusOK {
				if len(rs.AllRows()) != 0 {
					t.Fatalf("invalid rule must not be stored: %+v", rs.AllRows())
				}
				return
			}
			rows := rs.AllRows()
			if len(rows) != 1 || rows[0].PokemonID != c.wantPokemon || rows[0].Level != c.wantLevel {
				t.Fatalf("expected pokemon=%d level=%d, got %+v", c.wantPokemon, c.wantLevel, rows)
			}
		})
	}
}

// Response projection: stored 9000/90 (everything) => pokemon_id null + level
// null; 9000/5 (any boss tier 5) => pokemon_id null + level 5; 149/9000
// (specific boss) => pokemon_id 149 + level null.
func TestV2Raid_LevelNullProjection(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantPokemon any
		wantLevel   any
	}{
		{"everything", `[{}]`, nil, nil},
		{"by_level_tier", `[{"level":5}]`, nil, float64(5)},
		{"specific_boss", `[{"pokemon_id":149}]`, float64(149), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _, _, restore := newV2RaidTestAPI(t)
			defer restore()
			v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", c.body)
			w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
			out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
			if out[0]["pokemon_id"] != c.wantPokemon {
				t.Fatalf("pokemon_id: got %v want %v", out[0]["pokemon_id"], c.wantPokemon)
			}
			if out[0]["level"] != c.wantLevel {
				t.Fatalf("level: got %v want %v", out[0]["level"], c.wantLevel)
			}
		})
	}
}

// Round-trip (GET->PUT->GET identical) for the three derivation cases.
func TestV2Raid_LevelRoundTrip(t *testing.T) {
	cases := map[string]string{
		"everything":    `[{}]`,
		"specific_boss": `[{"pokemon_id":149}]`,
		"by_level_tier": `[{"level":5}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r, rs, _, restore := newV2RaidTestAPI(t)
			defer restore()
			roundTripRule(t, r, "/api/v2/humans/u1/tracking/raid", body)
			rows := rs.AllRows()
			if len(rows) != 1 {
				t.Fatalf("expected 1 row after round-trip, got %d", len(rows))
			}
		})
	}
}

func TestV2Raid_GymIDRoundTrip(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"gym_id":"abc"}]`)
	rows := rs.AllRows()
	if len(rows) != 1 || !rows[0].GymID.Valid || rows[0].GymID.String != "abc" {
		t.Fatalf("expected stored gym_id abc, got %+v", rows)
	}
	// Empty string normalises to null (any). Distinct team so it inserts a new row.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"team":"valor","gym_id":""}]`)
	for _, row := range rs.AllRows() {
		if row.Team == 2 && row.GymID.Valid {
			t.Fatalf("empty gym_id must normalise to null, got %q", row.GymID.String)
		}
	}
}

func TestV2Raid_ExclusiveRoundTrip(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"exclusive":true}]`)
	rows := rs.AllRows()
	if len(rows) != 1 || !bool(rows[0].Exclusive) {
		t.Fatalf("expected exclusive stored true, got %+v", rows)
	}
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
	out := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if out[0]["exclusive"] != true {
		t.Fatalf("expected exclusive true on read-back, got %v", out[0]["exclusive"])
	}
}

func TestV2Raid_CleanEditSummaryBitmask(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5,"clean":true,"edit":true,"summary":true}]`)
	rows := rs.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

// --- diff behaviour (team is match key) ------------------------------------

func TestV2Raid_TeamIsMatchKey(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"mystic","distance":100}]`)
	// Same team, different distance ⇒ update (not a second row).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"mystic","distance":200}]`)
	resp := v2DecodeBody(t, w)
	if len(v2RulesArray(t, resp, "updated")) != 1 {
		t.Fatalf("same team different distance should be an update, got %v", resp)
	}
	if len(rs.AllRows()) != 1 || rs.AllRows()[0].Distance != 200 {
		t.Fatalf("expected single row distance 200, got %+v", rs.AllRows())
	}
	// Different team ⇒ new insert.
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"valor"}]`)
	if len(rs.AllRows()) != 2 {
		t.Fatalf("distinct team should add a row, got %d", len(rs.AllRows()))
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Raid_ListShape(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"mystic"},{"team":"valor"}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Raid_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/raid/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Raid_DeleteSingle(t *testing.T) {
	r, rs, pushes, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/raid/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(rs.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Raid_BulkDelete(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"harmony"},{"team":"mystic"},{"team":"valor"}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/raid?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(rs.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(rs.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Raid_PutFullReplace_NewUID(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"mystic","exclusive":true}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted exclusive resets to default false.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/raid/"+itoa(oldUID), `{"team":"mystic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := rs.AllRows()
	if len(rows) != 1 || bool(rows[0].Exclusive) {
		t.Fatalf("replace did not reset exclusive: %+v", rows)
	}
}

func TestV2Raid_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"team":"any"}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/raid/"+itoa(uid), `{"team":"any"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Raid_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", `[{"level":5}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Raid_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid?include_descriptions=true", `[{"level":5}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Raid_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid?silent=true", `[{"level":5}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Raid_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2RaidTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/raid", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}

// --- costume -----------------------------------------------------------

// TestV2Raid_CostumeRoundTrip mirrors the v2 pokemon costume round-trip:
// omitted -> stored 9000 (any) -> read back null; 0 -> stored 0 (no costume)
// -> read back literal 0 (not null); a specific costume -> stored/round-trips
// unchanged.
func TestV2Raid_CostumeRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantStored  int
		wantReadout any
	}{
		{"omitted -> any (9000)", `[{"level":5}]`, 9000, nil},
		{"zero -> no costume (0)", `[{"level":5,"costume":0}]`, 0, float64(0)},
		{"specific costume", `[{"level":5,"costume":7}]`, 7, float64(7)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, rs, _, restore := newV2RaidTestAPI(t)
			defer restore()

			w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/raid", c.body)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			rows := rs.AllRows()
			if len(rows) != 1 || rows[0].Costume != c.wantStored {
				t.Fatalf("expected stored costume %d, got %+v", c.wantStored, rows)
			}

			out := v2RulesArray(t, v2DecodeBody(t, v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/raid", "")), "rules")
			if out[0]["costume"] != c.wantReadout {
				t.Fatalf("costume read-back mismatch: got %v want %v", out[0]["costume"], c.wantReadout)
			}
		})
	}
}

// TestV2Raid_RoundTrip is the Part B fixed-point check for raid: a rule with a
// specific boss + a set move + default team/rsvp/etc GETs back with the defaults
// as null and survives a PUT of that body unchanged.
func TestV2Raid_RoundTrip(t *testing.T) {
	r, rs, _, restore := newV2RaidTestAPI(t)
	defer restore()

	roundTripRule(t, r, "/api/v2/humans/u1/tracking/raid",
		`[{"pokemon_id":150,"distance":750,"clean":true}]`)

	rows := rs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row after round-trip, got %d", len(rows))
	}
	row := rows[0]
	if row.PokemonID != 150 || row.Distance != 750 || !db.IsClean(row.Clean) {
		t.Fatalf("set fields not preserved across round-trip: %+v", row)
	}
	// With a specific pokemon_id, level is forced to the 9000 sentinel; move and
	// team stay at their wildcards.
	if row.Level != 9000 || row.Move != 9000 || row.Team != 4 {
		t.Fatalf("default/forced fields drifted across round-trip: %+v", row)
	}
}
