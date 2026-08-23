package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichWeatherChange_TemplateTypeAndFields covers superpowers/sdd
// task-4: the derived "weatherchange" DTS test type. Unlike the live
// consumeWeatherChanges path (cmd/processor/weather.go), which discovers the
// old/new weather IDs and affected-pokemon list from live tracker state,
// enrichWeatherChange takes a fully-specified partial (the testdata.json
// sample) supplying them directly. This confirms the base/perLang fields the
// "weatherchange" DTS template reads — including enrichedActivePokemons, the
// affected-pokemon list (see internal/api/dts_fields.go's
// weatherChangeFields) — are populated from that partial.
func TestEnrichWeatherChange_TemplateTypeAndFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "weatherchange", "rain")

	r, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange error: %v", err)
	}
	if r.templateType != "weatherchange" {
		t.Errorf("templateType = %q, want %q", r.templateType, "weatherchange")
	}
	if len(r.base) == 0 {
		t.Errorf("base enrichment is empty, want populated map")
	}
	if r.perLang == nil || r.perLang["weatherName"] == "" || r.perLang["weatherName"] == nil {
		t.Errorf("perLang[weatherName] = %v, want a non-empty translated weather name", r.perLang["weatherName"])
	}
	if r.perLang == nil || r.perLang["oldWeatherName"] == "" || r.perLang["oldWeatherName"] == nil {
		t.Errorf("perLang[oldWeatherName] = %v, want a non-empty translated weather name", r.perLang["oldWeatherName"])
	}

	// enrichedActivePokemons is the field weatherChangeFields (dts_fields.go)
	// documents as the affected-pokemon list the template reads.
	affected, ok := r.perLang["enrichedActivePokemons"].([]map[string]any)
	if !ok {
		t.Fatalf("perLang[enrichedActivePokemons] = %v (%T), want []map[string]any", r.perLang["enrichedActivePokemons"], r.perLang["enrichedActivePokemons"])
	}
	if len(affected) == 0 {
		t.Errorf("perLang[enrichedActivePokemons] is empty, want the testdata sample's affected-pokemon list")
	}
	for _, entry := range affected {
		if name, ok := entry["name"].(string); !ok || name == "" {
			t.Errorf("enrichedActivePokemons entry missing translated name: %+v", entry)
		}
	}
}

// TestEnrichForType_WeatherChange locks in the enrichForType dispatch added
// for this task: "weatherchange" (and its raw webhook-type spelling
// "weather-change") must resolve via enrichWeatherChange with the alias's
// TemplateType intact, matching the "incident" precedent from task 3.
func TestEnrichForType_WeatherChange(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "weatherchange", "rain")

	r, err := ps.enrichForType("weatherchange", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("weatherchange", ...) error: %v`, err)
	}
	if r.templateType != "weatherchange" {
		t.Errorf(`enrichForType("weatherchange", ...).templateType = %q, want "weatherchange"`, r.templateType)
	}

	r2, err := ps.enrichForType("weather-change", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("weather-change", ...) error: %v`, err)
	}
	if r2.templateType != "weatherchange" {
		t.Errorf(`enrichForType("weather-change", ...).templateType = %q, want "weatherchange"`, r2.templateType)
	}

	// Other still-unimplemented derived names must keep erroring — this task
	// only wires "weatherchange" (in addition to "incident" from task 3).
	if _, err := ps.enrichForType("monsterChanged", raw, "en", false); err == nil {
		t.Errorf(`enrichForType("monsterChanged", ...) error = nil, want a "derived type not yet supported" error`)
	}
}

// TestProcessTestWeatherChange_EnqueuesWeatherChangeTemplate verifies the
// !poracle-test / /api/test dispatch path: processTestWeatherChange must
// enqueue a RenderJob carrying TemplateType "weatherchange", built via the
// shared renderJobFromEnrich helper like the other non-pokemon test
// handlers.
func TestProcessTestWeatherChange_EnqueuesWeatherChangeTemplate(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "weatherchange", "rain")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestWeatherChange(raw, target); err != nil {
		t.Fatalf("processTestWeatherChange error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if job.TemplateType != "weatherchange" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "weatherchange")
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestEnrichWeatherChange_FreshenStaleTimeFlag locks in the freshenStaleTime
// behaviour documented on enrichWeatherChange: a past affected-pokemon
// disappear_time is bumped into the future only when freshenStaleTime=true
// (the editor-preview affordance), and left untouched when false (the live
// /api/test path's pre-existing convention — see enrichPokemon's doc comment
// for the full rationale this mirrors).
func TestEnrichWeatherChange_FreshenStaleTimeFlag(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := []byte(`{
		"latitude": 51.5, "longitude": -0.1,
		"gameplay_condition": 2, "old_gameplay_condition": 1,
		"affected": [{"pokemon_id": 129, "form": 0, "disappear_time": 1000000}]
	}`)

	live, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange error: %v", err)
	}
	liveAffected, ok := live.perLang["enrichedActivePokemons"].([]map[string]any)
	if !ok || len(liveAffected) != 1 {
		t.Fatalf("live perLang[enrichedActivePokemons] = %v, want a single-entry list", live.perLang["enrichedActivePokemons"])
	}
	if got, _ := liveAffected[0]["disappearTime"].(int64); got != wayInThePast {
		t.Errorf("freshenStaleTime=false: disappearTime = %v, want unchanged %d", liveAffected[0]["disappearTime"], wayInThePast)
	}

	editor, err := ps.enrichWeatherChange(raw, "en", true)
	if err != nil {
		t.Fatalf("enrichWeatherChange error: %v", err)
	}
	editorAffected, ok := editor.perLang["enrichedActivePokemons"].([]map[string]any)
	if !ok || len(editorAffected) != 1 {
		t.Fatalf("editor perLang[enrichedActivePokemons] = %v, want a single-entry list", editor.perLang["enrichedActivePokemons"])
	}
	got, _ := editorAffected[0]["disappearTime"].(int64)
	if got <= wayInThePast {
		t.Errorf("freshenStaleTime=true: disappearTime = %v, want bumped past %d (editor-preview affordance)", got, wayInThePast)
	}
}

// TestEnrichWeatherChange_ActivePokemonsHonorsConfig locks in the fix for the
// "!poracle-test weatherchange shows no changed pokemon" bug. The bundled and
// operator weatherchange templates iterate {{#each activePokemons}}, but
// WeatherTranslate only populates the activePokemons key when
// [weather] show_altered_pokemon_static_map is set (enrichedActivePokemons is
// always set — a separate key the templates don't read). enrichWeatherChange
// used to hardcode the flag false, so the test render never matched
// production (where the flag is on). It must read the flag from config, like
// the live consumeWeatherChanges path, so !poracle-test faithfully reproduces
// the live alert.
func TestEnrichWeatherChange_ActivePokemonsHonorsConfig(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.cfg = &config.Config{Weather: config.WeatherConfig{ShowAlteredPokemonStaticMap: true}}

	raw := loadTestdataSample(t, "weatherchange", "rain")
	r, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange error: %v", err)
	}

	// activePokemons is the key the weatherchange templates iterate — it must
	// be present (and non-empty) once the config flag is on.
	active, ok := r.perLang["activePokemons"].([]map[string]any)
	if !ok {
		t.Fatalf("perLang[activePokemons] = %v (%T), want []map[string]any — the key {{#each activePokemons}} reads", r.perLang["activePokemons"], r.perLang["activePokemons"])
	}
	if len(active) == 0 {
		t.Errorf("perLang[activePokemons] is empty, want the affected-pokemon list so the template renders it")
	}
}

// TestEnrichWeatherChange_ActivePokemonsNilCfg guards the nil-cfg fallback the
// original hardcode existed to protect: the enrich-parity harness leaves
// ps.cfg nil, so the flag reads false and activePokemons stays absent (the
// always-present enrichedActivePokemons still carries the data) — no panic.
func TestEnrichWeatherChange_ActivePokemonsNilCfg(t *testing.T) {
	ps := newEnrichParityService(t)
	if ps.cfg != nil {
		t.Fatalf("harness precondition: ps.cfg = %v, want nil", ps.cfg)
	}

	raw := loadTestdataSample(t, "weatherchange", "rain")
	r, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange error: %v", err)
	}
	if _, present := r.perLang["activePokemons"]; present {
		t.Errorf("nil cfg: perLang[activePokemons] present, want absent (flag defaults off)")
	}
	if _, ok := r.perLang["enrichedActivePokemons"].([]map[string]any); !ok {
		t.Errorf("nil cfg: perLang[enrichedActivePokemons] missing, want the always-present affected list")
	}
}

// TestEnrichWeatherChange_TilePreservedWhenFlagOn locks in the tile-selection
// fix. When [weather] show_altered_pokemon_static_map is on, enricher.Weather
// returns no base tile — it defers to the per-user tile that WeatherTranslate
// builds with active-pokemon markers. enrichWeatherChange must carry that
// pending through (mirroring the live consumeWeatherChanges "use per-user tile
// if available" selection); discarding it (the original perLang, _ = ... form)
// made the weather-change tile vanish from !poracle-test even though
// production shows it. The flag-off arm confirms the base tile still comes
// from enricher.Weather.
func TestEnrichWeatherChange_TilePreservedWhenFlagOn(t *testing.T) {
	ps := newEnrichParityService(t)
	// A .test URL never resolves (RFC 6761), so the tile workers fail fast
	// with no real egress; SubmitTile returns the pending synchronously
	// regardless, which is all this test inspects.
	resolver := staticmap.New(staticmap.Config{Provider: "tileservercache", ProviderURL: "http://tiles.test"})
	t.Cleanup(resolver.Close)
	ps.enricher.StaticMap = resolver

	raw := loadTestdataSample(t, "weatherchange", "rain")

	// Flag ON: base tile is nil, per-user tile must be carried through.
	ps.cfg = &config.Config{Weather: config.WeatherConfig{ShowAlteredPokemonStaticMap: true}}
	on, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange (flag on) error: %v", err)
	}
	if on.tilePending == nil {
		t.Error("flag on: tilePending = nil, want the per-user weather tile carried through from WeatherTranslate")
	}

	// Flag OFF: base tile is produced by enricher.Weather as before.
	ps.cfg = &config.Config{Weather: config.WeatherConfig{ShowAlteredPokemonStaticMap: false}}
	off, err := ps.enrichWeatherChange(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichWeatherChange (flag off) error: %v", err)
	}
	if off.tilePending == nil {
		t.Error("flag off: tilePending = nil, want the base weather tile from enricher.Weather")
	}
}

// TestProcessTest_DispatchesWeatherChange verifies the top-level ProcessTest
// switch has an explicit "weatherchange" case routing to
// processTestWeatherChange. Source-grep because exercising ProcessTest
// end-to-end needs a fully-wired dtsRenderer/dispatcher (same convention as
// TestProcessTest_DispatchesIncident in incident_test.go).
func TestProcessTest_DispatchesWeatherChange(t *testing.T) {
	src, err := os.ReadFile("test.go")
	if err != nil {
		t.Fatalf("read test.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(n, `case "weatherchange": return ps.processTestWeatherChange(raw, matchedUser)`) {
		t.Error(`test.go's ProcessTest switch missing: case "weatherchange": return ps.processTestWeatherChange(raw, matchedUser)`)
	}
}
