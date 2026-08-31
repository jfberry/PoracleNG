package dts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestAPIEndToEndEnvelope proves a rendered api-platform payload flows,
// unmodified, all the way through a real APISender into the wire envelope.
// It renders a raid via the DTS renderer (an inline api entry), converts the
// resulting webhook.DeliveryJob into a delivery.Job the way cmd/processor's
// render pool does, and asserts the httptest-captured envelope carries the
// rendered payload plus the correct alert_type/destination metadata.
func TestAPIEndToEndEnvelope(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "default", Platform: "api",
			Template: map[string]any{"level": "{{level}}", "boss": "{{name}}"}},
	}
	r := newTestRenderer(t, entries)
	enrichment := map[string]any{"level": 5, "name": "Mewtwo", "latitude": 1.0, "longitude": 2.0, "tth": map[string]any{"totalSeconds": 600}}
	users := []webhook.MatchedUser{{ID: "u-1", Type: "api:user", Template: "default", Language: "en", Name: "James"}}
	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "ref", "")
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}

	var payload map[string]any
	var alertType, destType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var env map[string]any
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &env)
		alertType, _ = env["alert_type"].(string)
		if d, ok := env["destination"].(map[string]any); ok {
			destType, _ = d["type"].(string)
		}
		if p, ok := env["payload"].(map[string]any); ok {
			payload = p
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := delivery.NewAPISender(delivery.APIConfig{Endpoint: srv.URL, Secret: "x", TimeoutMs: 2000})
	dj := &delivery.Job{
		Target: jobs[0].Target, Type: jobs[0].Type, Name: jobs[0].Name,
		Language: jobs[0].Language, Message: jobs[0].Message,
		MsgType: "raid", TemplateID: jobs[0].TemplateSelected,
	}
	if _, err := s.Send(context.Background(), dj); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if alertType != "raid" || destType != "api:user" {
		t.Errorf("envelope alert_type=%q destType=%q", alertType, destType)
	}
	if payload["boss"] != "Mewtwo" {
		t.Errorf("payload.boss = %v, want Mewtwo", payload["boss"])
	}
}

// TestDiademPackSparsePokemonValidJSON pins the numeric-guard authoring rule
// for the shipped diadem partner pack (fallbacks/dts/diadem.toml): a sparse
// (unencountered) pokemon webhook must still render valid JSON, with the
// guarded fields resolving to null/0 rather than being emitted as bare
// Handlebars-empty tokens that break JSON parsing.
//
// The pokemon-schema entry ships with DTS type "monster" (not the schema
// name "pokemon") because that is the literal template-type string
// RenderPokemon/RenderPokemonChanged query at render time — see the note at
// the top of fallbacks/dts/diadem.toml. This test therefore looks up
// ts.Get("monster", "api", "diadem", "en"), matching real selection.
//
// The view mirrors real unencountered-pokemon enrichment output
// (internal/enrichment/pokemon.go): iv/atk/def/sta/cp/level keys are PRESENT
// with their unencountered sentinel values (iv=-1, atk/def/sta/cp/level=0),
// not absent. A genuinely-absent iv/atk/def/sta key is unsafe for the
// {{#if (gte x 0)}} guard (nil coerces to 0, which is >=0 — see
// task-2-report.md) but real enrichment always sets these keys, so this is
// the representative sparse shape, not the (also-guarded, but not the
// real-world path) absent-key case.
//
// This loads the real shipped file (not a copy) so a future edit to
// fallbacks/dts/diadem.toml that removes a guard fails this test immediately.
func TestDiademPackSparsePokemonValidJSON(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// .../processor/internal/dts/api_render_test.go -> repo root is 3 levels up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	fallbackDir := filepath.Join(repoRoot, "fallbacks")

	ts, err := LoadTemplates(t.TempDir(), fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	tmpl := ts.Get("monster", "api", "diadem", "en")
	if tmpl == nil {
		t.Fatal("no template found — expected fallbacks/dts/diadem.toml to provide monster/api/diadem")
	}

	// Deliberately sparse (unencountered): iv/atk/def/sta/cp/level present
	// with their real unencountered-sentinel values; every other optional
	// field (distance, despawnTimestamp, address, etc.) genuinely absent.
	view := map[string]any{
		"pokemonId":   25,
		"name":        "Pikachu",
		"encountered": false,
		"iv":          -1,
		"atk":         0,
		"def":         0,
		"sta":         0,
		"cp":          0,
		"level":       0,
	}

	rendered, err := tmpl.Exec(view)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !json.Valid([]byte(rendered)) {
		t.Fatalf("rendered output is not valid JSON: %s", rendered)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(rendered), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["iv"] != nil {
		t.Errorf("iv = %v, want nil (guard should emit JSON null for -1 sentinel)", parsed["iv"])
	}
	if parsed["cp"] != nil {
		t.Errorf("cp = %v, want nil", parsed["cp"])
	}
	if parsed["level"] != nil {
		t.Errorf("level = %v, want nil", parsed["level"])
	}
	if dist, ok := parsed["distance_m"].(float64); !ok || dist != 0 {
		t.Errorf("distance_m = %v, want 0", parsed["distance_m"])
	}
	if despawn, ok := parsed["despawn_at"].(float64); !ok || despawn != 0 {
		t.Errorf("despawn_at = %v, want 0", parsed["despawn_at"])
	}
	if parsed["pokemon_id"] != float64(25) {
		t.Errorf("pokemon_id = %v, want 25", parsed["pokemon_id"])
	}
	if parsed["name"] != "Pikachu" {
		t.Errorf("name = %v, want Pikachu", parsed["name"])
	}
}
