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
			"slash.cmd.version":  "version",
			"slash.desc.version": "Show Poracle version",
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
	// Only "version" is implemented in buildCommandDef so far.
	// Empty enable list → return everything we can actually build.
	defs := AllDefinitions(bundle, nil)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def (version), got %d", len(defs))
	}
	if defs[0].Name != "version" {
		t.Errorf("expected version, got %q", defs[0].Name)
	}

	// Explicit enable that excludes "version" → empty result.
	defs = AllDefinitions(bundle, []string{"track"})
	if len(defs) != 0 {
		t.Errorf("expected 0 defs when version excluded, got %d", len(defs))
	}

	// Explicit enable that includes "version" → returns it.
	defs = AllDefinitions(bundle, []string{"version"})
	if len(defs) != 1 || defs[0].Name != "version" {
		t.Errorf("expected version, got %+v", defs)
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
