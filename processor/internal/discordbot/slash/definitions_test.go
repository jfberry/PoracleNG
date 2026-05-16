package slash

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// bundleOpt mutates the per-language message maps used by testBundle.
type bundleOpt func(map[string]map[string]string)

// withOverride lets a test simulate an operator override
// (e.g. config/custom.en.json renaming a slash command).
func withOverride(lang, key, val string) bundleOpt {
	return func(langs map[string]map[string]string) {
		m, ok := langs[lang]
		if !ok {
			m = map[string]string{}
			langs[lang] = m
		}
		m[key] = val
	}
}

// testBundle builds a minimal i18n.Bundle pre-populated with the slash.*
// keys needed by definition tests. Opts can extend or override entries.
func testBundle(t *testing.T, opts ...bundleOpt) *i18n.Bundle {
	t.Helper()

	langs := map[string]map[string]string{
		"en": {
			"slash.cmd.version":    "version",
			"slash.desc.version":   "Show Poracle version",
			"slash.cmd.tracked":    "tracked",
			"slash.desc.tracked":   "List your tracking rules",
			"slash.cmd.help":       "help",
			"slash.desc.help":      "Show help",
			"slash.cmd.info":       "info",
			"slash.desc.info":      "Show your bot registration info",
			"slash.cmd.language":   "language",
			"slash.desc.language":  "Show or set your language",
			"slash.cmd.track":      "track",
			"slash.desc.track":     "Track a Pokemon",
			"slash.cmd.raid":       "raid",
			"slash.desc.raid":      "Track a raid boss or raid level",
			"slash.cmd.egg":        "egg",
			"slash.desc.egg":       "Track an egg / raid level",
			"slash.cmd.quest":      "quest",
			"slash.desc.quest":     "Track a quest reward",
			"slash.cmd.invasion":   "invasion",
			"slash.desc.invasion":  "Track a Team Rocket invasion",
			"slash.cmd.lure":       "lure",
			"slash.desc.lure":      "Track a pokestop lure",
			"slash.cmd.nest":       "nest",
			"slash.desc.nest":      "Track a nesting pokemon",
			"slash.cmd.maxbattle":  "maxbattle",
			"slash.desc.maxbattle": "Track a max (Dynamax) battle",
			"slash.cmd.gym":        "gym",
			"slash.desc.gym":       "Track gym team / slot / battle changes",
			"slash.cmd.fort":       "fort",
			"slash.desc.fort":      "Track pokestop or gym updates",
			"slash.cmd.untrack":    "untrack",
			"slash.desc.untrack":   "Remove a tracking rule",
			"slash.cmd.area":       "area",
			"slash.desc.area":      "Manage your areas",
			"slash.cmd.profile":    "profile",
			"slash.desc.profile":   "Manage your profiles",
			"slash.cmd.location":   "location",
			"slash.desc.location":  "Set your location",
			"slash.cmd.summary":    "summary",
			"slash.desc.summary":   "Manage scheduled summary digests (e.g. quest)",
		},
	}
	for _, opt := range opts {
		opt(langs)
	}

	b := i18n.NewBundle()
	for lang, msgs := range langs {
		b.AddTranslator(i18n.NewTranslator(lang, msgs))
	}
	b.LinkFallbacks()
	return b
}

func TestVersionDefinition(t *testing.T) {
	bundle := testBundle(t)
	def := buildDefinition(bundle, "cmd.version", "version", nil)
	if def.Name != "version" {
		t.Errorf("name=%q", def.Name)
	}
	if len(def.Options) != 0 {
		t.Errorf("expected 0 options, got %d", len(def.Options))
	}
}

func TestVersionDefinitionRenamedByI18n(t *testing.T) {
	// Operator override (config/custom.en.json) renames /version → /poracle-version
	bundle := testBundle(t, withOverride("en", "slash.cmd.version", "poracle-version"))
	def := buildDefinition(bundle, "cmd.version", "version", nil)
	if def.Name != "poracle-version" {
		t.Errorf("expected renamed, got %q", def.Name)
	}
}

func TestSnapshotVersion(t *testing.T) {
	bundle := testBundle(t)
	def := buildDefinition(bundle, "cmd.version", "version", nil)
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/version.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestAllDefinitionsFiltersByEnable(t *testing.T) {
	bundle := testBundle(t)
	// Phase 1 + Phase 2 read-only commands implemented in buildCommandDef.
	// Empty enable list → return everything we can actually build.
	defs := AllDefinitions(bundle, nil)
	gotNames := map[string]bool{}
	for _, d := range defs {
		gotNames[d.Name] = true
	}
	for _, want := range []string{"version", "tracked", "help", "info", "language"} {
		if !gotNames[want] {
			t.Errorf("expected def for %q, got %v", want, gotNames)
		}
	}

	// Phase 4 + Phase 5 mutating commands now fully implemented; the only
	// remaining "not implemented" commands are anything outside the keyset
	// in allCommandKeys() (e.g. a typo in operator config).
	defs = AllDefinitions(bundle, []string{"not-a-real-command"})
	if len(defs) != 0 {
		t.Errorf("expected 0 defs when only unimplemented commands enabled, got %d", len(defs))
	}

	// Phase 4 mutating commands now fully implemented.
	defs = AllDefinitions(bundle, []string{
		"track", "raid", "egg",
		"quest", "invasion", "lure", "nest", "maxbattle", "gym", "fort",
	})
	if len(defs) != 10 {
		t.Fatalf("expected 10 defs for full Phase 4 set, got %d", len(defs))
	}

	// Phase 5 commands all implemented.
	defs = AllDefinitions(bundle, []string{"untrack", "area", "profile", "location"})
	if len(defs) != 4 {
		t.Fatalf("expected 4 defs for full Phase 5 set, got %d", len(defs))
	}

	// Explicit enable that includes "version" → returns just it.
	defs = AllDefinitions(bundle, []string{"version"})
	if len(defs) != 1 || defs[0].Name != "version" {
		t.Errorf("expected version only, got %+v", defs)
	}

	// Explicit enable that includes the new Phase 2 commands.
	defs = AllDefinitions(bundle, []string{"tracked", "help", "info", "language"})
	if len(defs) != 4 {
		t.Fatalf("expected 4 defs, got %d", len(defs))
	}
}

func TestSnapshotTracked(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.tracked", "tracked")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.tracked")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/tracked.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotHelp(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.help", "help")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.help")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/help.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotInfo(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.info", "info")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.info")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/info.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotLanguage(t *testing.T) {
	// Pin the bundle's loaded language set so the snapshot is deterministic.
	bundle := testBundle(t,
		withOverride("de", "_seed", ""),
		withOverride("fr", "_seed", ""),
	)
	def := buildCommandDef(bundle, "cmd.language", "language")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.language")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/language.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestLanguageChoicesSorted(t *testing.T) {
	// Bundle gets locales added in non-sorted order; output must still be sorted.
	bundle := testBundle(t,
		withOverride("zh-cn", "_seed", ""),
		withOverride("de", "_seed", ""),
		withOverride("fr", "_seed", ""),
	)
	choices := languageChoices(bundle)
	if len(choices) < 2 {
		t.Fatalf("expected at least 2 choices, got %d", len(choices))
	}
	prev := ""
	for _, c := range choices {
		if prev != "" && c.Value.(string) < prev {
			t.Errorf("choices not sorted: %q before %q", prev, c.Value)
		}
		prev = c.Value.(string)
	}
}

func TestSnapshotTrack(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.track", "track")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.track")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/track.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotRaid(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.raid", "raid")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.raid")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/raid.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotEgg(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.egg", "egg")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.egg")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/egg.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotQuest(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.quest", "quest")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.quest")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/quest.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotInvasion(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.invasion", "invasion")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.invasion")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/invasion.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotLure(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.lure", "lure")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.lure")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/lure.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotNest(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.nest", "nest")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.nest")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/nest.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotMaxbattle(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.maxbattle", "maxbattle")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.maxbattle")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/maxbattle.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotGym(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.gym", "gym")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.gym")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/gym.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotFort(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.fort", "fort")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.fort")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/fort.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotUntrack(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.untrack", "untrack")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.untrack")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/untrack.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotArea(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.area", "area")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.area")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/area.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotProfile(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.profile", "profile")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.profile")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/profile.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotLocation(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.location", "location")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.location")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/location.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestSnapshotSummary(t *testing.T) {
	bundle := testBundle(t)
	def := buildCommandDef(bundle, "cmd.summary", "summary")
	if def == nil {
		t.Fatal("buildCommandDef returned nil for cmd.summary")
	}
	got, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := os.ReadFile("testdata/summary.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCanonShortName(t *testing.T) {
	cases := map[string]string{
		"cmd.version":  "version",
		"cmd.track":    "track",
		"cmd.untrack":  "untrack",
		"cmd.location": "location",
	}
	for in, want := range cases {
		if got := canonShortName(in); got != want {
			t.Errorf("canonShortName(%q)=%q, want %q", in, got, want)
		}
	}
}
