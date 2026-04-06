package api

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// ConfigDeps holds dependencies for config API handlers.
type ConfigDeps struct {
	Cfg       *config.Config
	ConfigDir string
	ReloadFn  func() // called after hot-reloadable settings change
}

// HandleConfigValues returns current merged config values.
// GET /api/config/values?section=discord
func HandleConfigValues(deps ConfigDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		filterSection := c.Query("section")

		values := extractValues(deps.Cfg, filterSection)

		c.JSON(http.StatusOK, gin.H{"status": "ok", "values": values})
	}
}

// HandleConfigSave saves config changes to overrides.json.
// POST /api/config/values
func HandleConfigSave(deps ConfigDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var updates map[string]any
		if err := c.ShouldBindJSON(&updates); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body: " + err.Error()})
			return
		}

		if len(updates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "no changes provided"})
			return
		}

		// Validate that all sections/fields exist in schema
		if err := validateUpdates(updates); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		// Save to overrides.json
		if err := config.SaveOverrides(deps.ConfigDir, updates); err != nil {
			log.Errorf("config save: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "save failed: " + err.Error()})
			return
		}

		// Apply to in-memory config
		config.ApplyOverrides(deps.Cfg, updates)

		// Check if restart is required
		restartRequired, restartFields := checkRestartRequired(updates)

		// Trigger hot-reload if applicable
		if !restartRequired && deps.ReloadFn != nil {
			deps.ReloadFn()
		}

		saved := countFields(updates)
		log.Infof("config: saved %d field(s) via API (restart_required=%v)", saved, restartRequired)

		resp := gin.H{
			"status":           "ok",
			"saved":            saved,
			"restart_required": restartRequired,
		}
		if len(restartFields) > 0 {
			resp["restart_fields"] = restartFields
		}
		c.JSON(http.StatusOK, resp)
	}
}

// extractValues reads config fields that are in the schema (web-editable only).
// Uses reflection to walk the config struct and match by TOML tag.
func extractValues(cfg *config.Config, filterSection string) map[string]any {
	result := make(map[string]any)

	for _, section := range configSchema {
		if filterSection != "" && section.Name != filterSection {
			continue
		}

		sectionValues := make(map[string]any)
		sectionStruct := findConfigSection(cfg, section.Name)
		if !sectionStruct.IsValid() {
			continue
		}

		for _, field := range section.Fields {
			if field.Sensitive {
				sectionValues[field.Name] = "****"
				continue
			}
			val := getFieldByTag(sectionStruct, field.Name)
			if val.IsValid() {
				sectionValues[field.Name] = val.Interface()
			}
		}

		// Extract table values
		for _, table := range section.Tables {
			val := getFieldByTag(sectionStruct, table.Name)
			if val.IsValid() {
				sectionValues[table.Name] = val.Interface()
			}
		}

		result[section.Name] = sectionValues
	}

	return result
}

// findConfigSection returns the reflect.Value for a top-level config section.
func findConfigSection(cfg *config.Config, sectionName string) reflect.Value {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == sectionName {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// getFieldByTag finds a struct field by its TOML tag name.
func getFieldByTag(v reflect.Value, tagName string) reflect.Value {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == tagName {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// validateUpdates checks that all section/field names in the update exist in the schema.
func validateUpdates(updates map[string]any) error {
	// Build lookup
	schemaLookup := make(map[string]map[string]bool)
	for _, section := range configSchema {
		fields := make(map[string]bool)
		for _, f := range section.Fields {
			fields[f.Name] = true
		}
		for _, t := range section.Tables {
			fields[t.Name] = true
		}
		schemaLookup[section.Name] = fields
	}

	for sectionName, sectionVal := range updates {
		fields, ok := schemaLookup[sectionName]
		if !ok {
			return fmt.Errorf("unknown config section: %s", sectionName)
		}
		if sectionMap, ok := sectionVal.(map[string]any); ok {
			for fieldName := range sectionMap {
				if !fields[fieldName] {
					return fmt.Errorf("unknown field %s.%s", sectionName, fieldName)
				}
			}
		}
	}
	return nil
}

// checkRestartRequired returns true if any updated field has hotReload: false.
func checkRestartRequired(updates map[string]any) (bool, []string) {
	// Build hot-reload lookup from schema
	hotReloadable := make(map[string]bool) // "section.field" → hotReload
	for _, section := range configSchema {
		for _, f := range section.Fields {
			hotReloadable[section.Name+"."+f.Name] = f.HotReload
		}
	}

	var restartFields []string
	for sectionName, sectionVal := range updates {
		if sectionMap, ok := sectionVal.(map[string]any); ok {
			for fieldName := range sectionMap {
				key := sectionName + "." + fieldName
				if !hotReloadable[key] {
					restartFields = append(restartFields, key)
				}
			}
		}
	}

	return len(restartFields) > 0, restartFields
}

// countFields counts the total number of individual field changes.
func countFields(updates map[string]any) int {
	count := 0
	for _, v := range updates {
		if m, ok := v.(map[string]any); ok {
			count += len(m)
		}
	}
	return count
}
