package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/pokemon/poracleng/processor/internal/backup"
)

// MigrateOverridesIntoTOML folds any existing config/overrides.json into
// config/config.toml as a one-time transition. After this runs:
//
//   - config.toml contains everything the operator had as
//     config.toml + overrides.json semantics
//   - overrides.json is gone
//   - the previous config.toml is preserved in config/backups/
//
// Subsequent saves from the editor write directly to config.toml via the
// api package — there is no overrides.json on the steady-state path.
//
// No-op when overrides.json doesn't exist. Idempotent in the sense that
// running it twice with no overrides.json present is harmless.
//
// Errors are returned to the caller. The bootstrap path in main.go
// treats this as fatal: continuing to start with overrides.json on disk
// after a failed migration would mean the editor's persistence semantics
// silently changed between the documented "config.toml is authoritative"
// and the legacy "overrides.json overlays config.toml" — operators would
// be left guessing why their last save isn't visible in the file.
func MigrateOverridesIntoTOML(configDir string) error {
	overridesPath := filepath.Join(configDir, "overrides.json")
	configPath := filepath.Join(configDir, "config.toml")

	overridesBytes, err := os.ReadFile(overridesPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", overridesPath, err)
	}

	var overrides map[string]any
	if err := json.Unmarshal(overridesBytes, &overrides); err != nil {
		return fmt.Errorf("parse %s: %w", overridesPath, err)
	}

	rawTOML, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}
	var rawMap map[string]any
	if err := toml.Unmarshal(rawTOML, &rawMap); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}

	// Expand dotted top-level keys ("reconciliation.discord") into nested
	// maps so the merge sees them at the right depth.
	overrides = expandDottedKeys(overrides)
	deepMerge(rawMap, overrides)

	backupRel, err := backup.Save(configDir, "config.toml")
	if err != nil {
		return fmt.Errorf("backup config.toml: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("# PoracleNG config — overrides.json merged in on startup.\n")
	buf.WriteString("# Previous config.toml preserved at: ")
	buf.WriteString(backupRel)
	buf.WriteString("\n")
	buf.WriteString("# Comments and key ordering are not preserved across editor saves.\n")
	buf.WriteString("# Hand-author this file directly if you need either; otherwise the\n")
	buf.WriteString("# web config editor (POST /api/config/values) rewrites it on save.\n\n")
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(rawMap); err != nil {
		return fmt.Errorf("encode merged config.toml: %w", err)
	}

	tmpPath := configPath + ".new"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmpPath, configPath, err)
	}

	if err := os.Remove(overridesPath); err != nil {
		// The merge succeeded, the new config.toml is on disk, but we
		// failed to remove the old overrides.json. Next startup would
		// re-merge the same values harmlessly, but the operator would
		// still see two-file behaviour. Surface as a warn-equivalent
		// error so the caller can log it loudly.
		return fmt.Errorf("merged into config.toml but failed to remove %s: %w", overridesPath, err)
	}

	return nil
}
