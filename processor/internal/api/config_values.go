package api

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
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

// HandleConfigValues returns current merged config values along with the
// list of fields currently overridden by config/overrides.json. The editor
// uses the overridden list to mark fields visually so users can tell which
// values come from config.toml vs the web editor.
//
// GET /api/config/values?section=discord
func HandleConfigValues(deps ConfigDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		filterSection := c.Query("section")

		values := extractValues(deps.Cfg, filterSection)

		// List dotted paths of currently overridden fields (e.g. "discord.admins")
		overrides, _, _ := config.LoadOverrides(deps.ConfigDir)
		var overridden []string
		collectOverrideFieldPaths("", overrides, &overridden)

		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"values":     values,
			"overridden": overridden,
		})
	}
}

// collectOverrideFieldPaths walks an override map and produces dotted field paths.
func collectOverrideFieldPaths(prefix string, m map[string]any, out *[]string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			collectOverrideFieldPaths(path, sub, out)
			continue
		}
		*out = append(*out, path)
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

		// Per-field value validation (colour format, array length, paths)
		issues := validateConfigValues(updates, deps.ConfigDir)
		var errorIssues []ValidationIssue
		for _, iss := range issues {
			if iss.Severity == "error" {
				errorIssues = append(errorIssues, iss)
			}
		}
		if len(errorIssues) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "validation failed",
				"issues":  errorIssues,
			})
			return
		}

		// Strip masked sensitive values ("****") so the editor can resubmit
		// a form without wiping secrets the user didn't touch.
		stripMaskedSensitiveValues(updates)

		// Convert flat table-row fields (discord_channels etc.) back into
		// the nested struct shape before persisting.
		nestTableUpdates(updates)

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
			if !val.IsValid() {
				continue
			}
			// Some tables store nested sub-structs that the schema exposes as
			// flat fields (e.g. area_security.communities has a Discord
			// sub-struct surfaced as discord_channels / discord_user_role).
			// Flatten here so the editor sees the shape its schema describes.
			if section.Name == "area_security" && table.Name == "communities" {
				if comms, ok := val.Interface().([]config.CommunityConfig); ok {
					sectionValues[table.Name] = flattenCommunities(comms)
					continue
				}
			}
			// tileserver_settings is a map keyed by maptype — flatten into an
			// array of rows with maptype as a regular field so the editor can
			// edit it with the standard Tables UI.
			if section.Name == "geocoding" && table.Name == "tileserver_settings" {
				if settings, ok := val.Interface().(map[string]config.TileserverConfig); ok {
					sectionValues[table.Name] = flattenTileserverSettings(settings)
					continue
				}
			}
			sectionValues[table.Name] = val.Interface()
		}

		result[section.Name] = sectionValues
	}

	return result
}

// findConfigSection returns the reflect.Value for a config section.
// Supports dotted paths like "reconciliation.discord" or "geofence.koji".
func findConfigSection(cfg *config.Config, sectionName string) reflect.Value {
	v := reflect.ValueOf(cfg).Elem()
	parts := strings.SplitSeq(sectionName, ".")
	for part := range parts {
		v = getFieldByTag(v, part)
		if !v.IsValid() {
			return reflect.Value{}
		}
	}
	return v
}

// getFieldByTag finds a struct field by its TOML tag name.
func getFieldByTag(v reflect.Value, tagName string) reflect.Value {
	if v.Kind() == reflect.Pointer {
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
	tableRowFields := make(map[string]map[string]bool) // "section.table" → set of row field names
	for _, section := range configSchema {
		fields := make(map[string]bool)
		for _, f := range section.Fields {
			fields[f.Name] = true
		}
		for _, t := range section.Tables {
			fields[t.Name] = true
			rowFields := make(map[string]bool, len(t.Fields))
			for _, rf := range t.Fields {
				rowFields[rf.Name] = true
			}
			tableRowFields[section.Name+"."+t.Name] = rowFields
		}
		schemaLookup[section.Name] = fields
	}

	for sectionName, sectionVal := range updates {
		fields, ok := schemaLookup[sectionName]
		if !ok {
			return fmt.Errorf("unknown config section: %s", sectionName)
		}
		sectionMap, ok := sectionVal.(map[string]any)
		if !ok {
			continue
		}
		for fieldName, fieldVal := range sectionMap {
			if !fields[fieldName] {
				return fmt.Errorf("unknown field %s.%s", sectionName, fieldName)
			}
			// If this field corresponds to a table, validate per-row keys.
			rowFields, isTable := tableRowFields[sectionName+"."+fieldName]
			if !isTable {
				continue
			}
			rows, ok := fieldVal.([]any)
			if !ok {
				continue
			}
			for i, row := range rows {
				rowMap, ok := row.(map[string]any)
				if !ok {
					continue
				}
				for k := range rowMap {
					if !rowFields[k] {
						return fmt.Errorf("unknown field %s.%s[%d].%s", sectionName, fieldName, i, k)
					}
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

// stripMaskedSensitiveValues walks the updates map and removes any
// sensitive field whose value is the masked placeholder "****". This lets
// the editor resubmit a whole form without wiping secrets the user
// didn't actually change.
func stripMaskedSensitiveValues(updates map[string]any) {
	sensitive := make(map[string]map[string]bool)
	for _, section := range configSchema {
		fields := make(map[string]bool)
		for _, f := range section.Fields {
			if f.Sensitive {
				fields[f.Name] = true
			}
		}
		if len(fields) > 0 {
			sensitive[section.Name] = fields
		}
	}

	for sectionName, sectionVal := range updates {
		sectionMap, ok := sectionVal.(map[string]any)
		if !ok {
			continue
		}
		fields, ok := sensitive[sectionName]
		if !ok {
			continue
		}
		for fieldName := range fields {
			if v, ok := sectionMap[fieldName]; ok && v == "****" {
				delete(sectionMap, fieldName)
			}
		}
	}
}

// flattenCommunities converts []config.CommunityConfig into the flat-field
// shape the schema exposes to the editor (discord_channels, discord_user_role,
// telegram_channels, telegram_admins). Inverse of nestCommunityRows.
func flattenCommunities(communities []config.CommunityConfig) []map[string]any {
	out := make([]map[string]any, 0, len(communities))
	for _, c := range communities {
		out = append(out, map[string]any{
			"name":              c.Name,
			"allowed_areas":     c.AllowedAreas,
			"location_fence":    []string(c.LocationFence),
			"discord_channels":  c.Discord.Channels,
			"discord_user_role": c.Discord.UserRole,
			"telegram_channels": c.Telegram.Channels,
			"telegram_admins":   c.Telegram.Admins,
		})
	}
	return out
}

// nestCommunityRows takes the editor's flat community rows and rebuilds the
// nested struct shape expected by CommunityConfig (discord: {channels, user_role},
// telegram: {channels, admins}). Leaves unrelated keys alone so that any schema
// field we forgot to flatten still round-trips unchanged.
func nestCommunityRows(rows []any) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			out = append(out, row)
			continue
		}
		nested := make(map[string]any, len(m))
		var discord, telegram map[string]any
		for k, v := range m {
			switch k {
			case "discord_channels", "discord_user_role":
				if discord == nil {
					discord = map[string]any{}
				}
				discord[strings.TrimPrefix(k, "discord_")] = v
			case "telegram_channels", "telegram_admins":
				if telegram == nil {
					telegram = map[string]any{}
				}
				telegram[strings.TrimPrefix(k, "telegram_")] = v
			default:
				nested[k] = v
			}
		}
		if discord != nil {
			nested["discord"] = discord
		}
		if telegram != nil {
			nested["telegram"] = telegram
		}
		out = append(out, nested)
	}
	return out
}

// nestTableUpdates applies any table-row flattening inverse transforms on an
// incoming save payload. Keeps overrides.json aligned with the Go struct's
// tagged shape so ApplyOverrides can deserialise it cleanly.
func nestTableUpdates(updates map[string]any) {
	if area, ok := updates["area_security"].(map[string]any); ok {
		if comms, ok := area["communities"].([]any); ok {
			area["communities"] = nestCommunityRows(comms)
		}
	}
	if geo, ok := updates["geocoding"].(map[string]any); ok {
		if rows, ok := geo["tileserver_settings"].([]any); ok {
			geo["tileserver_settings"] = nestTileserverRows(rows)
		}
	}
}

// flattenTileserverSettings converts map[string]TileserverConfig into the flat
// array-of-rows shape the editor expects, with maptype as a regular field.
// Inverse of nestTileserverRows.
//
// include_stops and pregenerate are emitted as JSON null when unset (nil *bool)
// rather than false. The merge in staticmap.go:mergeOpts distinguishes nil
// ("inherit from default") from *bool(false) ("explicit override"), so a
// round-trip that coerced nil→false would silently clobber a default=true
// setting on every per-maptype entry.
func flattenTileserverSettings(settings map[string]config.TileserverConfig) []map[string]any {
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		v := settings[k]
		row := map[string]any{
			"maptype":       k,
			"type":          v.Type,
			"width":         v.Width,
			"height":        v.Height,
			"zoom":          v.Zoom,
			"ttl":           v.TTL,
			"include_stops": boolPtrOrNull(v.IncludeStops),
			"pregenerate":   boolPtrOrNull(v.Pregenerate),
		}
		out = append(out, row)
	}
	return out
}

// nestTileserverRows rebuilds a map[string]<config> keyed by maptype from the
// editor's array-of-rows payload. The TileserverConfig struct has JSON tags
// matching the row field names, so ApplyOverrides' JSON round-trip will
// deserialise it into the live config cleanly.
func nestTileserverRows(rows []any) map[string]any {
	out := make(map[string]any, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["maptype"].(string)
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		entry := make(map[string]any, len(m))
		for k, v := range m {
			if k == "maptype" {
				continue
			}
			entry[k] = v
		}
		out[key] = entry
	}
	return out
}

// boolPtrOrNull returns the underlying bool as any, or an untyped nil (which
// marshals to JSON null) when the pointer is nil. Used so tri-state *bool
// config fields round-trip through the editor without collapsing nil to false.
func boolPtrOrNull(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
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
