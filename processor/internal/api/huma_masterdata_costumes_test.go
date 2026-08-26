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
