package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/pokemon/poracleng/processor/internal/backup"
)

// NormalizeNumericValues walks a value recursively and coerces every
// whole-number float64 into an int64. JSON unmarshal decodes every
// number — including integer-valued ones — as float64. BurntSushi's
// TOML encoder then preserves that float64-ness on the wire as
// "1500.0", which the next config.Load can't unmarshal back into a Go
// `int` field. Running the merged map through this normalizer before
// encoding keeps the TOML output type-faithful to what the operator
// actually had in mind.
//
// Legitimately fractional float64s pass through unchanged. Genuine
// float64 fields that happen to receive a whole number (e.g. 1.0)
// are coerced to int64 but BurntSushi reads `1` back into a float64
// field gracefully, so the round-trip is safe.
//
// Exported because both the load-time overrides migration and the
// editor save path need it; both write through the BurntSushi encoder.
func NormalizeNumericValues(v any) any {
	switch t := v.(type) {
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return t
		}
		if t == math.Trunc(t) && t >= math.MinInt64 && t <= math.MaxInt64 {
			return int64(t)
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = NormalizeNumericValues(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = NormalizeNumericValues(val)
		}
		return t
	}
	return v
}

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

	// Coerce JSON-decoded whole-number float64 values to int64. Without
	// this, an integer field like pvp.filter_max_rank in overrides.json
	// arrives as float64(100) and BurntSushi writes "100.0" — which the
	// next config.Load can't unmarshal back into the Go int field.
	NormalizeNumericValues(rawMap)

	configBackupRel, err := backup.Save(configDir, "config.toml")
	if err != nil {
		return fmt.Errorf("backup config.toml: %w", err)
	}
	// Back up overrides.json BEFORE doing anything destructive. Even
	// once the merged config.toml is on disk and the in-memory cfg
	// parses cleanly, a future operator might want to audit what was
	// folded in — having both backups under config/backups/ leaves a
	// complete record of the pre-migration state.
	overridesBackupRel, err := backup.Save(configDir, "overrides.json")
	if err != nil {
		return fmt.Errorf("backup overrides.json: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("# PoracleNG config — overrides.json merged in on startup.\n")
	buf.WriteString("# Previous config.toml preserved at: ")
	buf.WriteString(configBackupRel)
	buf.WriteString("\n")
	buf.WriteString("# Previous overrides.json preserved at: ")
	buf.WriteString(overridesBackupRel)
	buf.WriteString("\n")
	buf.WriteString("# Comments and key ordering are not preserved across editor saves.\n")
	buf.WriteString("# Hand-author this file directly if you need either; otherwise the\n")
	buf.WriteString("# web config editor (POST /api/config/values) rewrites it on save.\n\n")
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(rawMap); err != nil {
		return fmt.Errorf("encode merged config.toml: %w", err)
	}

	// Verify-then-commit: parse the encoded bytes back into a *Config
	// BEFORE renaming the temp file or deleting overrides.json. The
	// motivating case was a number-type regression (JSON float64 →
	// "100.0" TOML float → "can't unmarshal float into int" at next
	// Load) where the encode SUCCEEDED but the result was unloadable,
	// so the migration happily deleted overrides.json on its way to a
	// fatal startup failure. The verify step catches that class of
	// bug before any destructive operation runs — if the merged TOML
	// doesn't round-trip, abort with overrides.json untouched and the
	// old config.toml unchanged.
	var verify Config
	if _, err := toml.Decode(buf.String(), &verify); err != nil {
		return fmt.Errorf("merged config.toml didn't round-trip into *Config (refusing to commit, overrides.json preserved): %w", err)
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
