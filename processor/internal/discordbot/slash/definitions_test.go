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
			"slash.cmd.version":   "version",
			"slash.desc.version":  "Show Poracle version",
			"slash.cmd.tracked":   "tracked",
			"slash.desc.tracked":  "List your tracking rules",
			"slash.cmd.help":      "help",
			"slash.desc.help":     "Show help",
			"slash.cmd.info":      "info",
			"slash.desc.info":     "Show your bot registration info",
			"slash.cmd.language":  "language",
			"slash.desc.language": "Show or set your language",
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

	// Explicit enable that excludes "version" → no version in result.
	defs = AllDefinitions(bundle, []string{"track"})
	if len(defs) != 0 {
		t.Errorf("expected 0 defs when only unimplemented commands enabled, got %d", len(defs))
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
