package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// This file characterises the behaviour of the per-type enrich* functions in
// enrich.go BEFORE and AFTER unifying test.go onto them (superpowers/sdd task
// 1). It exists to prove the refactor is behaviour-preserving: test.go's
// processTest* handlers used to re-run this same enrichment inline; they now
// call these functions (plus renderJobFromEnrich) instead. If a future change
// alters templateType selection or silently drops enrichment data, this test
// should catch it.

// testdataEntry mirrors one row of fallbacks/testdata.json — the same
// bundled fixtures !poracle-test (/api/test) uses.
type testdataEntry struct {
	Type    string          `json:"type"`
	Test    string          `json:"test"`
	Webhook json.RawMessage `json:"webhook"`
}

// repoRoot walks up from this package (processor/cmd/processor) to the repo
// root, where fallbacks/ and resources/ live (see CLAUDE.md directory layout).
func repoRoot() string {
	return filepath.Join("..", "..", "..")
}

// loadTestdataSample returns the raw "webhook" payload for the given
// (type, test-id) pair from the bundled fallbacks/testdata.json. Skips the
// test if the fixture file isn't present.
func loadTestdataSample(t *testing.T, webhookType, testID string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "fallbacks", "testdata.json"))
	if err != nil {
		t.Skipf("read fallbacks/testdata.json: %v", err)
	}
	var entries []testdataEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse testdata.json: %v", err)
	}
	for _, e := range entries {
		if e.Type == webhookType && e.Test == testID {
			return e.Webhook
		}
	}
	t.Fatalf("no testdata entry for type=%q test=%q", webhookType, testID)
	return nil
}

// newEnrichParityService builds a ProcessorService wired with real game data
// and translations loaded from the repo's resources/ directory (the same
// data the live processor downloads at startup), so enrich* functions exercise
// their normal code paths (name/type/move lookups, translated fields, etc.)
// instead of the nil-GameData short-circuits. Skips the test if resources/
// hasn't been populated yet (gitignored; downloaded on first processor run —
// same skip condition internal/enrichment's equivalence tests use).
func newEnrichParityService(t *testing.T) *ProcessorService {
	t.Helper()
	gd, err := gamedata.Load(repoRoot())
	if err != nil {
		t.Skipf("load game data: %v (run the processor once to populate resources/)", err)
	}
	tr := i18n.Load(repoRoot())

	return &ProcessorService{
		enricher: &enrichment.Enricher{
			GameData:        gd,
			Translations:    tr,
			WeatherProvider: stubWeatherProvider{}, // Pokemon/Raid/Invasion/Maxbattle enrichment dereferences this unconditionally
			IvColors:        []string{"#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"},
		},
		// translations mirrors enricher.Translations — real processor init sets
		// both from the same i18n.Load call (see main.go). buildQuestSummaryGroupView
		// (used by enrichQuestSummary) reads ps.translations directly, same as
		// the live DispatchQuestSummary scheduler path; a nil *i18n.Bundle would
		// panic on .For(lang) rather than fail gracefully.
		translations: tr,
		stats:        &tracker.StatsTracker{}, // zero-value: GetRarityGroup falls back to RarityUnknown
		pvpCfg:       &pvp.Config{},
	}
}

func TestEnrichTypes_TemplateTypeAndNonEmptyBase(t *testing.T) {
	ps := newEnrichParityService(t)

	cases := []struct {
		name             string
		webhookType      string
		testID           string
		enrich           func(raw json.RawMessage) (*enrichResult, error)
		wantTemplateType string
	}{
		{
			name:        "pokemon",
			webhookType: "pokemon",
			testID:      "hundo",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichPokemon(raw, "en", true)
			},
			wantTemplateType: "monster",
		},
		{
			name:        "raid",
			webhookType: "raid",
			testID:      "level1",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichRaid(raw, "en", false, true)
			},
			wantTemplateType: "raid",
		},
		{
			name:        "egg",
			webhookType: "raid",
			testID:      "egg1",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				// isEgg=false mirrors processTestRaid: the actual template
				// type is decided by raid.PokemonID inside enrichRaid, not
				// by the caller's isEgg flag, for the "raid"/"egg" test path.
				return ps.enrichRaid(raw, "en", false, true)
			},
			wantTemplateType: "egg",
		},
		{
			name:        "quest",
			webhookType: "quest",
			testID:      "quest-item",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichQuest(raw, "en")
			},
			wantTemplateType: "quest",
		},
		{
			name:        "invasion",
			webhookType: "pokestop",
			testID:      "invasion",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichInvasion(raw, "en", true)
			},
			wantTemplateType: "invasion",
		},
		{
			name:        "lure",
			webhookType: "pokestop",
			testID:      "lure",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichLure(raw, "en")
			},
			wantTemplateType: "lure",
		},
		{
			name:        "gym",
			webhookType: "gym",
			testID:      "teamchange",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichGym(raw, "en")
			},
			wantTemplateType: "gym",
		},
		{
			name:        "fort_update",
			webhookType: "fort_update",
			testID:      "edit",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichFort(raw, "en")
			},
			wantTemplateType: "fort-update",
		},
		{
			name:        "maxbattle",
			webhookType: "max_battle",
			testID:      "level1",
			enrich: func(raw json.RawMessage) (*enrichResult, error) {
				return ps.enrichMaxbattle(raw, "en")
			},
			wantTemplateType: "maxbattle",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadTestdataSample(t, tc.webhookType, tc.testID)
			r, err := tc.enrich(raw)
			if err != nil {
				t.Fatalf("enrich error: %v", err)
			}
			if r.templateType != tc.wantTemplateType {
				t.Errorf("templateType = %q, want %q", r.templateType, tc.wantTemplateType)
			}
			if len(r.base) == 0 {
				t.Errorf("base enrichment is empty, want populated map")
			}
			if r.webhookFields == nil {
				t.Errorf("webhookFields is nil, want parsed raw JSON fields")
			}
		})
	}
}

// TestEnrichNest hand-rolls a minimal NestWebhook payload since fallbacks/
// testdata.json (as of this writing) has no bundled "nest" sample.
func TestEnrichNest(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := json.RawMessage(`{
		"nest_id": 1234,
		"name": "Test Park",
		"lat": 51.5,
		"lon": -0.1,
		"pokemon_id": 1,
		"form": 0,
		"pokemon_count": 42,
		"pokemon_avg": 6.0,
		"pokemon_ratio": 0.9,
		"reset_time": 1775478965
	}`)

	r, err := ps.enrichNest(raw, "en")
	if err != nil {
		t.Fatalf("enrichNest error: %v", err)
	}
	if r.templateType != "nest" {
		t.Errorf("templateType = %q, want nest", r.templateType)
	}
	if len(r.base) == 0 {
		t.Errorf("base enrichment is empty, want populated map")
	}
}

// TestEnrichPokemon_DoesNotComputePerUser locks in Step 2 of the enrichment
// unification: enrichPokemon must no longer compute the synthetic-user PVP
// perUser map itself — that responsibility moved to EnrichWebhook (editor
// path only). The live /api/test path (via renderJobFromEnrich) computes
// perUser separately using the REAL target user. If enrichPokemon starts
// setting perUser again, EnrichWebhook's editor block and renderJobFromEnrich
// would both feed it a preset perUser (or double-compute it), which is
// exactly the duplication this task removes.
func TestEnrichPokemon_DoesNotComputePerUser(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "pokemon", "great-rank1")

	r, err := ps.enrichPokemon(raw, "en", true)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	if r.perUser != nil {
		t.Errorf("enrichPokemon set perUser = %v, want nil — perUser must be computed by the caller (EnrichWebhook for the editor's synthetic user, renderJobFromEnrich for the live target user)", r.perUser)
	}
}

// TestEnrichPokemon_ExtrasEncountered verifies enrichPokemon threads the
// Encountered flag through extras["encountered"] — this is how
// processTestPokemon (test.go) now recovers IsEncountered without
// re-unmarshalling/re-processing the webhook itself.
func TestEnrichPokemon_ExtrasEncountered(t *testing.T) {
	ps := newEnrichParityService(t)

	encounteredRaw := loadTestdataSample(t, "pokemon", "hundo")
	r, err := ps.enrichPokemon(encounteredRaw, "en", true)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	if got, ok := r.extras["encountered"].(bool); !ok || !got {
		t.Errorf("extras[encountered] = %v (ok=%v), want true for an IV-scanned sample", r.extras["encountered"], ok)
	}

	unencounteredRaw := loadTestdataSample(t, "pokemon", "unencountered")
	r2, err := ps.enrichPokemon(unencounteredRaw, "en", true)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	if got, ok := r2.extras["encountered"].(bool); !ok || got {
		t.Errorf("extras[encountered] = %v (ok=%v), want false for the unencountered sample", r2.extras["encountered"], ok)
	}
}

// TestEnrichPokemon_PerLangHasNameFields checks that the per-language layer
// (the same one test.go used to build inline via PokemonTranslate) carries
// the translated name fields templates rely on.
func TestEnrichPokemon_PerLangHasNameFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "pokemon", "hundo")

	r, err := ps.enrichPokemon(raw, "en", true)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	if r.perLang == nil {
		t.Fatalf("perLang is nil, want populated translation map (GameData+Translations are set)")
	}
	if _, ok := r.perLang["name"]; !ok {
		t.Errorf("perLang missing %q key", "name")
	}
	if _, ok := r.perLang["fullName"]; !ok {
		t.Errorf("perLang missing %q key", "fullName")
	}
}

// The following three tests lock in a subtlety found while wiring test.go
// onto the shared enrich* core (superpowers/sdd task 1): enrichPokemon,
// enrichRaid, and enrichInvasion were originally written for the DTS editor
// preview (/api/dts/enrich) and unconditionally bump an already-past
// timestamp into the near future so canned sample JSON never previews as
// "already expired". The pre-refactor /api/test (poracle-test) handlers in
// test.go never had this correction. Naively calling the shared enrich*
// functions from test.go would have silently given the live test path new
// behaviour for stale fixtures — every bundled fallbacks/testdata.json
// timestamp becomes stale as real time passes it. The freshenStaleTime
// parameter keeps both call sites byte-for-byte on their pre-existing
// behaviour: EnrichWebhook (editor) always passes true; test.go's
// processTest* handlers always pass false.

const wayInThePast = int64(1_000_000) // 1970-01-12 — always in the past

func TestEnrichPokemon_FreshenStaleTimeFlag(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := json.RawMessage(`{"pokemon_id":1,"latitude":51.5,"longitude":-0.1,"encounter_id":"abc","disappear_time":1000000}`)

	live, err := ps.enrichPokemon(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	if got, _ := live.base["despawnTimestamp"].(int64); got != wayInThePast {
		t.Errorf("freshenStaleTime=false: despawnTimestamp = %v, want unchanged %d (this is the live /api/test path's pre-existing behaviour)", live.base["despawnTimestamp"], wayInThePast)
	}

	editor, err := ps.enrichPokemon(raw, "en", true)
	if err != nil {
		t.Fatalf("enrichPokemon error: %v", err)
	}
	got, _ := editor.base["despawnTimestamp"].(int64)
	if got <= wayInThePast {
		t.Errorf("freshenStaleTime=true: despawnTimestamp = %v, want bumped past %d (editor-preview affordance)", got, wayInThePast)
	}
}

func TestEnrichRaid_FreshenStaleTimeFlag(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := json.RawMessage(`{"gym_id":"g1","pokemon_id":150,"latitude":51.5,"longitude":-0.1,"level":5,"start":999000,"end":1000000}`)

	live, err := ps.enrichRaid(raw, "en", false, false)
	if err != nil {
		t.Fatalf("enrichRaid error: %v", err)
	}
	if got, _ := live.base["end"].(int64); got != wayInThePast {
		t.Errorf("freshenStaleTime=false: end = %v, want unchanged %d", live.base["end"], wayInThePast)
	}

	editor, err := ps.enrichRaid(raw, "en", false, true)
	if err != nil {
		t.Fatalf("enrichRaid error: %v", err)
	}
	got, _ := editor.base["end"].(int64)
	if got <= wayInThePast {
		t.Errorf("freshenStaleTime=true: end = %v, want bumped past %d", got, wayInThePast)
	}
}

func TestEnrichInvasion_FreshenStaleTimeFlag(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := json.RawMessage(`{"pokestop_id":"p1","latitude":51.5,"longitude":-0.1,"incident_expiration":1000000,"grunt_type":1,"display_type":7}`)

	live, err := ps.enrichInvasion(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichInvasion error: %v", err)
	}
	if got, _ := live.base["expiration"].(int64); got != wayInThePast {
		t.Errorf("freshenStaleTime=false: expiration = %v, want unchanged %d", live.base["expiration"], wayInThePast)
	}

	editor, err := ps.enrichInvasion(raw, "en", true)
	if err != nil {
		t.Fatalf("enrichInvasion error: %v", err)
	}
	got, _ := editor.base["expiration"].(int64)
	if got <= wayInThePast {
		t.Errorf("freshenStaleTime=true: expiration = %v, want bumped past %d", got, wayInThePast)
	}
}
