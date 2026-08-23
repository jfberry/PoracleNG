package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichMonsterChanged_TemplateTypeAndFields covers superpowers/sdd
// task-6: the derived "monsterChanged" DTS test type. The testdata.json
// sample ("monster_changed"/"ditto-reveal" — the underscore wire spelling;
// see resolveDTSTypeFromRaw) pairs an unencountered wild sighting disguised
// as Foongus (pokemon_id 590) with the same encounter later revealed and
// encountered as Ditto (pokemon_id 132) — the real-world species-change
// scenario dispatchPokemonAlert's ChangeSpecies bucket exists for.
// enrichMonsterChanged enriches `new` via the same enrichPokemon helper the
// plain "pokemon" test path uses, and builds extras["original"] from `old`
// via tracker.EncounterStateFromPokemon + dts.BuildOriginalView — this
// confirms both halves land correctly and stay distinct.
func TestEnrichMonsterChanged_TemplateTypeAndFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")

	r, err := ps.enrichMonsterChanged(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichMonsterChanged error: %v", err)
	}
	if r.templateType != "monsterChanged" {
		t.Errorf("templateType = %q, want %q", r.templateType, "monsterChanged")
	}
	if len(r.base) == 0 {
		t.Fatalf("base enrichment is empty, want the `new` pokemon's populated map")
	}

	// base/perLang describe the NEW sighting (Ditto, pokemon_id 132).
	if got, _ := r.base["pokemonId"].(int); got != 132 {
		t.Errorf(`base["pokemonId"] = %v, want 132 (new/Ditto)`, r.base["pokemonId"])
	}

	original, ok := r.extras["original"].(map[string]any)
	if !ok {
		t.Fatalf(`extras["original"] = %v (%T), want map[string]any`, r.extras["original"], r.extras["original"])
	}

	// original.* describes the OLD sighting (disguised Foongus, pokemon_id
	// 590) — distinct from the new pokemonId above.
	if got, _ := original["pokemonId"].(int); got != 590 {
		t.Errorf(`extras["original"]["pokemonId"] = %v, want 590 (old/Foongus disguise)`, original["pokemonId"])
	}
	if got, _ := original["encountered"].(bool); got {
		t.Errorf(`extras["original"]["encountered"] = %v, want false (old sighting had no CP/IVs yet)`, original["encountered"])
	}
	if got, _ := original["cp"].(int); got != 0 {
		t.Errorf(`extras["original"]["cp"] = %v, want 0 (unencountered old sighting)`, original["cp"])
	}
	name, _ := original["name"].(string)
	if name == "" {
		t.Errorf(`extras["original"]["name"] is empty, want a translated pokemon name for the old sighting`)
	}
	fullName, _ := original["fullName"].(string)
	if fullName == "" {
		t.Errorf(`extras["original"]["fullName"] is empty, want a translated name for the old sighting`)
	}
}

// TestEnrichMonsterChanged_ChangeTypeFields covers the Task 6 fix: the
// bundled monsterChanged templates render {{changeTypeText}} unguarded (see
// fallbacks/dts.json's "— {{changeTypeText}}" description text), so without
// this, !poracle-test monster-changed,ditto-reveal renders a dangling "— "
// with nothing after it. enrichMonsterChanged must populate
// changeType/changeTypeText on perLang the same way the live
// dispatchPokemonAlert path does via perLangWithChangeFields. Foongus (590)
// -> Ditto (132) is a species-level identity change, so the expected bucket
// is "species".
func TestEnrichMonsterChanged_ChangeTypeFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")

	r, err := ps.enrichMonsterChanged(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichMonsterChanged error: %v", err)
	}

	if got, _ := r.perLang["changeType"].(string); got != "species" {
		t.Errorf(`r.perLang["changeType"] = %q, want "species" (pokemon_id 590 -> 132)`, got)
	}

	wantText := ps.translatorFor("en").T("change_type_text_species")
	got, _ := r.perLang["changeTypeText"].(string)
	if got == "" {
		t.Errorf(`r.perLang["changeTypeText"] is empty, want %q`, wantText)
	}
	if got != wantText {
		t.Errorf(`r.perLang["changeTypeText"] = %q, want %q`, got, wantText)
	}
}

// TestProcessTestMonsterChanged_PerLangCarriesChangeTypeText confirms the
// changeType/changeTypeText fields enrichMonsterChanged sets survive into the
// RenderJob's PerLangEnrichment (renderJobFromEnrich copies r.perLang into
// perLang[target.Language]) — the exact LayeredView field the bundled
// {{changeTypeText}} template expression reads at render time (priority
// level 3, ahead of base/aliases/webhook — see LayeredView doc in CLAUDE.md).
func TestProcessTestMonsterChanged_PerLangCarriesChangeTypeText(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestMonsterChanged(raw, target); err != nil {
		t.Fatalf("processTestMonsterChanged error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		langView, ok := job.PerLangEnrichment["en"]
		if !ok {
			t.Fatalf("job.PerLangEnrichment missing \"en\" slot: %+v", job.PerLangEnrichment)
		}
		if got, _ := langView["changeTypeText"].(string); got == "" {
			t.Errorf(`job.PerLangEnrichment["en"]["changeTypeText"] is empty, want a non-empty translated string`)
		}
		if got, _ := langView["changeType"].(string); got != "species" {
			t.Errorf(`job.PerLangEnrichment["en"]["changeType"] = %q, want "species"`, got)
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestEnrichForType_MonsterChanged locks in the enrichForType dispatch added
// for this task: "monsterChanged" (and its raw webhook-type spelling
// "monster-changed") must resolve via enrichMonsterChanged with the alias's
// TemplateType intact, matching the incident/weatherchange/questSummary
// precedent from tasks 3-5.
func TestEnrichForType_MonsterChanged(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")

	r, err := ps.enrichForType("monsterChanged", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("monsterChanged", ...) error: %v`, err)
	}
	if r.templateType != "monsterChanged" {
		t.Errorf(`enrichForType("monsterChanged", ...).templateType = %q, want "monsterChanged"`, r.templateType)
	}
	if _, ok := r.extras["original"].(map[string]any); !ok {
		t.Errorf(`enrichForType("monsterChanged", ...).extras["original"] missing or wrong type: %v`, r.extras["original"])
	}

	r2, err := ps.enrichForType("monster-changed", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("monster-changed", ...) error: %v`, err)
	}
	if r2.templateType != "monsterChanged" {
		t.Errorf(`enrichForType("monster-changed", ...).templateType = %q, want "monsterChanged"`, r2.templateType)
	}

	// Unrecognized names must keep erroring. "rsvpChanges" used to be the
	// still-unimplemented case asserted here, but superpowers/sdd task-7
	// wired it up (the last derived type) — see rsvpchanges_test.go's
	// TestEnrichForType_RsvpChanges for its now-passing coverage.
	if _, err := ps.enrichForType("totally-bogus-derived-type", raw, "en", false); err == nil {
		t.Errorf(`enrichForType("totally-bogus-derived-type", ...) error = nil, want an "unsupported webhook type" error`)
	}
}

// TestEnrichWebhook_MonsterChanged confirms the /api/dts/enrich-facing
// surface (EnrichWebhook) exposes the {{original.X}} prior-sighting view
// under a top-level "original" key — the injectOriginalExtra step, since
// LayeredView.Flatten (unlike LayeredView.GetField, used during real
// template rendering) doesn't special-case "original" on its own.
// newEnrichParityService doesn't wire a dtsRenderer, so this exercises the
// mergeEnrichment fallback branch of EnrichWebhook.
func TestEnrichWebhook_MonsterChanged(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")

	vars, err := ps.EnrichWebhook("monsterChanged", raw, "en", "discord")
	if err != nil {
		t.Fatalf(`EnrichWebhook("monsterChanged", ...) error: %v`, err)
	}

	original, ok := vars["original"].(map[string]any)
	if !ok {
		t.Fatalf(`EnrichWebhook("monsterChanged", ...)["original"] = %v (%T), want map[string]any`, vars["original"], vars["original"])
	}
	if got, _ := original["pokemonId"].(int); got != 590 {
		t.Errorf(`vars["original"]["pokemonId"] = %v, want 590 (old/Foongus disguise)`, original["pokemonId"])
	}
	if got, _ := vars["pokemonId"].(int); got != 132 {
		t.Errorf(`vars["pokemonId"] = %v, want 132 (new/Ditto)`, vars["pokemonId"])
	}
}

// TestEnrichWebhook_SyntheticPerUserPVP locks in the fix for a verified
// defect: EnrichWebhook's synthetic "_editor" PokemonPerUser block (the
// per-user PVP display data the editor preview shows) was gated on
// `src.WebhookType == "pokemon"`, which is true for "monster"/"monsterNoIv"
// (WebhookType "pokemon") but false for "monsterChanged" (WebhookType is the
// derived "monster_changed" spelling) — even though monsterChanged's base
// enrichment IS a pokemon spawn (the NEW sighting, built via enrichPokemon —
// see enrichMonsterChanged's doc comment) and the LIVE path
// (processTestMonsterChanged -> renderJobFromEnrich(isPokemon=true)) DOES
// compute this block. So a monsterChanged template's {{pvpGreat}}/etc.
// previewed empty in the editor but rendered populated live.
//
// Unlike TestEnrichWebhook_MonsterChanged above, this wires a real
// dtsRenderer so EnrichWebhook takes the LayeredView.Flatten branch instead
// of the mergeEnrichment fallback — mergeEnrichment never carries perUser at
// all (by design; see EnrichWebhook's branch), so the fallback path can't
// distinguish "computed but not merged" from "never computed" and would mask
// this defect either way.
func TestEnrichWebhook_SyntheticPerUserPVP(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.enricher.PVPDisplay = &enrichment.PVPDisplayConfig{}

	// dts.NewRenderer requires at least an empty dts.json to be present
	// (it errors "no DTS source found" otherwise) — this test only needs
	// the renderer's ViewBuilder (EnrichWebhook's LayeredView.Flatten
	// branch), never a real template lookup, so an empty entry list is
	// sufficient.
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "dts.json"), []byte("[]"), 0644); err != nil {
		t.Fatalf("write dts.json: %v", err)
	}
	renderer, err := dts.NewRenderer(dts.RendererConfig{
		ConfigDir:     configDir,
		FallbackDir:   t.TempDir(),
		DefaultLocale: "en",
	})
	if err != nil {
		t.Fatalf("dts.NewRenderer: %v", err)
	}
	ps.dtsRenderer = renderer

	pokemonRaw := loadTestdataSample(t, "pokemon", "hundo")
	monsterChangedRaw := loadTestdataSample(t, "monster_changed", "ditto-reveal")

	t.Run("monster", func(t *testing.T) {
		vars, err := ps.EnrichWebhook("monster", pokemonRaw, "en", "discord")
		if err != nil {
			t.Fatalf(`EnrichWebhook("monster", ...) error: %v`, err)
		}
		if _, ok := vars["userHasPvpTracks"]; !ok {
			t.Errorf(`EnrichWebhook("monster", ...)["userHasPvpTracks"] missing, want the synthetic-user PVP block to be present (no regression)`)
		}
	})

	t.Run("monsterNoIv", func(t *testing.T) {
		vars, err := ps.EnrichWebhook("monsterNoIv", pokemonRaw, "en", "discord")
		if err != nil {
			t.Fatalf(`EnrichWebhook("monsterNoIv", ...) error: %v`, err)
		}
		if _, ok := vars["userHasPvpTracks"]; !ok {
			t.Errorf(`EnrichWebhook("monsterNoIv", ...)["userHasPvpTracks"] missing, want the synthetic-user PVP block to be present (no regression)`)
		}
	})

	t.Run("monsterChanged", func(t *testing.T) {
		vars, err := ps.EnrichWebhook("monsterChanged", monsterChangedRaw, "en", "discord")
		if err != nil {
			t.Fatalf(`EnrichWebhook("monsterChanged", ...) error: %v`, err)
		}
		if _, ok := vars["userHasPvpTracks"]; !ok {
			t.Errorf(`EnrichWebhook("monsterChanged", ...)["userHasPvpTracks"] missing, want the synthetic-user PVP block to be present — matches the live processTestMonsterChanged/renderJobFromEnrich path, which always computes it for monsterChanged`)
		}
	})
}

// TestProcessTestMonsterChanged_EnqueuesChangeJob verifies the
// !poracle-test / /api/test dispatch path (wire type "monster_changed" —
// see resolveDTSTypeFromRaw): processTestMonsterChanged must enqueue a
// RenderJob with IsChange=true, TemplateType "monsterChanged", and a
// populated OriginalView reflecting the OLD sighting — the shape
// processRenderJob's IsChange branch needs to call RenderPokemonChanged
// instead of RenderPokemon.
func TestProcessTestMonsterChanged_EnqueuesChangeJob(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "monster_changed", "ditto-reveal")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestMonsterChanged(raw, target); err != nil {
		t.Fatalf("processTestMonsterChanged error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if !job.IsChange {
			t.Errorf("job.IsChange = false, want true")
		}
		if job.TemplateType != "monsterChanged" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "monsterChanged")
		}
		if !job.IsPokemon {
			t.Errorf("job.IsPokemon = false, want true")
		}
		if job.ReplyKey != "9912873645123456789" {
			t.Errorf("job.ReplyKey = %q, want the new sighting's encounter_id %q", job.ReplyKey, "9912873645123456789")
		}
		if job.OriginalView == nil {
			t.Fatalf("job.OriginalView is nil, want the old-sighting view")
		}
		if got, _ := job.OriginalView["pokemonId"].(int); got != 590 {
			t.Errorf("job.OriginalView[\"pokemonId\"] = %v, want 590 (old/Foongus disguise)", job.OriginalView["pokemonId"])
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestProcessTest_DispatchesMonsterChanged verifies the top-level
// ProcessTest switch has an explicit "monster_changed" case (the wire
// spelling — see resolveDTSTypeFromRaw) routing to
// processTestMonsterChanged. Source-grep because exercising ProcessTest
// end-to-end needs a fully-wired dtsRenderer/dispatcher (same convention as
// TestProcessTest_DispatchesIncident/Weatherchange/QuestSummary).
func TestProcessTest_DispatchesMonsterChanged(t *testing.T) {
	src, err := os.ReadFile("test.go")
	if err != nil {
		t.Fatalf("read test.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(n, `case "monster_changed": return ps.processTestMonsterChanged(raw, matchedUser)`) {
		t.Error(`test.go's ProcessTest switch missing: case "monster_changed": return ps.processTestMonsterChanged(raw, matchedUser)`)
	}
}

// TestResolveDTSTypeFromRaw_MonsterChanged locks in the wire-spelling→DTS-type
// mapping ProcessTest's CheckTemplate call depends on: without this explicit
// case, resolveDTSTypeFromRaw's default branch would return "monster_changed"
// unchanged, which doesn't match the registered "monsterChanged" DTS
// template type — CheckTemplate would fail to find it and every
// !poracle-test monster-changed,<id> / POST /api/test
// {"type":"monster_changed"} invocation would error out before ever reaching
// processTestMonsterChanged.
func TestResolveDTSTypeFromRaw_MonsterChanged(t *testing.T) {
	if got := resolveDTSTypeFromRaw("monster_changed", nil); got != "monsterChanged" {
		t.Errorf(`resolveDTSTypeFromRaw("monster_changed", nil) = %q, want "monsterChanged"`, got)
	}
}
