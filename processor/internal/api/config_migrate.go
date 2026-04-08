package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// MigrateResponse describes the result of a config migration.
type MigrateResponse struct {
	Status     string   `json:"status"`
	Backup     string   `json:"backup"`
	FieldsKept []string `json:"fields_kept"`
	FieldsMoved []string `json:"fields_moved"`
}

// HandleConfigMigrate slims down config.toml by moving every web-editable
// non-default value into config/overrides.json. The original config.toml is
// backed up to config.toml.bak.YYYY-MM-DD_HHMMSS, then rewritten to contain
// only TOML-only fields (database, tokens, processor host/port).
//
// POST /api/config/migrate
//
// Idempotent: running it twice produces the same result (the second run
// finds no web-editable fields left in TOML, so nothing to move).
func HandleConfigMigrate(deps ConfigDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		configPath := filepath.Join(deps.ConfigDir, "config.toml")

		// 1. Read the existing config.toml as both raw map and parsed struct
		rawTOML, err := os.ReadFile(configPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "read config.toml: " + err.Error()})
			return
		}

		var rawMap map[string]any
		if err := toml.Unmarshal(rawTOML, &rawMap); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "parse config.toml: " + err.Error()})
			return
		}

		// 2. Backup the original
		timestamp := time.Now().Format("2006-01-02_150405")
		backupPath := filepath.Join(deps.ConfigDir, "config.toml.bak."+timestamp)
		if err := os.WriteFile(backupPath, rawTOML, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "write backup: " + err.Error()})
			return
		}

		// 3. Walk the live config struct, finding non-default web-editable values
		toMove := make(map[string]any)
		var movedPaths []string
		for _, section := range configSchema {
			sectionStruct := findConfigSection(deps.Cfg, section.Name)
			if !sectionStruct.IsValid() {
				continue
			}
			sectionUpdates := make(map[string]any)
			for _, field := range section.Fields {
				val := getFieldByTag(sectionStruct, field.Name)
				if !val.IsValid() {
					continue
				}
				current := val.Interface()
				if isDefaultValue(current, field.Default) {
					continue
				}
				sectionUpdates[field.Name] = current
				movedPaths = append(movedPaths, section.Name+"."+field.Name)
			}
			// Move tables (array-of-tables) too if they have any entries
			for _, table := range section.Tables {
				val := getFieldByTag(sectionStruct, table.Name)
				if !val.IsValid() {
					continue
				}
				if val.Kind() == reflect.Slice && val.Len() == 0 {
					continue
				}
				sectionUpdates[table.Name] = val.Interface()
				movedPaths = append(movedPaths, section.Name+"."+table.Name)
			}
			if len(sectionUpdates) > 0 {
				placeNested(toMove, section.Name, sectionUpdates)
			}
		}

		// 4. Merge into existing overrides.json (without overwriting existing overrides)
		existingOverrides, _, _ := config.LoadOverrides(deps.ConfigDir)
		if existingOverrides == nil {
			existingOverrides = make(map[string]any)
		}
		mergeIfAbsent(existingOverrides, toMove)

		// Marshal merged overrides through SaveOverrides for consistency
		if err := config.SaveOverrides(deps.ConfigDir, existingOverrides); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "save overrides: " + err.Error()})
			return
		}

		// 5. Build slim TOML containing only fields NOT in the schema
		webEditable := buildWebEditableLookup()
		slim := stripWebEditableFields(rawMap, "", webEditable)
		var keptPaths []string
		collectFieldPaths("", slim, &keptPaths)

		var slimBuf bytes.Buffer
		slimBuf.WriteString("# PoracleNG config — slimmed by /api/config/migrate\n")
		slimBuf.WriteString("# Original backed up to: " + filepath.Base(backupPath) + "\n")
		slimBuf.WriteString("# Web-editable settings live in config/overrides.json — view and edit them via the DTS Editor.\n")
		slimBuf.WriteString("# This file should now contain only database/connection/token settings.\n\n")
		if err := toml.NewEncoder(&slimBuf).Encode(slim); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "encode slim toml: " + err.Error()})
			return
		}

		// 6. Atomic write: temp file then rename
		tmpPath := configPath + ".new"
		if err := os.WriteFile(tmpPath, slimBuf.Bytes(), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "write slim toml: " + err.Error()})
			return
		}
		if err := os.Rename(tmpPath, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "rename slim toml: " + err.Error()})
			return
		}

		sort.Strings(movedPaths)
		sort.Strings(keptPaths)
		log.Infof("config: migrated %d field(s) to overrides.json, kept %d in config.toml (backup: %s)",
			len(movedPaths), len(keptPaths), filepath.Base(backupPath))

		c.JSON(http.StatusOK, MigrateResponse{
			Status:      "ok",
			Backup:      filepath.Base(backupPath),
			FieldsKept:  keptPaths,
			FieldsMoved: movedPaths,
		})
	}
}

// isDefaultValue compares a current config value to its schema default.
// Uses JSON round-trip for type-agnostic equality.
func isDefaultValue(current, defaultVal any) bool {
	if defaultVal == nil {
		// No default declared — treat zero value as default
		return reflect.ValueOf(current).IsZero()
	}
	curJSON, err1 := json.Marshal(current)
	defJSON, err2 := json.Marshal(defaultVal)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(curJSON, defJSON)
}

// placeNested writes value into m at the dotted path (e.g. "geofence.koji"),
// creating intermediate maps as needed.
func placeNested(m map[string]any, dottedPath string, value map[string]any) {
	parts := strings.Split(dottedPath, ".")
	cur := m
	for i, part := range parts {
		if i == len(parts)-1 {
			cur[part] = value
			return
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			cur[part] = next
		}
		cur = next
	}
}

// mergeIfAbsent recursively copies entries from src into dst, but only when
// the dst key does not already exist. Used to migrate config.toml fields into
// overrides.json without clobbering existing overrides.
func mergeIfAbsent(dst, src map[string]any) {
	for k, v := range src {
		existing, hasExisting := dst[k]
		if !hasExisting {
			dst[k] = v
			continue
		}
		if existingMap, ok := existing.(map[string]any); ok {
			if srcMap, ok := v.(map[string]any); ok {
				mergeIfAbsent(existingMap, srcMap)
			}
		}
	}
}

// buildWebEditableLookup builds a set of dotted field paths that are
// web-editable (i.e. defined in the schema). Used to filter the raw TOML
// map down to only fields that should remain in config.toml.
func buildWebEditableLookup() map[string]bool {
	result := make(map[string]bool)
	for _, section := range configSchema {
		for _, field := range section.Fields {
			result[section.Name+"."+field.Name] = true
		}
		for _, table := range section.Tables {
			result[section.Name+"."+table.Name] = true
		}
	}
	return result
}

// stripWebEditableFields walks the raw TOML map and returns a copy with all
// fields whose dotted path is in webEditable removed. Empty sections are
// also removed. Top-level sections that are not in the schema at all (e.g.
// [database], [processor]) are kept entirely.
func stripWebEditableFields(m map[string]any, prefix string, webEditable map[string]bool) map[string]any {
	out := make(map[string]any)
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if webEditable[path] {
			continue // drop — moved to overrides.json
		}
		if subMap, ok := v.(map[string]any); ok {
			cleaned := stripWebEditableFields(subMap, path, webEditable)
			if len(cleaned) > 0 {
				out[k] = cleaned
			}
			continue
		}
		out[k] = v
	}
	return out
}

// collectFieldPaths walks a map and produces dotted field paths.
func collectFieldPaths(prefix string, m map[string]any, out *[]string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			collectFieldPaths(path, sub, out)
			continue
		}
		*out = append(*out, path)
	}
}

