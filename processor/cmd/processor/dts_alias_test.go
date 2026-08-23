package main

import (
	"encoding/json"
	"testing"
)

// TestDtsAlias_DTSNamesResolve covers the DTS template-type name → canonical
// webhook-source resolution described in CLAUDE.md's DTS template selection
// docs and superpowers/sdd task-2-brief.md. dtsAlias is the single table
// used by EnrichWebhook (this task), and later by testdata lookup / the
// !poracle-test command / the API's type-list endpoint (later tasks).
func TestDtsAlias_DTSNamesResolve(t *testing.T) {
	cases := []struct {
		name             string
		wantWebhookType  string
		wantTemplateType string
		wantDerived      bool
	}{
		{"monster", "pokemon", "monster", false},
		{"monsterNoIv", "pokemon", "monsterNoIv", false},
		// WebhookType for the four derived types below is the literal
		// testdata.json/wire spelling (underscores, or no separator for
		// weatherchange), not the hyphenated CLI-display spelling — see
		// dtsmap's doc comment on the canonical table.
		{"monsterChanged", "monster_changed", "monsterChanged", true},
		{"raid", "raid", "raid", false},
		{"egg", "raid", "egg", false},
		{"rsvpChanges", "rsvp_changes", "rsvpChanges", true},
		{"quest", "quest", "quest", false},
		{"questSummary", "quest_summary", "questSummary", true},
		{"invasion", "pokestop", "invasion", false},
		{"incident", "incident", "incident", true},
		{"showcase", "showcase", "showcase", false},
		{"lure", "pokestop", "lure", false},
		{"weatherchange", "weatherchange", "weatherchange", true},
		{"gym", "gym", "gym", false},
		{"nest", "nest", "nest", false},
		{"maxbattle", "max_battle", "maxbattle", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, ok := dtsAlias(tc.name)
			if !ok {
				t.Fatalf("dtsAlias(%q) ok = false, want true", tc.name)
			}
			if src.WebhookType != tc.wantWebhookType {
				t.Errorf("dtsAlias(%q).WebhookType = %q, want %q", tc.name, src.WebhookType, tc.wantWebhookType)
			}
			if src.TemplateType != tc.wantTemplateType {
				t.Errorf("dtsAlias(%q).TemplateType = %q, want %q", tc.name, src.TemplateType, tc.wantTemplateType)
			}
			if src.Derived != tc.wantDerived {
				t.Errorf("dtsAlias(%q).Derived = %v, want %v", tc.name, src.Derived, tc.wantDerived)
			}
		})
	}
}

// TestDtsAlias_RawWebhookTypesResolve confirms raw webhook-type spellings
// (as used by the existing EnrichWebhook switch and by Golbat/testdata.json)
// also resolve via dtsAlias, so callers don't need to know whether a given
// string is a DTS template name or the underlying wire type.
func TestDtsAlias_RawWebhookTypesResolve(t *testing.T) {
	cases := []struct {
		name             string
		wantWebhookType  string
		wantTemplateType string
	}{
		{"pokemon", "pokemon", "monster"},
		{"max_battle", "max_battle", "maxbattle"},
		{"fort_update", "fort_update", "fort-update"},
		// "fort-update" is the DTS TemplateType spelling but must resolve to
		// the same underscored WebhookType as "fort_update" — the literal
		// testdata.json/wire spelling — same as the four derived types
		// above, so ?dtsType=fort-update matches testdata entries instead
		// of returning zero results.
		{"fort-update", "fort_update", "fort-update"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, ok := dtsAlias(tc.name)
			if !ok {
				t.Fatalf("dtsAlias(%q) ok = false, want true", tc.name)
			}
			if src.WebhookType != tc.wantWebhookType {
				t.Errorf("dtsAlias(%q).WebhookType = %q, want %q", tc.name, src.WebhookType, tc.wantWebhookType)
			}
			if src.TemplateType != tc.wantTemplateType {
				t.Errorf("dtsAlias(%q).TemplateType = %q, want %q", tc.name, src.TemplateType, tc.wantTemplateType)
			}
		})
	}
}

func TestDtsAlias_Unknown(t *testing.T) {
	if _, ok := dtsAlias("not-a-real-type"); ok {
		t.Errorf("dtsAlias(unknown) ok = true, want false")
	}
}

// TestDtsTypeMap_ContainsCanonicalEntries checks the full-table accessor
// (exposed for the API in a later task) contains at least the canonical DTS
// names and is a defensive copy (mutating the result must not affect the
// package-level table).
// TestEnrichWebhook_ResolvesDTSNames confirms EnrichWebhook itself performs
// the name resolution: calling it with a DTS template-type name behaves
// exactly like calling it with the underlying raw webhook type, and derived
// names (not yet wired into the switch — Tasks 3-7 do that) still return the
// pre-existing "unsupported" error rather than silently misrouting.
func TestEnrichWebhook_ResolvesDTSNames(t *testing.T) {
	ps := newEnrichParityService(t)

	pokemonRaw := loadTestdataSample(t, "pokemon", "hundo")
	if _, err := ps.EnrichWebhook("monster", pokemonRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("monster", ...) error: %v, want success like EnrichWebhook("pokemon", ...)`, err)
	}
	if _, err := ps.EnrichWebhook("monsterNoIv", pokemonRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("monsterNoIv", ...) error: %v, want success (resolves to "pokemon")`, err)
	}
	if _, err := ps.EnrichWebhook("pokemon", pokemonRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("pokemon", ...) error: %v, want success`, err)
	}

	eggRaw := loadTestdataSample(t, "raid", "egg1")
	if _, err := ps.EnrichWebhook("egg", eggRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("egg", ...) error: %v, want success (resolves to "raid")`, err)
	}
	if _, err := ps.EnrichWebhook("raid", eggRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("raid", ...) error: %v, want success`, err)
	}

	invasionRaw := loadTestdataSample(t, "pokestop", "invasion")
	if _, err := ps.EnrichWebhook("invasion", invasionRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("invasion", ...) error: %v, want success (must not be rewritten to unsupported "pokestop")`, err)
	}

	lureRaw := loadTestdataSample(t, "pokestop", "lure")
	if _, err := ps.EnrichWebhook("lure", lureRaw, "en", "discord"); err != nil {
		t.Errorf(`EnrichWebhook("lure", ...) error: %v, want success (must not be rewritten to unsupported "pokestop")`, err)
	}

	if _, err := ps.EnrichWebhook("monsterChanged", pokemonRaw, "en", "discord"); err == nil {
		t.Errorf(`EnrichWebhook("monsterChanged", ...) error = nil, want an "unsupported" error — derived-type handling isn't wired until Tasks 3-7`)
	}
}

// TestEnrichForType_AliasTemplateTypeAuthoritative locks in the fix for two
// verified defects where the resolved alias's TemplateType was discarded in
// favor of whatever the underlying enrich* function hardcodes or infers from
// the payload:
//
//   - "monsterNoIv" dispatches through enrichPokemon just like "monster", but
//     enrichPokemon always hardcodes templateType "monster". Without the
//     alias's TemplateType winning, enrichForType("monsterNoIv", ...) would
//     be indistinguishable from enrichForType("monster", ...), silently
//     losing the distinct DTS alias set view.go keys off templateType (see
//     CLAUDE.md's DTS template selection docs).
//   - "egg" is always rewritten to the "raid" WebhookType before dispatch
//     (mirroring the pre-existing "pokestop" rewrite guard), so it always
//     reaches enrichRaid with isEgg=false; enrichRaid then infers "raid" vs
//     "egg" from raid.PokemonID. For a malformed egg fixture (pokemon_id !=
//     0 — Golbat is not expected to send this, but the alias was requested
//     explicitly), that inference lands on "raid", not "egg".
//
// This test exercises enrichForType directly at the *enrichResult level
// rather than through EnrichWebhook: the existing harness doesn't set
// dtsRenderer, so EnrichWebhook takes the mergeEnrichment fallback and
// templateType is invisible in its return value — exactly how this defect
// slipped through review.
func TestEnrichForType_AliasTemplateTypeAuthoritative(t *testing.T) {
	ps := newEnrichParityService(t)

	pokemonRaw := loadTestdataSample(t, "pokemon", "hundo")

	t.Run("monster", func(t *testing.T) {
		r, err := ps.enrichForType("monster", pokemonRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("monster", ...) error: %v`, err)
		}
		if r.templateType != "monster" {
			t.Errorf(`enrichForType("monster", ...).templateType = %q, want "monster"`, r.templateType)
		}
	})

	t.Run("monsterNoIv", func(t *testing.T) {
		r, err := ps.enrichForType("monsterNoIv", pokemonRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("monsterNoIv", ...) error: %v`, err)
		}
		if r.templateType != "monsterNoIv" {
			t.Errorf(`enrichForType("monsterNoIv", ...).templateType = %q, want "monsterNoIv" (alias's TemplateType must win over enrichPokemon's hardcoded "monster")`, r.templateType)
		}
	})

	t.Run("pokemon", func(t *testing.T) {
		r, err := ps.enrichForType("pokemon", pokemonRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("pokemon", ...) error: %v`, err)
		}
		if r.templateType != "monster" {
			t.Errorf(`enrichForType("pokemon", ...).templateType = %q, want "monster"`, r.templateType)
		}
	})

	t.Run("raid", func(t *testing.T) {
		raidRaw := loadTestdataSample(t, "raid", "level1")
		r, err := ps.enrichForType("raid", raidRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("raid", ...) error: %v`, err)
		}
		if r.templateType != "raid" {
			t.Errorf(`enrichForType("raid", ...).templateType = %q, want "raid"`, r.templateType)
		}
	})

	t.Run("egg_malformed_pokemon_id", func(t *testing.T) {
		// A malformed egg: the "egg" alias is requested explicitly, but the
		// payload carries a real pokemon_id — the signal enrichRaid uses to
		// infer "raid" vs "egg" when not told which one was requested.
		malformedEggRaw := json.RawMessage(`{
			"gym_id": "g1",
			"latitude": 51.5,
			"longitude": -0.1,
			"pokemon_id": 150,
			"level": 5,
			"start": 999000,
			"end": 1000000
		}`)
		r, err := ps.enrichForType("egg", malformedEggRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("egg", ...) error: %v`, err)
		}
		if r.templateType != "egg" {
			t.Errorf(`enrichForType("egg", <pokemon_id != 0>).templateType = %q, want "egg" (alias's TemplateType must win over enrichRaid's payload-based inference)`, r.templateType)
		}
	})

	t.Run("invasion", func(t *testing.T) {
		invasionRaw := loadTestdataSample(t, "pokestop", "invasion")
		r, err := ps.enrichForType("invasion", invasionRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("invasion", ...) error: %v`, err)
		}
		if r.templateType != "invasion" {
			t.Errorf(`enrichForType("invasion", ...).templateType = %q, want "invasion"`, r.templateType)
		}
	})

	t.Run("lure", func(t *testing.T) {
		lureRaw := loadTestdataSample(t, "pokestop", "lure")
		r, err := ps.enrichForType("lure", lureRaw, "en", true)
		if err != nil {
			t.Fatalf(`enrichForType("lure", ...) error: %v`, err)
		}
		if r.templateType != "lure" {
			t.Errorf(`enrichForType("lure", ...).templateType = %q, want "lure"`, r.templateType)
		}
	})

	t.Run("derived_name_errors", func(t *testing.T) {
		if _, err := ps.enrichForType("monsterChanged", pokemonRaw, "en", true); err == nil {
			t.Errorf(`enrichForType("monsterChanged", ...) error = nil, want a "derived type not yet supported" error`)
		}
	})
}

func TestDtsTypeMap_ContainsCanonicalEntries(t *testing.T) {
	m := dtsTypeMap()
	for _, name := range []string{"monster", "raid", "egg", "quest", "invasion", "lure", "gym", "nest", "maxbattle"} {
		if _, ok := m[name]; !ok {
			t.Errorf("dtsTypeMap() missing entry %q", name)
		}
	}

	delete(m, "monster")
	if _, ok := dtsAlias("monster"); !ok {
		t.Errorf("mutating dtsTypeMap() result affected dtsAlias — table should be defensively copied")
	}
}
