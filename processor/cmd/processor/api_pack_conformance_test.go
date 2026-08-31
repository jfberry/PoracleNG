package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestAPIPackConformance is the REAL-enrichment conformance guard for the
// Diadem api pack (fallbacks/dts/diadem.toml, id="diadem"). For every
// documented entry it runs a bundled fallbacks/testdata.json fixture
// through the exact same enrich* functions the live processor uses (via
// ps.enrichForType — see enrich.go), renders it through a real
// dts.Renderer loaded from fallbacks/, and asserts the payload is (a)
// valid JSON and (b) has its documented required fields populated —
// non-empty, not just present.
//
// Why non-empty and not just present: a hand-built enrichment map (the
// kind a lighter-weight test would construct) can only prove the template
// parses — it's written to match the template's field names by
// construction, so it can never catch a template referencing a field name
// enrichment doesn't actually populate. That exact defect class shipped in
// the pack's first draft (Task 2 review: maxbattle's pokestop_name/
// quick_move/charge_move, fort-update's change_type, and rsvps[] on
// raid/egg/rsvpChanges all rendered empty because the template referenced
// a field name enrichment never sets) and was fixed in e8c4e28f. Rendering
// against REAL enrichment output is the only way to catch a recurrence.
func TestAPIPackConformance(t *testing.T) {
	ps := newEnrichParityService(t)

	r, err := dts.NewRenderer(dts.RendererConfig{
		ConfigDir:     t.TempDir(),
		FallbackDir:   filepath.Join(repoRoot(), "fallbacks"), // loads fallbacks/dts/diadem.toml
		DefaultLocale: "en",
	})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	// The api destination user every diadem entry renders for.
	user := webhook.MatchedUser{ID: "u-1", Type: "api:user", Template: "diadem", Language: "en"}

	cases := []struct {
		dtsType          string
		webhookType      string
		testID           string
		requiredNonEmpty []string
	}{
		{"monster", "pokemon", "hundo", []string{"name", "pokemon_id"}},
		{"monsterChanged", "monster_changed", "ditto-reveal", []string{"name", "previous.pokemon_id"}},
		{"raid", "raid", "level1", []string{"level_name", "boss.name", "gym.name", "end_at"}},
		{"egg", "raid", "egg1", []string{"level_name", "gym.name"}},
		{"rsvpChanges", "rsvp_changes", "level5", []string{"rsvps"}},
		{"quest", "quest", "quest-item", []string{"quest", "reward", "pokestop_name"}},
		{"questSummary", "quest_summary", "stardust", []string{"reward_name", "entries"}},
		{"invasion", "pokestop", "invasion", []string{"grunt_type_name", "pokestop_name"}},
		{"incident", "incident", "goldstop", []string{"incident_type_name", "pokestop_name"}},
		{"showcase", "showcase", "type", []string{"pokestop_name", "showcase.present"}},
		{"lure", "pokestop", "lure", []string{"lure_type_name", "pokestop_name"}},
		{"gym", "gym", "teamchange", []string{"gym_name", "team"}},
		{"fort-update", "fort_update", "edit", []string{"change_type", "fort_type"}},
		{"maxbattle", "max_battle", "level1", []string{"pokestop_name", "name", "quick_move"}},
		{"weatherchange", "weatherchange", "rain", []string{"weather"}},
		{"nest", "nest", "park", []string{"nest_name", "name", "pokemon_id", "spawn_avg"}},
	}

	for _, tc := range cases {
		t.Run(tc.dtsType, func(t *testing.T) {
			raw := loadTestdataSample(t, tc.webhookType, tc.testID)

			var jobs []webhook.DeliveryJob

			switch tc.dtsType {
			case "monster":
				res, err := ps.enrichForType(tc.dtsType, raw, "en", true)
				if err != nil {
					t.Fatalf("enrichForType(%q): %v", tc.dtsType, err)
				}
				jobs = r.RenderPokemon(res.base, map[string]map[string]any{"en": res.perLang}, perUserMapFor(user.ID, res.perUser), res.webhookFields, []webhook.MatchedUser{user}, nil, true, "ref", "")
			case "monsterChanged":
				res, err := ps.enrichForType(tc.dtsType, raw, "en", true)
				if err != nil {
					t.Fatalf("enrichForType(%q): %v", tc.dtsType, err)
				}
				originalView, _ := res.extras["original"].(map[string]any)
				jobs = r.RenderPokemonChanged(res.base, map[string]map[string]any{"en": res.perLang}, perUserMapFor(user.ID, res.perUser), res.webhookFields, originalView, []webhook.MatchedUser{user}, nil, "ref", "")
			default:
				res, err := ps.enrichForType(tc.dtsType, raw, "en", true)
				if err != nil {
					t.Fatalf("enrichForType(%q): %v", tc.dtsType, err)
				}
				jobs = r.RenderAlert(res.templateType, res.base, map[string]map[string]any{"en": res.perLang}, res.webhookFields, []webhook.MatchedUser{user}, nil, "ref", "")
			}

			if len(jobs) != 1 {
				t.Fatalf("got %d delivery jobs, want exactly 1", len(jobs))
			}
			payload := jobs[0].Message

			if !json.Valid(payload) {
				t.Fatalf("payload is not valid JSON: %s", payload)
			}

			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				t.Fatalf("unmarshal payload: %v\npayload: %s", err, payload)
			}

			for _, key := range tc.requiredNonEmpty {
				assertNonEmpty(t, m, key, payload)
			}
		})
	}

}

// perUserMapFor wraps a flat per-user enrichment map (enrichResult.perUser)
// under the given user ID for RenderPokemon/RenderPokemonChanged, whose
// perUserEnrichment parameter is keyed by user ID
// (map[string]map[string]any). enrichPokemon never actually sets perUser
// (see TestEnrichPokemon_DoesNotComputePerUser in enrich_test.go — it's the
// caller's job, and this test has no PVPDisplay wired), so this is nil
// today; it's here so the shape is correct if that ever changes.
func perUserMapFor(userID string, perUser map[string]any) map[string]map[string]any {
	if perUser == nil {
		return nil
	}
	return map[string]map[string]any{userID: perUser}
}

// apiPackNavigate walks a dotted path ("boss.name", "showcase.present")
// through the nested map[string]any tree produced by unmarshalling a
// rendered JSON payload. Returns (nil, false) if any segment is missing or
// the path passes through a non-object value.
func apiPackNavigate(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := cm[part]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// assertNonEmpty asserts that the dotted-path key resolves to a non-empty
// value in the decoded payload — this is the assertion that actually
// catches the empty-field-from-wrong-name defect class (see the test's
// doc comment): a field that renders as "" / 0 / [] / null still passes
// json.Valid, so presence alone isn't enough.
//
//   - string:        non-""
//   - float64:       > 0 (id/count numerics; json.Unmarshal decodes all
//     JSON numbers into float64 for map[string]any)
//   - bool:          must be true (only used for showcase.present, where
//     the fixture is known to have an active showcase)
//   - []any:         non-empty array
//   - map[string]any: non-empty object
//   - nil / missing:  always fails
func assertNonEmpty(t *testing.T, m map[string]any, path string, payload json.RawMessage) {
	t.Helper()
	v, ok := apiPackNavigate(m, path)
	if !ok {
		t.Errorf("required key %q is missing from payload: %s", path, payload)
		return
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			t.Errorf("required key %q is an empty string", path)
		}
	case float64:
		if val <= 0 {
			t.Errorf("required key %q is <= 0 (%v)", path, val)
		}
	case bool:
		if !val {
			t.Errorf("required key %q is false, want true", path)
		}
	case []any:
		if len(val) == 0 {
			t.Errorf("required key %q is an empty array", path)
		}
	case map[string]any:
		if len(val) == 0 {
			t.Errorf("required key %q is an empty object", path)
		}
	case nil:
		t.Errorf("required key %q is null", path)
	default:
		t.Errorf("required key %q has unexpected decoded type %T (%v)", path, val, val)
	}
}
