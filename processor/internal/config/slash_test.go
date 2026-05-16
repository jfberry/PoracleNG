package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFromReader is a test-only helper that loads config from a TOML string.
// It writes the provided TOML to a temp config/config.toml under a fresh
// baseDir (prepending a minimal [database] block so Load()'s required-field
// validation passes) and calls the public Load() entry point. This keeps the
// production API surface unchanged.
func loadFromReader(t *testing.T, raw string) (*Config, error) {
	t.Helper()
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "config"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Strip any leading whitespace/newline so the [database] preamble lands as
	// its own top-level block. The supplied TOML may begin with `[discord...`.
	body := strings.TrimLeft(raw, " \t\r\n")
	full := "[database]\nuser = \"u\"\ndatabase = \"d\"\n\n" + body
	path := filepath.Join(baseDir, "config", "config.toml")
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	return Load(baseDir)
}

func TestParseSlashCommandsConfig(t *testing.T) {
	raw := `
[discord.slash_commands]
enabled = true
register_globally = false
guilds = ["111", "222"]
sync_on_startup = true
enable = ["track", "raid", "tracked", "version"]
`
	cfg, err := loadFromReader(t, raw)
	if err != nil {
		t.Fatal(err)
	}

	sc := cfg.Discord.SlashCommands
	if !sc.Enabled {
		t.Error("Enabled should be true")
	}
	if sc.RegisterGlobally {
		t.Error("RegisterGlobally should be false")
	}
	if len(sc.Guilds) != 2 {
		t.Errorf("Guilds: %v", sc.Guilds)
	}
	if len(sc.Enable) != 4 {
		t.Errorf("Enable: %v", sc.Enable)
	}
}

func TestSlashCommandsDefaults(t *testing.T) {
	cfg, err := loadFromReader(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sc := cfg.Discord.SlashCommands
	if sc.Enabled {
		t.Error("should default disabled (master switch off)")
	}
	if !sc.RegisterGlobally {
		t.Error("should default global")
	}
	if !sc.SyncOnStartup {
		t.Error("should default sync on")
	}
	if len(sc.Enable) != 0 {
		t.Error("Enable should default to empty (meaning all)")
	}
}

func TestIsSlashCommandEnabledEmptyMeansAll(t *testing.T) {
	sc := DiscordSlashCommands{} // empty Enable
	if !sc.IsEnabled("track") {
		t.Error("empty Enable should enable everything")
	}
	if !sc.IsEnabled("gym") {
		t.Error("empty Enable should enable everything")
	}
}

func TestIsSlashCommandEnabledExplicitSubset(t *testing.T) {
	sc := DiscordSlashCommands{Enable: []string{"track", "raid"}}
	if !sc.IsEnabled("track") {
		t.Error("track should be enabled")
	}
	if sc.IsEnabled("gym") {
		t.Error("gym should not be enabled when subset restricts")
	}
}
