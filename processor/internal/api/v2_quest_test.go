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

// newV2QuestTestAPI wires the strict v2 quest endpoints against mock stores
// through a REAL huma+gin engine. Mirrors newV2LureTestAPI.
func newV2QuestTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.QuestTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	questStore := store.NewMockTrackingStore[db.QuestTrackingAPI](
		store.QuestGetUID, store.QuestSetUID,
	).WithIDScope(func(q *db.QuestTrackingAPI) string { return q.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Quests: questStore},
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

	RegisterV2TrackingQuest(humaAPI, deps)
	return r, questStore, pushes, restore
}

// --- create (single + array) ----------------------------------------------

func TestV2Quest_CreateArray_OK(t *testing.T) {
	r, _, pushes, restore := newV2QuestTestAPI(t)
	defer restore()

	body := `[{"reward_type":3,"reward":1000},{"reward_type":7,"reward":25}]`
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", body)
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

func TestV2Quest_CreateSingleElementArray_OK(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":2,"reward":701}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := qs.AllRows()
	if len(rows) != 1 || rows[0].RewardType != 2 || rows[0].Reward != 701 {
		t.Fatalf("expected 1 stored row reward_type=2 reward=701, got %+v", rows)
	}
}

// TestV2Quest_PokecoinsRoundTrip verifies pokecoins (reward_type 8) stores its
// minimum amount in the Reward column (mirroring stardust) and round-trips.
func TestV2Quest_PokecoinsRoundTrip(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":8,"reward":10}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := qs.AllRows()
	if len(rows) != 1 || rows[0].RewardType != 8 || rows[0].Reward != 10 {
		t.Fatalf("expected 1 stored row reward_type=8 reward=10 (min pokecoin amount), got %+v", rows)
	}
}

// --- reward_type required + discrete-set validation -------------------------

func TestV2Quest_RejectsMissingRewardType(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward":1000}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing reward_type, got %d: %s", w.Code, w.Body.String())
	}
	if len(qs.AllRows()) != 0 {
		t.Fatalf("missing reward_type must not be stored: %+v", qs.AllRows())
	}
}

func TestV2Quest_RejectsBadRewardType(t *testing.T) {
	for _, bad := range []int{5, 99, 0, 1} {
		r, qs, _, restore := newV2QuestTestAPI(t)
		body := `[{"reward_type":` + itoa(int64(bad)) + `}]`
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", body)
		if w.Code != http.StatusUnprocessableEntity {
			restore()
			t.Fatalf("reward_type=%d: expected 422, got %d: %s", bad, w.Code, w.Body.String())
		}
		if len(qs.AllRows()) != 0 {
			restore()
			t.Fatalf("reward_type=%d: bad value must not be stored: %+v", bad, qs.AllRows())
		}
		restore()
	}
}

func TestV2Quest_AcceptsAllValidRewardTypes(t *testing.T) {
	for _, rt := range []int{2, 3, 4, 7, 8, 12} {
		r, qs, _, restore := newV2QuestTestAPI(t)
		body := `[{"reward_type":` + itoa(int64(rt)) + `}]`
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", body)
		if w.Code != http.StatusOK {
			restore()
			t.Fatalf("reward_type=%d: expected 200, got %d: %s", rt, w.Code, w.Body.String())
		}
		rows := qs.AllRows()
		if len(rows) != 1 || rows[0].RewardType != rt {
			restore()
			t.Fatalf("reward_type=%d: not stored correctly: %+v", rt, rows)
		}
		restore()
	}
}

// --- strict rejection -------------------------------------------------------

func TestV2Quest_RejectsUnknownBodyField(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3,"bogus_field":1}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown body field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Quest_RejectsWrongType(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":"x"}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string reward_type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Quest_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Quest_RejectsEmptyArray(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for empty array, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaults + round-trip --------------------------------------------------

func TestV2Quest_DefaultsOnOmission(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3}]`)
	rows := qs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Reward != 0 || row.Form != 0 || row.Amount != 0 || bool(row.Shiny) {
		t.Fatalf("unexpected reward/form/amount/shiny defaults: %+v", row)
	}
	if row.Clean != 0 || row.Distance != 0 || row.Template != "" {
		t.Fatalf("unexpected common defaults: %+v", row)
	}
	if row.Ping != "" {
		t.Errorf("ping should be server-managed empty, got %q", row.Ping)
	}
}

func TestV2Quest_RewardFormAmountRoundTrip(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	body := `[{"reward_type":7,"reward":201,"form":65,"amount":3}]`
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", body)
	rows := qs.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.RewardType != 7 || row.Reward != 201 || row.Form != 65 || row.Amount != 3 {
		t.Fatalf("round-trip mismatch: %+v", row)
	}
}

// TestV2Quest_RewardWildcard pins the "all items / all pokemon" contract (the
// behaviour PR #152 fixed in the matcher): reward=0 — whether sent explicitly or
// reached by omitting the field — is the wildcard for that reward_type, and the
// GET projection reports it back as null (never 0). Without this, a regression
// that added a `minimum:1` constraint or stopped defaulting reward to 0 would
// silently break "all items"/"all pokemon" tracking via the v2 API.
func TestV2Quest_RewardWildcard(t *testing.T) {
	// Explicit reward:0 and omitted reward must both store 0 (= any) and
	// project back as null. reward_type 2 = all items, 7 = all pokemon.
	cases := []struct {
		name string
		body string
	}{
		{"all_items_explicit_zero", `[{"reward_type":2,"reward":0}]`},
		{"all_items_omitted", `[{"reward_type":2}]`},
		{"all_pokemon_explicit_zero", `[{"reward_type":7,"reward":0}]`},
		{"all_pokemon_omitted", `[{"reward_type":7}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, qs, _, restore := newV2QuestTestAPI(t)
			defer restore()

			w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", tc.body)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			rows := qs.AllRows()
			if len(rows) != 1 || rows[0].Reward != 0 {
				t.Fatalf("expected 1 stored row with reward=0 (wildcard), got %+v", rows)
			}

			// Response projects the wildcard as null, not 0.
			created := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
			if rv, present := created["reward"]; present && rv != nil {
				t.Fatalf("expected reward=null (wildcard) in response, got %v", rv)
			}

			// GET round-trip projects it as null too.
			w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest", "")
			rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
			if rv, present := rule["reward"]; present && rv != nil {
				t.Fatalf("expected reward=null (wildcard) on GET, got %v", rv)
			}
		})
	}
}

func TestV2Quest_ShinyRoundTrip(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":7,"reward":25,"shiny":true}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := qs.AllRows()
	if len(rows) != 1 || !bool(rows[0].Shiny) {
		t.Fatalf("expected stored shiny=true, got %+v", rows)
	}

	// Response should echo shiny=true.
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if s, ok := rule["shiny"].(bool); !ok || !s {
		t.Fatalf("expected shiny=true in response, got %v", rule["shiny"])
	}
}

// --- clean/edit/summary bitmask --------------------------------------------

func TestV2Quest_CleanEditSummaryBitmask(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3,"clean":true,"edit":true,"summary":true}]`)
	rows := qs.AllRows()
	if len(rows) != 1 || rows[0].Clean != 7 {
		t.Fatalf("expected clean bitmask 7, got %+v", rows)
	}
}

func TestV2Quest_SummaryBitOnly(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()

	// The summary bit (bit 4) is what routes a quest into the summary digest.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3,"summary":true}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := qs.AllRows()
	if len(rows) != 1 || rows[0].Clean != 4 {
		t.Fatalf("expected clean bitmask 4 (summary only), got %+v", rows)
	}
	if !db.IsSummary(rows[0].Clean) || db.IsClean(rows[0].Clean) || db.IsEdit(rows[0].Clean) {
		t.Fatalf("summary bit semantics wrong: %+v", rows[0])
	}

	// Round-trip: response should report summary=true, clean/edit=false.
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if s, _ := rule["summary"].(bool); !s {
		t.Fatalf("expected summary=true in response, got %v", rule["summary"])
	}
	if c, _ := rule["clean"].(bool); c {
		t.Fatalf("expected clean=false in response, got %v", rule["clean"])
	}
}

// --- list + get-by-uid ------------------------------------------------------

func TestV2Quest_ListShape(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":2,"reward":701},{"reward_type":3}]`)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "rules")) != 2 {
		t.Fatalf("expected 2 rules")
	}
}

func TestV2Quest_GetByUID_OwnedAndCrossHuman(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/quest/"+itoa(uid), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
}

// --- delete + bulk ----------------------------------------------------------

func TestV2Quest_DeleteSingle(t *testing.T) {
	r, qs, pushes, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))
	*pushes = (*pushes)[:0]

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/quest/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 1 {
		t.Fatalf("expected 1 deleted")
	}
	if len(qs.AllRows()) != 0 {
		t.Fatalf("row not deleted")
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected removal push, got %d", len(*pushes))
	}
}

func TestV2Quest_BulkDelete(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":2,"reward":701},{"reward_type":2,"reward":702},{"reward_type":3}]`)
	created := v2RulesArray(t, v2DecodeBody(t, w), "created")
	u0 := int64(created[0]["uid"].(float64))
	u1 := int64(created[1]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/tracking/quest?uid="+itoa(u0)+","+itoa(u1), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(v2RulesArray(t, v2DecodeBody(t, w), "deleted")) != 2 {
		t.Fatalf("expected 2 deleted")
	}
	if len(qs.AllRows()) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(qs.AllRows()))
	}
}

// --- PUT full-replace -------------------------------------------------------

func TestV2Quest_PutFullReplace_NewUID(t *testing.T) {
	r, qs, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3,"reward":1000,"distance":500}]`)
	oldUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	// PUT replaces; omitted reward/distance reset to defaults.
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/quest/"+itoa(oldUID), `{"reward_type":7,"reward":25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newUID := int64(v2RulesArray(t, v2DecodeBody(t, w), "updated")[0]["uid"].(float64))
	if newUID == oldUID {
		t.Fatalf("PUT must yield a NEW uid")
	}
	rows := qs.AllRows()
	if len(rows) != 1 || rows[0].RewardType != 7 || rows[0].Reward != 25 || rows[0].Distance != 0 {
		t.Fatalf("replace did not apply correctly: %+v", rows)
	}
}

func TestV2Quest_PutCrossHuman404(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u2/tracking/quest/"+itoa(uid), `{"reward_type":3}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 replacing another human's uid, got %d", w.Code)
	}
}

// --- include_descriptions + silent -----------------------------------------

func TestV2Quest_IncludeDescriptionsOnRead(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3}]`)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest", "")
	if _, ok := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]["description"]; ok {
		t.Fatalf("description should be absent without include_descriptions")
	}
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/quest?include_descriptions=true", "")
	rule := v2RulesArray(t, v2DecodeBody(t, w), "rules")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected non-empty description, got %v", rule["description"])
	}
}

func TestV2Quest_IncludeDescriptionsOnCreate(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest?include_descriptions=true", `[{"reward_type":3}]`)
	rule := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]
	if d, ok := rule["description"].(string); !ok || d == "" {
		t.Fatalf("expected description on created rule, got %v", rule["description"])
	}
	if _, ok := v2DecodeBody(t, w)["message"]; ok {
		t.Fatalf("response body must not contain an assembled 'message' field")
	}
}

func TestV2Quest_SilentSuppressesPush(t *testing.T) {
	r, _, pushes, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest?silent=true", `[{"reward_type":3}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(*pushes) != 0 {
		t.Fatalf("silent=true must suppress the push, got %d", len(*pushes))
	}
}

func TestV2Quest_UnknownHuman404(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/tracking/quest", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown human, got %d", w.Code)
	}
}
