package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestWriteConfigTOML_NonDefaultPersists — a value differing from the
// schema default is written into config.toml.
func TestWriteConfigTOML_NonDefaultPersists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	updates := map[string]any{
		"general": map[string]any{
			"locale": "de", // schema default is "en"
		},
	}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}

	parsed := readTOML(t, filepath.Join(dir, "config.toml"))
	general, _ := parsed["general"].(map[string]any)
	if general == nil || general["locale"] != "de" {
		t.Errorf("expected general.locale = \"de\"; got %v", general)
	}
}

// TestWriteConfigTOML_DefaultElided — a value matching the schema
// default is removed (elided) rather than written explicitly. This is
// the npm/Cargo convention; verifies our default-elision contract.
func TestWriteConfigTOML_DefaultElided(t *testing.T) {
	dir := t.TempDir()
	// Seed config.toml with a value matching the default; the editor
	// then saves the same value again. After write, the key must be
	// gone from disk.
	seed := "[general]\nlocale = \"en\"\nrole_check_mode = \"ignore\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(seed), 0644); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	updates := map[string]any{
		"general": map[string]any{
			"locale":          "en",     // schema default — should be elided
			"role_check_mode": "ignore", // schema default — should be elided
		},
	}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}

	parsed := readTOML(t, filepath.Join(dir, "config.toml"))
	general, _ := parsed["general"].(map[string]any)
	if _, present := general["locale"]; present {
		t.Errorf("default-value locale should be elided; got %v", general["locale"])
	}
	if _, present := general["role_check_mode"]; present {
		t.Errorf("default-value role_check_mode should be elided; got %v", general["role_check_mode"])
	}
}

// TestWriteConfigTOML_NonSchemaPassthrough — fields not in the schema
// (database password, tokens) are kept as-is from the existing file
// when not touched by the update.
func TestWriteConfigTOML_NonSchemaPassthrough(t *testing.T) {
	dir := t.TempDir()
	seed := `[database]
password = "secret123"
host = "db.example.com"

[discord]
token = ["bot-token"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(seed), 0644); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	updates := map[string]any{
		"general": map[string]any{
			"locale": "de",
		},
	}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}

	parsed := readTOML(t, filepath.Join(dir, "config.toml"))
	db, _ := parsed["database"].(map[string]any)
	if db == nil || db["password"] != "secret123" {
		t.Errorf("database.password lost on save; got %v", db)
	}
	if db["host"] != "db.example.com" {
		t.Errorf("database.host lost on save; got %v", db)
	}
	discord, _ := parsed["discord"].(map[string]any)
	tokens, _ := discord["token"].([]any)
	if len(tokens) != 1 || tokens[0] != "bot-token" {
		t.Errorf("discord.token lost on save; got %v", tokens)
	}
}

// TestWriteConfigTOML_BackupCreated — every save leaves a copy of the
// previous file in config/backups/. Operators with elaborate
// hand-authored configs need this for recovery.
func TestWriteConfigTOML_BackupCreated(t *testing.T) {
	dir := t.TempDir()
	seed := "# important comment\n[general]\nlocale = \"en\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(seed), 0644); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	backupRel, err := writeConfigTOML(dir, map[string]any{
		"general": map[string]any{"locale": "de"},
	})
	if err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}
	if backupRel == "" {
		t.Fatal("expected backupRel path, got empty string")
	}

	backupPath := filepath.Join(dir, backupRel)
	backupBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup %s: %v", backupPath, err)
	}
	if !strings.Contains(string(backupBytes), "important comment") {
		t.Errorf("backup should contain original content including comments; got:\n%s", backupBytes)
	}
}

// TestWriteConfigTOML_IntCoercedRoundTrip — editor POSTs an integer
// value; JSON unmarshal makes it float64; the writer must coerce it
// back to int so the TOML file uses an integer literal and the next
// config.Load can unmarshal into an `int` field.
//
// Same regression as the load-time migration path, different entry
// point. Pinned here so a future writer refactor can't skip the
// normalisation step.
func TestWriteConfigTOML_IntCoercedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Editor sends an int — JSON unmarshal makes it float64.
	updates := map[string]any{
		"pvp": map[string]any{
			"filter_max_rank": float64(100),
		},
	}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}

	var dst struct {
		PVP struct {
			FilterMaxRank int `toml:"filter_max_rank"`
		} `toml:"pvp"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "config.toml"), &dst); err != nil {
		t.Fatalf("decode into int field: %v", err)
	}
	if dst.PVP.FilterMaxRank != 100 {
		t.Errorf("got %d, want 100", dst.PVP.FilterMaxRank)
	}
}

// TestMigrateLegacyLoggingKeys_ExplicitLevelWins — when an explicit `level`
// is set, it survives and both legacy keys are dropped (they were inert).
func TestMigrateLegacyLoggingKeys_ExplicitLevelWins(t *testing.T) {
	raw := map[string]any{"logging": map[string]any{
		"level": "debug", "log_level": "info", "console_log_level": "info",
	}}
	migrateLegacyLoggingKeys(raw)
	logging := raw["logging"].(map[string]any)
	if logging["level"] != "debug" {
		t.Errorf("level = %v, want debug", logging["level"])
	}
	if _, ok := logging["log_level"]; ok {
		t.Error("log_level should be removed")
	}
	if _, ok := logging["console_log_level"]; ok {
		t.Error("console_log_level should be removed")
	}
}

// TestMigrateLegacyLoggingKeys_FallbackWhenLevelEmpty — when `level` is absent,
// the value migrates from log_level (matching the load-time precedence) so the
// setting is not lost.
func TestMigrateLegacyLoggingKeys_FallbackWhenLevelEmpty(t *testing.T) {
	raw := map[string]any{"logging": map[string]any{"log_level": "warn"}}
	migrateLegacyLoggingKeys(raw)
	logging := raw["logging"].(map[string]any)
	if logging["level"] != "warn" {
		t.Errorf("level = %v, want warn (migrated from log_level)", logging["level"])
	}
	if _, ok := logging["log_level"]; ok {
		t.Error("log_level should be removed")
	}
}

// TestMigrateLegacyLoggingKeys_ConsoleFallback — console_log_level is the last
// resort when neither level nor log_level is set.
func TestMigrateLegacyLoggingKeys_ConsoleFallback(t *testing.T) {
	raw := map[string]any{"logging": map[string]any{"console_log_level": "warn"}}
	migrateLegacyLoggingKeys(raw)
	logging := raw["logging"].(map[string]any)
	if logging["level"] != "warn" {
		t.Errorf("level = %v, want warn (migrated from console_log_level)", logging["level"])
	}
}

// TestMigrateLegacyLoggingKeys_NoLegacyNoOp — a section with no legacy keys is
// left untouched.
func TestMigrateLegacyLoggingKeys_NoLegacyNoOp(t *testing.T) {
	raw := map[string]any{"logging": map[string]any{"level": "debug"}}
	migrateLegacyLoggingKeys(raw)
	if raw["logging"].(map[string]any)["level"] != "debug" {
		t.Errorf("level should be unchanged")
	}
}

// TestWriteConfigTOML_StripsLegacyLoggingKeys — the exact three-key file the
// web editor produced: after any save the legacy shadow keys are gone and the
// effective level (`level`) is the single source of truth.
func TestWriteConfigTOML_StripsLegacyLoggingKeys(t *testing.T) {
	dir := t.TempDir()
	seed := "[logging]\nconsole_log_level = \"info\"\nlevel = \"debug\"\nlog_level = \"info\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(seed), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	updates := map[string]any{"logging": map[string]any{"level": "debug"}}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}
	logging, _ := readTOML(t, filepath.Join(dir, "config.toml"))["logging"].(map[string]any)
	if logging["level"] != "debug" {
		t.Errorf("level = %v, want debug", logging["level"])
	}
	if _, ok := logging["log_level"]; ok {
		t.Error("log_level should be stripped from disk")
	}
	if _, ok := logging["console_log_level"]; ok {
		t.Error("console_log_level should be stripped from disk")
	}
}

// TestWriteConfigTOML_MigratesLegacyOnUnrelatedSave — saving an unrelated
// section must still clean up legacy logging keys without dropping the level
// they carried (partial-save safety).
func TestWriteConfigTOML_MigratesLegacyOnUnrelatedSave(t *testing.T) {
	dir := t.TempDir()
	seed := "[logging]\nlog_level = \"warn\"\n\n[general]\nlocale = \"en\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(seed), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	updates := map[string]any{"general": map[string]any{"locale": "de"}}
	if _, err := writeConfigTOML(dir, updates); err != nil {
		t.Fatalf("writeConfigTOML: %v", err)
	}
	logging, _ := readTOML(t, filepath.Join(dir, "config.toml"))["logging"].(map[string]any)
	if logging == nil || logging["level"] != "warn" {
		t.Errorf("level = %v, want warn (migrated, not lost)", logging)
	}
	if _, ok := logging["log_level"]; ok {
		t.Error("log_level should be stripped even on an unrelated save")
	}
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse %s: %v\ncontent:\n%s", path, err, data)
	}
	return parsed
}
