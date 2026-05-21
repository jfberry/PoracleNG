package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestMigrate_NoOverridesFile — steady-state install. Function returns
// nil, the on-disk files are untouched.
func TestMigrate_NoOverridesFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := "[discord]\ntoken = [\"abc\"]\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	if err := MigrateOverridesIntoTOML(dir); err != nil {
		t.Fatalf("MigrateOverridesIntoTOML: %v", err)
	}

	got, _ := os.ReadFile(configPath)
	if string(got) != original {
		t.Errorf("config.toml mutated unexpectedly:\nbefore: %q\nafter:  %q", original, got)
	}
}

// TestMigrate_FoldsOverrides — overrides.json present + config.toml
// merges and the overrides file is removed after.
func TestMigrate_FoldsOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	overridesPath := filepath.Join(dir, "overrides.json")

	if err := os.WriteFile(configPath, []byte("[discord]\nprefix = \"!\"\n"), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	overrides := map[string]any{
		"discord": map[string]any{
			"admins": []any{"12345"},
		},
		"general": map[string]any{
			"locale": "de",
		},
	}
	ovBytes, _ := json.Marshal(overrides)
	if err := os.WriteFile(overridesPath, ovBytes, 0644); err != nil {
		t.Fatalf("write overrides.json: %v", err)
	}

	if err := MigrateOverridesIntoTOML(dir); err != nil {
		t.Fatalf("MigrateOverridesIntoTOML: %v", err)
	}

	// overrides.json must be gone.
	if _, err := os.Stat(overridesPath); !os.IsNotExist(err) {
		t.Errorf("overrides.json should be removed; stat err = %v", err)
	}

	// config.toml must contain the merged values from both files.
	merged, _ := os.ReadFile(configPath)
	var parsed map[string]any
	if err := toml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("merged config.toml does not parse: %v\ncontent:\n%s", err, merged)
	}
	discord, _ := parsed["discord"].(map[string]any)
	if discord == nil {
		t.Fatalf("merged config.toml missing [discord]: %v", parsed)
	}
	if discord["prefix"] != "!" {
		t.Errorf("original discord.prefix lost: got %v", discord["prefix"])
	}
	admins, _ := discord["admins"].([]any)
	if len(admins) != 1 || admins[0] != "12345" {
		t.Errorf("override discord.admins not merged: got %v", admins)
	}
	general, _ := parsed["general"].(map[string]any)
	if general == nil || general["locale"] != "de" {
		t.Errorf("override general.locale not merged: got %v", general)
	}

	// A backup of the pre-merge config.toml was left in
	// config/backups/. We don't pin the exact filename (it's a
	// timestamp) — just verify some backup file exists.
	backupsDir := filepath.Join(dir, "backups")
	entries, _ := os.ReadDir(backupsDir)
	if len(entries) == 0 {
		t.Errorf("expected a backup file under %s; directory empty or missing", backupsDir)
	}
}

// TestMigrate_DottedTopLevelKeys — overrides.json with dotted top-level
// keys like "reconciliation.discord" must expand into nested sections
// in the merged TOML rather than literal keys with dots.
func TestMigrate_DottedTopLevelKeys(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	overridesPath := filepath.Join(dir, "overrides.json")

	if err := os.WriteFile(configPath, []byte("[discord]\n"), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	ov := map[string]any{
		"reconciliation.discord": map[string]any{
			"check_role_interval": 24,
		},
	}
	ovBytes, _ := json.Marshal(ov)
	if err := os.WriteFile(overridesPath, ovBytes, 0644); err != nil {
		t.Fatalf("write overrides.json: %v", err)
	}

	if err := MigrateOverridesIntoTOML(dir); err != nil {
		t.Fatalf("MigrateOverridesIntoTOML: %v", err)
	}

	merged, _ := os.ReadFile(configPath)
	var parsed map[string]any
	if err := toml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("merged TOML doesn't parse: %v\ncontent:\n%s", err, merged)
	}
	recon, _ := parsed["reconciliation"].(map[string]any)
	if recon == nil {
		t.Fatalf("[reconciliation] section missing; got:\n%s", merged)
	}
	disc, _ := recon["discord"].(map[string]any)
	if disc == nil {
		t.Fatalf("[reconciliation.discord] not present in merged TOML:\n%s", merged)
	}
	if int(toFloat(disc["check_role_interval"])) != 24 {
		t.Errorf("check_role_interval merge missed: got %v", disc["check_role_interval"])
	}
}

// TestMigrate_NumericOverridesRoundTrip — overrides.json contains an
// integer that JSON unmarshal decodes as float64. The migration must
// coerce it back to int64 so BurntSushi writes "100" (TOML int) rather
// than "100.0" (TOML float). Otherwise the next config.Load fails to
// unmarshal into an `int` field.
//
// Reproduces the fatal-startup case operators hit on first upgrade
// when overrides.json contained any integer value.
func TestMigrate_NumericOverridesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	overridesPath := filepath.Join(dir, "overrides.json")

	if err := os.WriteFile(configPath, []byte("[pvp]\n"), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	ov := map[string]any{
		"pvp": map[string]any{
			"filter_max_rank": 100,
		},
	}
	ovBytes, _ := json.Marshal(ov)
	if err := os.WriteFile(overridesPath, ovBytes, 0644); err != nil {
		t.Fatalf("write overrides.json: %v", err)
	}

	if err := MigrateOverridesIntoTOML(dir); err != nil {
		t.Fatalf("MigrateOverridesIntoTOML: %v", err)
	}

	// Reload the merged file into a struct whose target field is int —
	// this is the real failure mode and the only honest pin against it.
	var dst struct {
		PVP struct {
			FilterMaxRank int `toml:"filter_max_rank"`
		} `toml:"pvp"`
	}
	if _, err := toml.DecodeFile(configPath, &dst); err != nil {
		t.Fatalf("decode merged config.toml into int field: %v", err)
	}
	if dst.PVP.FilterMaxRank != 100 {
		t.Errorf("got filter_max_rank = %d, want 100", dst.PVP.FilterMaxRank)
	}
}

// TestNormalizeNumericValues — pins the coercion contract directly.
func TestNormalizeNumericValues(t *testing.T) {
	in := map[string]any{
		"whole":      float64(100),
		"fractional": float64(1.5),
		"nested": map[string]any{
			"whole_negative": float64(-42),
			"array":          []any{float64(1), float64(2.5), float64(3)},
		},
	}
	out := NormalizeNumericValues(in).(map[string]any)

	if got := out["whole"]; got != int64(100) {
		t.Errorf("whole: got %v (%T), want int64(100)", got, got)
	}
	if got := out["fractional"]; got != float64(1.5) {
		t.Errorf("fractional: got %v (%T), want float64(1.5)", got, got)
	}
	nested := out["nested"].(map[string]any)
	if got := nested["whole_negative"]; got != int64(-42) {
		t.Errorf("whole_negative: got %v (%T), want int64(-42)", got, got)
	}
	arr := nested["array"].([]any)
	if arr[0] != int64(1) || arr[1] != float64(2.5) || arr[2] != int64(3) {
		t.Errorf("array: got %v, want [int64(1), float64(2.5), int64(3)]", arr)
	}
}

// toFloat coerces a TOML-decoded numeric value (int64 or float64) to a
// float64. Tiny helper for the test — production code already has its
// own coercions in the overrides path.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
