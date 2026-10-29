package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2TappableTestAPI wires the strict v2 tappable endpoints against mock
// stores through a real huma+gin engine. Mirrors newV2MaxbattleTestAPI.
func newV2TappableTestAPI(t *testing.T) (*gin.Engine, *store.MockTrackingStore[db.TappableTrackingAPI], *[]pushRecord, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en", CurrentProfileNo: 1})
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true, Language: "en", CurrentProfileNo: 1})

	tpStore := store.NewMockTrackingStore[db.TappableTrackingAPI](
		store.TappableGetUID, store.TappableSetUID,
	).WithIDScope(func(m *db.TappableTrackingAPI) string { return m.ID })

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Items:    map[int]*gamedata.Item{},
		Util:     &gamedata.UtilData{},
	}

	deps := &TrackingDeps{
		Humans:   humans,
		Tracking: &store.TrackingStores{Tappables: tpStore},
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

	RegisterV2TrackingTappable(humaAPI, deps)
	return r, tpStore, pushes, restore
}

const v2TappablePath = "/api/v2/humans/u1/tracking/tappable"

func TestV2Tappable_CreateArray_OK(t *testing.T) {
	r, ms, pushes, restore := newV2TappableTestAPI(t)
	defer restore()

	body := `[{"reward_type":4,"reward":41,"amount":3,"clean":true},{"reward_type":3,"reward":500},{"reward_type":2}]`
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if n := len(v2RulesArray(t, v2DecodeBody(t, w), "created")); n != 3 {
		t.Fatalf("expected 3 created, got %d", n)
	}
	if len(*pushes) != 1 {
		t.Fatalf("expected 1 push, got %d", len(*pushes))
	}
	for _, row := range ms.AllRows() {
		if row.RewardType == 4 && (row.Reward != 41 || row.Amount != 3 || row.Clean != 1) {
			t.Errorf("candy row stored wrong: %+v", row)
		}
	}
}

func TestV2Tappable_DefaultsOnOmission(t *testing.T) {
	r, ms, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":9}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.ID != "u1" || got.RewardType != 9 || got.Reward != 0 || got.Amount != 0 || got.Clean != 0 || got.Distance != 0 ||
		got.Template != "" || got.Ping != "" || got.ProfileNo != 1 || got.OverrideLocationLabel != "" || len(got.OverrideAreas) != 0 {
		t.Fatalf("defaults: got %+v", got)
	}
}

func TestV2Tappable_Rejections(t *testing.T) {
	cases := map[string]string{
		"missing reward_type":   `[{"reward":41}]`,
		"pokemon reward_type":   `[{"reward_type":7}]`,
		"unknown reward_type":   `[{"reward_type":1}]`,
		"amount on stardust":    `[{"reward_type":3,"amount":2}]`,
		"amount on pokecoins":   `[{"reward_type":8,"amount":2}]`,
		"edit true":             `[{"reward_type":4,"edit":true}]`,
		"summary not supported": `[{"reward_type":4,"summary":true}]`,
		"shiny not supported":   `[{"reward_type":4,"shiny":true}]`,
		"form not supported":    `[{"reward_type":4,"form":1}]`,
		"unknown field":         `[{"reward_type":4,"bogus":1}]`,
		"negative reward":       `[{"reward_type":4,"reward":-1}]`,
		"wrong type":            `[{"reward_type":"candy"}]`,
		"empty array":           `[]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r, ms, _, restore := newV2TappableTestAPI(t)
			defer restore()
			w := v2DoReq(t, r, http.MethodPost, v2TappablePath, body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
			}
			if len(ms.AllRows()) != 0 {
				t.Fatalf("rejected rule must not be stored: %+v", ms.AllRows())
			}
		})
	}
}

func TestV2Tappable_EditFalseAccepted(t *testing.T) {
	r, _, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"edit":false}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit:false should be accepted, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Tappable_RejectsUnknownQueryParam(t *testing.T) {
	r, _, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodGet, v2TappablePath+"?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query param, got %d: %s", w.Code, w.Body.String())
	}
}

// reward 0 / amount 0 / defaults read back as null; a specific reward reads
// back as given.
func TestV2Tappable_NullProjection(t *testing.T) {
	r, _, _, restore := newV2TappableTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4},{"reward_type":12,"reward":6,"amount":10}]`)
	w := v2DoReq(t, r, http.MethodGet, v2TappablePath, "")
	rules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	for _, rule := range rules {
		switch rule["reward_type"] {
		case float64(4):
			for _, k := range []string{"reward", "amount", "distance", "template", "ping", "clean", "edit", "override_location_label", "override_areas"} {
				if rule[k] != nil {
					t.Errorf("candy any: %s = %v, want null", k, rule[k])
				}
			}
		case float64(12):
			if rule["reward"] != float64(6) || rule["amount"] != float64(10) {
				t.Errorf("energy: got reward=%v amount=%v", rule["reward"], rule["amount"])
			}
		default:
			t.Errorf("unexpected rule %v", rule)
		}
	}
}

func TestV2Tappable_RoundTrip(t *testing.T) {
	for name, body := range map[string]string{
		"candy any":     `[{"reward_type":4}]`,
		"xl candy":      `[{"reward_type":9,"reward":25,"amount":2,"clean":true,"distance":500}]`,
		"stardust min":  `[{"reward_type":3,"reward":500}]`,
		"specific item": `[{"reward_type":2,"reward":1301,"template":"x"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			r, ms, _, restore := newV2TappableTestAPI(t)
			defer restore()
			roundTripRule(t, r, v2TappablePath, body)
			if n := len(ms.AllRows()); n != 1 {
				t.Fatalf("expected 1 row after round-trip, got %d", n)
			}
		})
	}
}

// reward_type and reward are match keys: an amount or distance change on the
// same reward squashes onto the existing rule (keeping its uid); a different
// reward is a new rule.
func TestV2Tappable_DiffSquashAndDistinct(t *testing.T) {
	r, ms, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"reward":41}]`)
	uid := v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"]

	w = v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"reward":41,"distance":500}]`)
	updated := v2RulesArray(t, v2DecodeBody(t, w), "updated")
	if len(updated) != 1 || updated[0]["uid"] != uid {
		t.Fatalf("distance change should update uid %v, got %s", uid, w.Body.String())
	}

	w = v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"reward":41,"distance":500}]`)
	if len(v2RulesArray(t, v2DecodeBody(t, w), "unchanged")) != 1 {
		t.Fatalf("exact duplicate should be unchanged: %s", w.Body.String())
	}

	v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"reward":25}]`)
	if n := len(ms.AllRows()); n != 2 {
		t.Fatalf("a different reward is a new rule, got %d rows", n)
	}
}

func TestV2Tappable_GetByUIDAndDelete(t *testing.T) {
	r, ms, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4,"reward":41}]`)
	uid := int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64))

	if w := v2DoReq(t, r, http.MethodGet, v2TappablePath+"/"+itoa(uid), ""); w.Code != http.StatusOK {
		t.Fatalf("owner get: expected 200, got %d", w.Code)
	}
	if w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u2/tracking/tappable/"+itoa(uid), ""); w.Code != http.StatusNotFound {
		t.Fatalf("cross-human get: expected 404, got %d", w.Code)
	}
	w = v2DoReq(t, r, http.MethodDelete, v2TappablePath+"/"+itoa(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(ms.AllRows()) != 0 {
		t.Fatalf("rule should be deleted")
	}
}

func TestV2Tappable_PatchRejectsEditAndSummary(t *testing.T) {
	r, _, _, restore := newV2TappableTestAPI(t)
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":4}]`)
	uid := itoa(int64(v2RulesArray(t, v2DecodeBody(t, w), "created")[0]["uid"].(float64)))

	for _, set := range []string{`{"edit":true}`, `{"summary":true}`} {
		w = v2DoReq(t, r, http.MethodPatch, v2TappablePath, `{"uids":[`+uid+`],"set":`+set+`}`)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH set %s: expected 422, got %d: %s", set, w.Code, w.Body.String())
		}
	}
	w = v2DoReq(t, r, http.MethodPatch, v2TappablePath, `{"uids":[`+uid+`],"set":{"clean":true}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"updated"`) {
		t.Fatalf("PATCH clean: got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Tappable_IncludeDescriptions(t *testing.T) {
	r, _, _, restore := newV2TappableTestAPI(t)
	defer restore()
	v2DoReq(t, r, http.MethodPost, v2TappablePath, `[{"reward_type":9,"reward":25,"amount":2}]`)
	w := v2DoReq(t, r, http.MethodGet, v2TappablePath+"?include_descriptions=true", "")
	if !strings.Contains(w.Body.String(), "XL candy") || !strings.Contains(w.Body.String(), "minimum 2") {
		t.Fatalf("description missing: %s", w.Body.String())
	}
}
