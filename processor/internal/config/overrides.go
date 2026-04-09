package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
)

// OverrideStatus describes how overrides.json is layering on top of
// config.toml. Returned by LoadOverrides so the caller can log it AFTER
// logging.Setup has run (LoadOverrides itself runs before logging is
// configured).
type OverrideStatus struct {
	Path      string   // path to overrides.json
	Conflicts []string // dotted field paths set in both files with different values
	Managed   []string // dotted field paths only in overrides.json
}

// LoadOverrides reads config/overrides.json and returns the parsed map
// plus an OverrideStatus describing how it layers on top of config.toml.
// The caller is responsible for logging the status — LoadOverrides itself
// stays silent because it runs before logging is configured.
//
// Returns (nil, nil, nil) if the file doesn't exist.
func LoadOverrides(configDir string) (map[string]any, *OverrideStatus, error) {
	path := filepath.Join(configDir, "overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read overrides.json: %w", err)
	}

	var overrides map[string]any
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, nil, fmt.Errorf("parse overrides.json: %w", err)
	}

	status := classifyOverrides(configDir, path, overrides)
	return overrides, status, nil
}

// LogOverrideStatus prints the override layering report. Call this AFTER
// logging.Setup so the output uses the configured formatter.
//
//   - Conflicts (config.toml and overrides.json disagree): WARN, listed
//   - Managed (only in overrides.json, no conflict): single INFO line
//
// After a config migration, almost everything is in the "managed" bucket
// and the warning section is silent unless the user manually re-edits
// config.toml.
func LogOverrideStatus(status *OverrideStatus) {
	if status == nil {
		return
	}
	if len(status.Conflicts) > 0 {
		log.Warnf("══════════════════════════════════════════════════════════════")
		log.Warnf("config: %d field(s) in config.toml are being overridden by %s", len(status.Conflicts), status.Path)
		log.Warnf("config.toml has these values but overrides.json takes precedence:")
		for _, f := range status.Conflicts {
			log.Warnf("  • %s", f)
		}
		log.Warnf("To use the config.toml values: remove the matching entries from %s", status.Path)
		log.Warnf("══════════════════════════════════════════════════════════════")
	}
	if len(status.Managed) > 0 {
		log.Infof("config: %d field(s) managed by %s (not present in config.toml)", len(status.Managed), status.Path)
	}
}

// classifyOverrides walks the override map and classifies each leaf field
// against the raw config.toml contents. See OverrideStatus.
func classifyOverrides(configDir, overridesPath string, overrides map[string]any) *OverrideStatus {
	tomlPath := filepath.Join(configDir, "config.toml")
	tomlMap := loadConfigTOMLAsMap(tomlPath)

	status := &OverrideStatus{Path: overridesPath}
	collectOverrideClassification("", overrides, tomlMap, &status.Conflicts, &status.Managed)
	return status
}

// loadConfigTOMLAsMap parses config.toml into a generic map so we can do
// path-based lookups. Returns nil if the file is missing or unparseable —
// in that case the conflict detection just degrades to "everything is
// managed", which is the safe (quiet) outcome.
func loadConfigTOMLAsMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// collectOverrideClassification walks the override map and classifies each
// leaf field as either "conflict" (same field set differently in tomlMap)
// or "managed" (field absent from tomlMap). Both lists use dotted paths.
func collectOverrideClassification(prefix string, overrides, tomlMap map[string]any, conflicts, managed *[]string) {
	for k, v := range overrides {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			subToml, _ := tomlMap[k].(map[string]any)
			collectOverrideClassification(path, sub, subToml, conflicts, managed)
			continue
		}
		// Leaf field — check if it's also in config.toml
		if tomlMap == nil {
			*managed = append(*managed, path)
			continue
		}
		tomlVal, present := tomlMap[k]
		if !present {
			*managed = append(*managed, path)
			continue
		}
		if !valuesEqual(tomlVal, v) {
			*conflicts = append(*conflicts, path)
		} else {
			// Same value in both — silently keep as managed (no warning needed)
			*managed = append(*managed, path)
		}
	}
}

// valuesEqual compares two parsed values for semantic equality. Uses
// JSON round-trip to normalise type differences (TOML int64 vs JSON
// float64, etc.).
func valuesEqual(a, b any) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}

// SaveOverrides reads the existing overrides.json, deep-merges the updates,
// and writes back. Creates the file if it doesn't exist.
func SaveOverrides(configDir string, updates map[string]any) error {
	path := filepath.Join(configDir, "overrides.json")

	// Read existing
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse existing overrides.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing overrides.json: %w", err)
	}

	// Expand dotted section keys (e.g. "reconciliation.discord": {...})
	// into nested maps before merging.
	updates = expandDottedKeys(updates)

	// Deep merge updates into existing
	deepMerge(existing, updates)

	// Write back
	buf, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		return fmt.Errorf("write overrides.json: %w", err)
	}

	log.Infof("config: saved overrides to %s", path)
	return nil
}

// expandDottedKeys converts flat dotted keys like "reconciliation.discord"
// into nested maps: {"reconciliation": {"discord": {...}}}. Non-dotted keys
// are left unchanged.
func expandDottedKeys(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		parts := strings.Split(k, ".")
		if len(parts) == 1 {
			result[k] = v
			continue
		}
		// Build nested structure from the dotted key
		cur := result
		for i, part := range parts {
			if i == len(parts)-1 {
				// Last part — set the value, merging if both sides are maps
				if existingMap, ok := cur[part].(map[string]any); ok {
					if vMap, ok := v.(map[string]any); ok {
						deepMerge(existingMap, vMap)
					} else {
						cur[part] = v
					}
				} else {
					cur[part] = v
				}
			} else {
				// Intermediate part — ensure nested map exists
				if next, ok := cur[part].(map[string]any); ok {
					cur = next
				} else {
					next := make(map[string]any)
					cur[part] = next
					cur = next
				}
			}
		}
	}
	return result
}

// deepMerge merges src into dst. For nested maps, recurses. For everything
// else (scalars, arrays), src replaces dst.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}

// ApplyOverrides walks the override map and sets matching fields on the
// Config struct using reflection. Fields are matched by TOML tag name.
func ApplyOverrides(cfg *Config, overrides map[string]any) {
	if overrides == nil {
		return
	}
	applyToStruct(reflect.ValueOf(cfg).Elem(), overrides)
}

// applyToStruct recursively applies override values to a struct.
func applyToStruct(v reflect.Value, overrides map[string]any) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Get TOML tag name
		tag := field.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		// Strip tag options (e.g., "omitempty")
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}

		override, ok := overrides[tag]
		if !ok {
			continue
		}

		// If both the field and override are maps/structs, recurse
		if fieldVal.Kind() == reflect.Struct {
			if subMap, ok := override.(map[string]any); ok {
				applyToStruct(fieldVal, subMap)
				continue
			}
		}

		// Set the field value via JSON round-trip (handles type coercion)
		jsonBytes, err := json.Marshal(override)
		if err != nil {
			log.Warnf("config: override %s: marshal error: %v", tag, err)
			continue
		}

		target := reflect.New(fieldVal.Type())
		if err := json.Unmarshal(jsonBytes, target.Interface()); err != nil {
			log.Warnf("config: override %s: type mismatch: %v", tag, err)
			continue
		}

		fieldVal.Set(target.Elem())
		log.Debugf("config: applied override for %s", tag)
	}
}
