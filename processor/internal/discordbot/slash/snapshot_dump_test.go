package slash

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestDumpSnapshots writes testdata snapshot files for the slash command
// definitions when POROCLE_DUMP_SNAPSHOTS=1. Skipped during normal test runs.
func TestDumpSnapshots(t *testing.T) {
	if os.Getenv("POROCLE_DUMP_SNAPSHOTS") != "1" {
		t.Skip("set POROCLE_DUMP_SNAPSHOTS=1 to regenerate")
	}
	bundle := testBundle(t,
		withOverride("en", "slash.cmd.track", "track"),
		withOverride("en", "slash.desc.track", "Track a Pokemon"),
		withOverride("en", "slash.cmd.raid", "raid"),
		withOverride("en", "slash.desc.raid", "Track a raid boss or raid level"),
		withOverride("en", "slash.cmd.egg", "egg"),
		withOverride("en", "slash.desc.egg", "Track an egg / raid level"),
		withOverride("en", "slash.cmd.quest", "quest"),
		withOverride("en", "slash.desc.quest", "Track a quest reward"),
		withOverride("en", "slash.cmd.invasion", "invasion"),
		withOverride("en", "slash.desc.invasion", "Track a Team Rocket invasion"),
		withOverride("en", "slash.cmd.incident", "incident"),
		withOverride("en", "slash.desc.incident", "Track a pokestop incident (Kecleon, Gold Pokestop, Showcase…)"),
		withOverride("en", "slash.cmd.lure", "lure"),
		withOverride("en", "slash.desc.lure", "Track a pokestop lure"),
		withOverride("en", "slash.cmd.nest", "nest"),
		withOverride("en", "slash.desc.nest", "Track a nesting pokemon"),
		withOverride("en", "slash.cmd.maxbattle", "maxbattle"),
		withOverride("en", "slash.desc.maxbattle", "Track a max (Dynamax) battle"),
		withOverride("en", "slash.cmd.gym", "gym"),
		withOverride("en", "slash.desc.gym", "Track gym team / slot / battle changes"),
		withOverride("en", "slash.cmd.fort", "fort"),
		withOverride("en", "slash.desc.fort", "Track pokestop or gym updates"),
		withOverride("en", "slash.cmd.untrack", "untrack"),
		withOverride("en", "slash.desc.untrack", "Remove a tracking rule"),
		withOverride("en", "slash.cmd.area", "area"),
		withOverride("en", "slash.desc.area", "Manage your areas"),
		withOverride("en", "slash.cmd.profile", "profile"),
		withOverride("en", "slash.desc.profile", "Manage your profiles"),
		withOverride("en", "slash.cmd.location", "location"),
		withOverride("en", "slash.desc.location", "Set your location"),
		withOverride("en", "slash.cmd.summary", "summary"),
		withOverride("en", "slash.desc.summary", "Manage scheduled summary digests (e.g. quest)"),
	)
	for _, c := range []struct{ key, canon string }{
		{"cmd.track", "track"},
		{"cmd.raid", "raid"},
		{"cmd.egg", "egg"},
		{"cmd.quest", "quest"},
		{"cmd.invasion", "invasion"},
		{"cmd.incident", "incident"},
		{"cmd.lure", "lure"},
		{"cmd.nest", "nest"},
		{"cmd.maxbattle", "maxbattle"},
		{"cmd.gym", "gym"},
		{"cmd.fort", "fort"},
		{"cmd.untrack", "untrack"},
		{"cmd.area", "area"},
		{"cmd.profile", "profile"},
		{"cmd.location", "location"},
		{"cmd.summary", "summary"},
	} {
		def := buildCommandDef(bundle, c.key, c.canon)
		if def == nil {
			t.Fatalf("nil def for %s", c.key)
		}
		got, err := json.MarshalIndent(def, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", c.key, err)
		}
		path := fmt.Sprintf("testdata/%s.json", c.canon)
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s", path)
	}
}
