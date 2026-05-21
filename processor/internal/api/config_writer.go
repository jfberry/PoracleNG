package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/pokemon/poracleng/processor/internal/backup"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// writeConfigTOML rewrites config/config.toml in place with the
// supplied editor updates applied on top of the existing on-disk file.
//
// Steady-state persistence model: there is no overrides.json — every
// saved value lives in config.toml. Two file properties keep that
// model honest:
//
//   - **Backup before write**: the previous config.toml is preserved
//     under config/backups/ via backup.Save. Operators who relied on
//     hand-authored comments or layout can recover them.
//
//   - **Default elision**: a value matching the schema default is
//     written as a key REMOVAL, not as a literal entry. Operators who
//     deliberately set a value matching the default still want the
//     line gone — the default will follow them if it ever changes.
//     This is npm/Cargo convention.
//
// updates is a nested map shaped like { section: { field: value, ... } }
// with dotted-section keys ("reconciliation.discord") already expanded
// by the caller (HandleConfigSave handles that step).
//
// Non-schema fields in the existing config.toml (database, tokens,
// processor host/port, any operator-added comments-on-keys) are
// passed through untouched.
func writeConfigTOML(configDir string, updates map[string]any) (backupRel string, err error) {
	configPath := filepath.Join(configDir, "config.toml")

	rawTOML, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read config.toml: %w", err)
	}
	var rawMap map[string]any
	if err := toml.Unmarshal(rawTOML, &rawMap); err != nil {
		return "", fmt.Errorf("parse config.toml: %w", err)
	}

	applySchemaUpdates(rawMap, updates)

	// JSON unmarshal gives float64 for every number — including
	// integer-valued ones — so the editor's POST payload arrives with
	// int-shaped values as float64. Without normalisation the TOML
	// encoder writes them as "100.0" and the next config.Load fails
	// to unmarshal them into Go int fields.
	config.NormalizeNumericValues(rawMap)

	backupRel, err = backup.Save(configDir, "config.toml")
	if err != nil {
		return "", fmt.Errorf("backup config.toml: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("# PoracleNG config — managed by the web editor.\n")
	buf.WriteString("# Comments and key ordering are not preserved across saves.\n")
	buf.WriteString("# Hand-author this file directly if you need either; the editor\n")
	buf.WriteString("# (POST /api/config/values) rewrites it on every save.\n")
	buf.WriteString("# Previous version: ")
	buf.WriteString(backupRel)
	buf.WriteString("\n\n")
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(rawMap); err != nil {
		return backupRel, fmt.Errorf("encode config.toml: %w", err)
	}

	tmpPath := configPath + ".new"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return backupRel, fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return backupRel, fmt.Errorf("rename %s → %s: %w", tmpPath, configPath, err)
	}

	return backupRel, nil
}

// applySchemaUpdates walks the updates and applies them to rawMap.
// Values matching the schema default are removed from rawMap rather
// than written explicitly — the elision behaviour described on
// writeConfigTOML.
//
// updates is { section: { field: value } } — a flat two-level shape
// after nestTableUpdates / expandDottedKeys ran in HandleConfigSave.
// Nested section names (reconciliation.discord) arrive as a single
// top-level key with a sub-map and are handled identically.
func applySchemaUpdates(rawMap, updates map[string]any) {
	schemaDefaults := buildSchemaDefaultsMap()
	for sectionName, sectionVal := range updates {
		sectionMap, ok := sectionVal.(map[string]any)
		if !ok {
			// Should not happen given the editor's wire shape, but
			// passthrough rather than drop so an unexpected shape
			// is at least observable.
			rawMap[sectionName] = sectionVal
			continue
		}
		applySectionUpdates(rawMap, sectionName, sectionMap, schemaDefaults)
	}
}

// applySectionUpdates handles a single section's worth of updates. The
// section may itself be nested (reconciliation.discord) — the rawMap
// branch is materialised on demand so first-time settings under a
// brand-new section work.
func applySectionUpdates(rawMap map[string]any, sectionName string, sectionUpdates map[string]any, schemaDefaults map[string]any) {
	section, ok := rawMap[sectionName].(map[string]any)
	if !ok {
		section = make(map[string]any)
		rawMap[sectionName] = section
	}
	for field, val := range sectionUpdates {
		// Tables (array-of-tables, e.g. autocreate.rules) and nested
		// sub-sections (reconciliation.discord) arrive as maps/slices
		// and don't have a schema-default to elide against. Pass
		// through as-is.
		fullPath := sectionName + "." + field
		defaultVal, hasDefault := schemaDefaults[fullPath]
		if !hasDefault {
			section[field] = val
			continue
		}
		if isDefaultValue(val, defaultVal) {
			delete(section, field)
			continue
		}
		section[field] = val
	}
	// If the section is empty after deletions, drop it entirely so the
	// rendered TOML doesn't carry a bare `[section]` header.
	if len(section) == 0 {
		delete(rawMap, sectionName)
	}
}

// buildSchemaDefaultsMap collects "section.field" → default-value pairs
// from configSchema. Defaults of nil (no default declared) are
// excluded — those fields are passed through without elision.
func buildSchemaDefaultsMap() map[string]any {
	out := make(map[string]any)
	for _, section := range configSchema {
		for _, field := range section.Fields {
			if field.Default == nil {
				continue
			}
			out[section.Name+"."+field.Name] = field.Default
		}
	}
	return out
}

// isDefaultValue compares a current config value to its schema default.
// Uses JSON round-trip for type-agnostic equality so int/float/string
// variants of the same number compare equal.
func isDefaultValue(current, defaultVal any) bool {
	if defaultVal == nil {
		// No default declared — treat zero value as default.
		return current == nil || jsonZero(current)
	}
	curJSON, err1 := json.Marshal(current)
	defJSON, err2 := json.Marshal(defaultVal)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(curJSON, defJSON)
}

// jsonZero reports whether v marshals to the JSON zero value for its
// kind (0, "", false, [], {}). Cheaper than reflect.Value.IsZero for
// the post-JSON-unmarshal interface{} shapes the editor produces.
func jsonZero(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case string:
		return t == ""
	case float64:
		return t == 0
	case int:
		return t == 0
	case int64:
		return t == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}
