package api

import "testing"

// TestResolveTestWireType covers POST /api/test's type-name resolution: a
// caller may pass a DTS template-type name (e.g. "monster", "monsterChanged",
// "maxbattle"), a raw webhook wire type (e.g. "pokemon", "monster_changed"),
// or the CLI-display hyphenated spelling (e.g. "fort-update", "max-battle")
// interchangeably — mirroring !poracle-test's resolveHookType
// (internal/bot/commands/poracletest.go) case-insensitively, since resolveTestWireType
// feeds ProcessTest's switch (cmd/processor/test.go) which only recognizes
// the underscored/plain wire spellings.
func TestResolveTestWireType(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		// DTS template-type names -> wire type.
		{"monster", "pokemon"},
		{"monsterNoIv", "pokemon"},
		{"monsterChanged", "monster_changed"},
		{"maxbattle", "max_battle"},
		{"egg", "raid"},
		{"rsvpChanges", "rsvp_changes"},
		{"questSummary", "quest_summary"},

		// Case-insensitive DTS names (JSON callers won't always match the
		// bot parser's lowercased convention).
		{"MonsterChanged", "monster_changed"},
		{"MONSTER", "pokemon"},

		// Raw wire types resolve to themselves (idempotent) — a caller who
		// already knows the wire spelling must not be broken by this change.
		{"pokemon", "pokemon"},
		{"raid", "raid"},
		{"monster_changed", "monster_changed"},
		{"max_battle", "max_battle"},
		{"fort_update", "fort_update"},
		{"quest_summary", "quest_summary"},
		{"rsvp_changes", "rsvp_changes"},
		{"weatherchange", "weatherchange"},
		{"incident", "incident"},
		{"showcase", "showcase"},
		{"gym", "gym"},
		{"nest", "nest"},
		{"quest", "quest"},

		// CLI-display hyphenated forms.
		{"fort-update", "fort_update"},
		{"max-battle", "max_battle"},
		{"monster-changed", "monster_changed"},
		{"rsvp-changes", "rsvp_changes"},
		{"quest-summary", "quest_summary"},
		{"weather-change", "weatherchange"},

		// "pokestop" has no dtsmap identity entry by design (ambiguous
		// without peeking the payload — see dtsmap's doc comment), but it's
		// a valid raw wire type ProcessTest's switch dispatches on directly
		// (case "pokestop": peeks lure_expiration) and must keep resolving
		// to itself.
		{"pokestop", "pokestop"},
		{"POKESTOP", "pokestop"},

		// invasion/lure: unaffected by this fix. Both keep resolving to
		// their OWN token rather than collapsing to "pokestop" the way
		// resolveHookType's testdata-bucket lookup needs (see
		// resolveTestWireType's doc comment for why the two callers'
		// pokestop-guards point in opposite directions) — ProcessTest's
		// switch already has a direct "invasion" case, and rewriting "lure"
		// to "pokestop" would be a behavior CHANGE (today "lure" isn't a
		// case ProcessTest recognizes at all), which is out of scope here.
		{"invasion", "invasion"},
		{"lure", "lure"},

		// Unknown types pass through unchanged so ProcessTest's own
		// "unsupported test webhook type: %s" error still fires with the
		// caller's original string — resolution must never mask an unknown
		// type behind a different error shape.
		{"totally-bogus-type", "totally-bogus-type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTestWireType(tc.name); got != tc.want {
				t.Errorf("resolveTestWireType(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
