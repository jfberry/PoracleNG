package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func costumeTestGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Costumes: map[int]gamedata.CostumeInfo{
			12: {ID: 12, Name: "Party Hat", Proto: "PARTY_HAT"},
			13: {ID: 13, Name: "Winter 2020", Proto: "WINTER_2020"},
		},
	}
}

func TestMasterdataCostumes_UsesLocaleTranslation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"costume_12": "Partyhut"}))
	RegisterMasterdataCostumes(humaAPI, costumeTestGameData(), bundle)

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/costumes?locale=de", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/costumes?locale=de = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	entry, ok := got["12"].(map[string]any)
	if !ok {
		t.Fatalf("body[12] = %v, want object; full: %v", got["12"], got)
	}
	if entry["name"] != "Partyhut" {
		t.Errorf("body[12].name = %v, want %q", entry["name"], "Partyhut")
	}
	if entry["id"] != float64(12) {
		t.Errorf("body[12].id = %v, want 12", entry["id"])
	}
}

func TestMasterdataCostumes_FallsBackToMasterfileNameWhenUntranslated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"costume_12": "Partyhut"}))
	RegisterMasterdataCostumes(humaAPI, costumeTestGameData(), bundle)

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/costumes?locale=de", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := decodeBody(t, w)
	entry, ok := got["13"].(map[string]any)
	if !ok {
		t.Fatalf("body[13] = %v, want object; full: %v", got["13"], got)
	}
	// costume_13 has no German translation: fall back to the raw masterfile
	// name rather than leaking the bare "costume_13" key.
	if entry["name"] != "Winter 2020" {
		t.Errorf("body[13].name = %v, want masterfile fallback %q", entry["name"], "Winter 2020")
	}
}

func TestMasterdataCostumes_NilGameDataReturnsEmptyObject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterMasterdataCostumes(humaAPI, nil, i18n.NewBundle())

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/costumes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/costumes (nil gd) = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var obj map[string]any
	if err := json.NewDecoder(w.Body).Decode(&obj); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if len(obj) != 0 {
		t.Errorf("body = %v, want empty object", obj)
	}
}

// --- #209 follow-up: grunt display names ------------------------------------

func gruntsTestGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Types: map[int]*gamedata.TypeInfo{11: {TypeID: 11, Name: "Grass"}},
		Grunts: map[int]*gamedata.Grunt{
			// Typed grunt: type name is localisable via poke_type_{id}.
			5: {ID: 5, TypeID: 11, CategoryID: 2, Gender: 2, Template: "CHARACTER_GRASS_GRUNT_FEMALE"},
			// Leader: no type, category carries the name.
			44: {ID: 44, CategoryID: 6, Template: "CHARACTER_GIOVANNI"},
			// Training leaders share category 1, so the TYPE field is what
			// keeps them distinct.
			1: {ID: 1, CategoryID: 1, Template: "CHARACTER_BLANCHE"},
			2: {ID: 2, CategoryID: 1, Template: "CHARACTER_CANDELA"},
		},
	}
}

func getGrunts(t *testing.T, locale string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"poke_type_11":         "Pflanze",
		"character_category_2": "Ruepel",
		"character_category_6": "Giovanni",
		"character_category_1": "Teamleiter",
		// The real display names: gamelocale ships grunt_<id> (full) and
		// grunt_a_<id> (abbreviated) for 415 grunts across 16 locales.
		"grunt_5":   "Pflanze - Ruepel (Weiblich)",
		"grunt_a_5": "Pflanze \u2640",
		"grunt_44":  "Giovanni",
		"grunt_1":   "Blanche",
		// grunt 2 deliberately has no key, to exercise the fallback.
	}))
	RegisterMasterdataGrunts(humaAPI, gruntsTestGameData(), bundle)

	q := ""
	if locale != "" {
		q = "?locale=" + locale
	}
	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/grunts"+q, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/grunts%s = %d: %s", q, w.Code, w.Body.String())
	}
	return decodeBody(t, w)
}

func gruntEntry(t *testing.T, body map[string]any, id string) map[string]any {
	t.Helper()
	e, ok := body[id].(map[string]any)
	if !ok {
		t.Fatalf("grunt %s missing from response: %v", id, body)
	}
	return e
}

// The endpoint was English-only and built once at startup, the same gap as
// costumes. It matters more since #209 made grunt_type the field every
// invasion read emits: the string is the identity, so a client needs
// somewhere to get its display name.
func TestMasterdataGrunts_LocalisesTypeAndCategory(t *testing.T) {
	body := getGrunts(t, "de")

	e := gruntEntry(t, body, "5")
	if e["type"] != "Pflanze" {
		t.Errorf("grunt 5 type = %v, want localised %q", e["type"], "Pflanze")
	}
	if e["grunt"] != "Ruepel" {
		t.Errorf("grunt 5 grunt = %v, want localised %q", e["grunt"], "Ruepel")
	}
}

// A grunt with no pokemon type keeps a distinct, usable type string from the
// template rather than going blank.
func TestMasterdataGrunts_UntypedGruntFallsBackToTemplateName(t *testing.T) {
	body := getGrunts(t, "de")

	if e := gruntEntry(t, body, "44"); e["type"] != "Giovanni" {
		t.Errorf("grunt 44 type = %v, want template-derived %q", e["type"], "Giovanni")
	}
}

// Blanche and Candela share category 1 ("Team Leader"), so the category alone
// cannot tell them apart — the type field is what does.
func TestMasterdataGrunts_SharedCategoryStillDistinguishable(t *testing.T) {
	body := getGrunts(t, "de")

	b, c := gruntEntry(t, body, "1"), gruntEntry(t, body, "2")
	if b["grunt"] != "Teamleiter" || c["grunt"] != "Teamleiter" {
		t.Fatalf("expected both to share the localised category, got %v / %v", b["grunt"], c["grunt"])
	}
	if b["type"] == c["type"] {
		t.Errorf("blanche and candela must stay distinguishable, both type = %v", b["type"])
	}
}

// grunt_type is the string an invasion rule stores and every v2 read emits;
// without it a client cannot map a rule back to a display name.
func TestMasterdataGrunts_ExposesGruntTypeKey(t *testing.T) {
	body := getGrunts(t, "de")

	if e := gruntEntry(t, body, "5"); e["grunt_type"] != "grass" {
		t.Errorf("grunt 5 grunt_type = %v, want %q", e["grunt_type"], "grass")
	}
	if e := gruntEntry(t, body, "1"); e["grunt_type"] != "blanche" {
		t.Errorf("grunt 1 grunt_type = %v, want %q", e["grunt_type"], "blanche")
	}
}

// No locale keeps the existing English wire shape.
func TestMasterdataGrunts_DefaultsToEnglish(t *testing.T) {
	body := getGrunts(t, "")

	if e := gruntEntry(t, body, "5"); e["type"] != "Grass" {
		t.Errorf("grunt 5 type = %v, want English %q", e["type"], "Grass")
	}
}

// gamelocale ships grunt_<id> — the complete display name, already composed as
// type + category + gender and translated across 16 locales ("Dark - Grunt
// (Female)" / "Unlicht - Rüpel (Weiblich)"). Composing our own from the type
// and category halves duplicated work that was already done, and did it worse.
func TestMasterdataGrunts_UsesGruntIDDisplayName(t *testing.T) {
	body := getGrunts(t, "de")

	e := gruntEntry(t, body, "5")
	if e["name"] != "Pflanze - Ruepel (Weiblich)" {
		t.Errorf("grunt 5 name = %v, want the localised grunt_5 value", e["name"])
	}
	if e["short_name"] != "Pflanze ♀" {
		t.Errorf("grunt 5 short_name = %v, want the localised grunt_a_5 value", e["short_name"])
	}
}

// Named grunts get a real name, not a category label — this is what makes
// Blanche distinguishable from Candela without leaning on the type fallback.
func TestMasterdataGrunts_NamedGruntHasItsOwnName(t *testing.T) {
	body := getGrunts(t, "de")

	if e := gruntEntry(t, body, "1"); e["name"] != "Blanche" {
		t.Errorf("grunt 1 name = %v, want %q", e["name"], "Blanche")
	}
	if e := gruntEntry(t, body, "44"); e["name"] != "Giovanni" {
		t.Errorf("grunt 44 name = %v, want %q", e["name"], "Giovanni")
	}
}

// A grunt with no grunt_<id> key falls back to the composed type + category
// rather than leaking the bare key.
func TestMasterdataGrunts_NameFallsBackWhenUntranslated(t *testing.T) {
	body := getGrunts(t, "de")

	e := gruntEntry(t, body, "2")
	name, _ := e["name"].(string)
	if name == "grunt_2" || name == "" {
		t.Errorf("grunt 2 name = %q, want a composed fallback rather than the bare key", name)
	}
}
