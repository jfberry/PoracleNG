# Config Editor API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add API endpoints for the DTS Editor to read, modify, and save PoracleNG configuration, plus batch-resolve Discord/Telegram IDs to human-readable names.

**Architecture:** Config schema is hand-maintained static data (generated initially from config struct/example). Config changes are saved to `config/overrides.json` (never touching config.toml). Hot-reloadable settings are applied immediately; others flag "restart required". A batch resolution endpoint resolves platform IDs using the Discord session and Telegram bot API, cached with ttlcache (10 min TTL).

**Tech Stack:** Go, Gin, existing config/config.go, discordgo, go-telegram-bot-api, jellydator/ttlcache

**Design spec:** `docs/superpowers/specs/2026-04-06-config-editor-api-design.md`

---

## File Structure

### Files to create
- `processor/internal/api/config_schema.go` — schema definitions: section/field metadata, types, defaults, descriptions, options, dependencies, resolve hints
- `processor/internal/api/config_values.go` — GET/POST config values handlers
- `processor/internal/api/config_resolve.go` — batch ID resolution handler
- `processor/internal/config/overrides.go` — LoadOverrides, SaveOverrides, ApplyOverrides

### Files to modify
- `processor/internal/config/config.go` — call LoadOverrides + ApplyOverrides after TOML load
- `processor/cmd/processor/main.go` — register new routes, pass dependencies

---

## Task 1: Override File Infrastructure

**Files:**
- Create: `processor/internal/config/overrides.go`
- Modify: `processor/internal/config/config.go`

- [ ] **Step 1: Create overrides.go with LoadOverrides**

Create `processor/internal/config/overrides.go`:

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
)

// LoadOverrides reads config/overrides.json and returns the parsed map.
// Returns nil (not an error) if the file doesn't exist.
func LoadOverrides(configDir string) (map[string]any, error) {
	path := filepath.Join(configDir, "overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read overrides.json: %w", err)
	}

	var overrides map[string]any
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("parse overrides.json: %w", err)
	}

	log.Infof("config: loaded %d override sections from %s", len(overrides), path)
	return overrides, nil
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
	}

	// Deep merge updates into existing
	deepMerge(existing, updates)

	// Write back
	var buf []byte
	var err error
	buf, err = json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		return fmt.Errorf("write overrides.json: %w", err)
	}

	log.Infof("config: saved overrides to %s", path)
	return nil
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
```

- [ ] **Step 2: Integrate override loading into config.go**

In `processor/internal/config/config.go`, find the `LoadConfig` function (or wherever the config is loaded after TOML parsing). After the TOML is parsed and `buildDelegatedAdmin` etc. are called, add:

```go
// Apply JSON overrides (from config editor)
configDir := filepath.Join(cfg.BaseDir, "config")
overrides, err := LoadOverrides(configDir)
if err != nil {
	log.Warnf("config: %v", err)
} else if overrides != nil {
	ApplyOverrides(cfg, overrides)
	// Re-run computed fields after overrides
	cfg.Discord.DelegatedAdministration = DelegatedAdminConfig{
		ChannelTracking: buildDelegatedAdmin(cfg.Discord.DelegatedAdmins),
		WebhookTracking: buildDelegatedAdmin(cfg.Discord.WebhookAdmins),
		UserTracking:    cfg.Discord.UserTrackingAdmins,
	}
	cfg.Telegram.DelegatedAdministration = TelegramDelegatedAdminConfig{
		ChannelTracking: buildDelegatedAdmin(cfg.Telegram.DelegatedAdmins),
		UserTracking:    cfg.Telegram.UserTrackingAdmins,
	}
}
```

**Note for implementing agent:** Find the exact location in `config.go` where `buildDelegatedAdmin` is called (around line 654-661) and add the override loading after that block. Check the actual function name used for config loading — it may be `LoadConfig`, `Load`, or inline in `main.go`.

- [ ] **Step 3: Build and verify**

```bash
cd processor && go build ./cmd/processor/
```

Expected: clean build. No overrides.json exists yet, so behaviour is unchanged.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/config/overrides.go processor/internal/config/config.go
git commit -m "feat: add JSON override file infrastructure for config editor"
```

---

## Task 2: Config Schema Definitions

**Files:**
- Create: `processor/internal/api/config_schema.go`

This is the largest file — hand-maintained schema definitions generated from the config struct and config.example.toml. Each field includes type, default, description, and optional metadata.

- [ ] **Step 1: Create config_schema.go**

Create `processor/internal/api/config_schema.go` with the full schema definition. The file defines types and static data:

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ConfigFieldDef describes a single config field for the editor.
type ConfigFieldDef struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"` // string, int, float, bool, string[], select, map
	Default     any                 `json:"default,omitempty"`
	Description string              `json:"description"`
	HotReload   bool                `json:"hotReload,omitempty"`
	Sensitive   bool                `json:"sensitive,omitempty"`
	Resolve     string              `json:"resolve,omitempty"`
	Options     []ConfigSelectOption `json:"options,omitempty"`
	DependsOn   *ConfigDependency   `json:"dependsOn,omitempty"`
}

// ConfigSelectOption is a constrained option for select-type fields.
type ConfigSelectOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ConfigDependency makes a field visible only when another field has a specific value.
type ConfigDependency struct {
	Field string `json:"field"`
	Value any    `json:"value"`
}

// ConfigTableDef describes an array-of-tables config section.
type ConfigTableDef struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Fields      []ConfigFieldDef `json:"fields"`
}

// ConfigSection groups fields and tables under a named section.
type ConfigSection struct {
	Name   string           `json:"name"`
	Title  string           `json:"title"`
	Fields []ConfigFieldDef `json:"fields"`
	Tables []ConfigTableDef `json:"tables,omitempty"`
}
```

Then define the schema sections. Below are the first few sections — the implementing agent must complete ALL sections from the config inventory.

**Note for implementing agent:** The full config has ~180 fields across ~20 sections. Build the complete schema from `processor/internal/config/config.go` (struct definitions with TOML tags) and `config/config.example.toml` (comments as descriptions). Exclude TOML-only fields: all of `[processor]`, all of `[database]`, all of `[database.scanner]`, discord/telegram tokens, accuweather API keys, geocoding keys, static map keys, koji bearer token. For `select` type fields, provide `options` with value/label/description. For fields with resolve hints, set the `resolve` field. For dependent fields, set `dependsOn`.

```go
var configSchema = []ConfigSection{
	{
		Name:  "general",
		Title: "General Settings",
		Fields: []ConfigFieldDef{
			{Name: "locale", Type: "string", Default: "en", Description: "Default language for new users and system messages", HotReload: true},
			{Name: "role_check_mode", Type: "select", Default: "ignore", Description: "Action when a user loses their required Discord/Telegram role", HotReload: true, Options: []ConfigSelectOption{
				{Value: "ignore", Label: "Ignore", Description: "Log the event but take no action — user keeps their registration"},
				{Value: "disable-user", Label: "Disable User", Description: "Set admin_disable flag, remove subscription roles, send goodbye message — user must re-register"},
				{Value: "delete", Label: "Delete", Description: "Permanently remove all tracking data and human record"},
			}},
			{Name: "default_template_name", Type: "string", Default: "1", Description: "Default DTS template ID for new tracking rules", HotReload: true},
			{Name: "disabled_commands", Type: "string[]", Default: []string{}, Description: "Commands to disable globally (e.g., [\"lure\", \"nest\"])", HotReload: true},
			{Name: "rdm_url", Type: "string", Default: "", Description: "RDM map instance URL for {{rdmUrl}} in templates", HotReload: true},
			{Name: "react_map_url", Type: "string", Default: "", Description: "ReactMap instance URL for {{reactMapUrl}} in templates", HotReload: true},
			{Name: "rocket_mad_url", Type: "string", Default: "", Description: "RocketMAD instance URL for {{rocketMadUrl}} in templates", HotReload: true},
			{Name: "img_url", Type: "string", Default: "https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS", Description: "Base URL for pokemon icon images ({{imgUrl}})", HotReload: false},
			{Name: "img_url_alt", Type: "string", Default: "", Description: "Alternative icon URL for {{imgUrlAlt}}", HotReload: false},
			{Name: "sticker_url", Type: "string", Default: "", Description: "Base URL for Telegram sticker icons ({{stickerUrl}})", HotReload: false},
			{Name: "request_shiny_images", Type: "bool", Default: false, Description: "Request shiny variants from icon repositories", HotReload: false},
			{Name: "populate_pokestop_name", Type: "bool", Default: false, Description: "Look up nearby pokestop names from scanner database (RDM only)", HotReload: false},
			{Name: "alert_minimum_time", Type: "int", Default: 120, Description: "Minimum seconds before expiry — alerts with less time remaining are dropped", HotReload: true},
			{Name: "ignore_long_raids", Type: "bool", Default: false, Description: "Skip raids/eggs with time-to-hatch over 47 minutes", HotReload: true},
			{Name: "shortlink_provider", Type: "select", Default: "", Description: "URL shortener for <S< >S> markers in DTS templates", HotReload: false, Options: []ConfigSelectOption{
				{Value: "", Label: "None", Description: "No URL shortening"},
				{Value: "shlink", Label: "Shlink", Description: "Self-hosted Shlink instance"},
				{Value: "hideuri", Label: "HideURI", Description: "HideURI public shortener"},
				{Value: "yourls", Label: "YOURLS", Description: "Your Own URL Shortener"},
			}},
			{Name: "shortlink_provider_url", Type: "string", Default: "", Description: "Shortlink service URL", HotReload: false, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "shortlink_provider_key", Type: "string", Default: "", Description: "Shortlink API key", HotReload: false, Sensitive: true, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "shortlink_provider_domain", Type: "string", Default: "", Description: "Shortlink domain override", HotReload: false, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "disable_pokemon", Type: "bool", Default: false, Description: "Disable pokemon webhook processing entirely", HotReload: false},
			{Name: "disable_raid", Type: "bool", Default: false, Description: "Disable raid webhook processing", HotReload: false},
			{Name: "disable_pokestop", Type: "bool", Default: false, Description: "Disable pokestop/invasion processing", HotReload: false},
			{Name: "disable_invasion", Type: "bool", Default: false, Description: "Disable invasion webhook processing", HotReload: false},
			{Name: "disable_lure", Type: "bool", Default: false, Description: "Disable lure webhook processing", HotReload: false},
			{Name: "disable_quest", Type: "bool", Default: false, Description: "Disable quest webhook processing", HotReload: false},
			{Name: "disable_weather", Type: "bool", Default: false, Description: "Disable weather webhook processing", HotReload: false},
			{Name: "disable_nest", Type: "bool", Default: false, Description: "Disable nest webhook processing", HotReload: false},
			{Name: "disable_gym", Type: "bool", Default: false, Description: "Disable gym webhook processing", HotReload: false},
			{Name: "disable_max_battle", Type: "bool", Default: false, Description: "Disable max battle webhook processing", HotReload: false},
			{Name: "disable_fort_update", Type: "bool", Default: false, Description: "Disable fort update webhook processing", HotReload: false},
		},
	},
	// ... (implementing agent: add ALL remaining sections following this pattern)
	// Sections to include: locale, discord, telegram, geofence, pvp, weather,
	// area_security, geocoding, tuning, alert_limits, tracking, reconciliation.discord,
	// reconciliation.telegram, stats, fallbacks, logging, webhookLogging, ai
}

// HandleConfigSchema returns the config schema for the editor.
// GET /api/config/schema
func HandleConfigSchema() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "sections": configSchema})
	}
}
```

**Implementing agent checklist for remaining sections:**

**[locale]** — timeformat, time, date, address_format, language. All hotReload: true.

**[discord]** — enabled, prefix, activity, channels (resolve: discord:channel), guilds (resolve: discord:guild), user_role (resolve: discord:role), admins (resolve: discord:user), check_role, check_role_interval (dependsOn check_role), lost_role_message (dependsOn check_role), disable_auto_greetings, dm_log_channel_id (resolve: discord:channel), dm_log_channel_deletion_time (dependsOn dm_log_channel_id), unrecognised_command_message, unregistered_user_message, iv_colors (string[]), upload_embed_images, message_delete_delay, user_tracking_admins (resolve: discord:user|role), command_security (map type). Tables: delegated_admins, webhook_admins, role_subscriptions. Exclude token.

**[telegram]** — enabled, channels (resolve: telegram:chat), admins (resolve: telegram:chat), check_role, check_role_interval (dependsOn check_role), bot_welcome_text, bot_goodbye_message (dependsOn check_role), group_welcome_text, unregistered_user_message, unrecognised_command_message, register_on_start, disable_auto_greetings, user_tracking_admins (resolve: telegram:chat). Tables: delegated_admins. Exclude token.

**[geofence]** — paths (string[]), default_name. Subsection koji: cache_dir. Exclude koji bearer_token.

**[pvp]** — all fields, none sensitive. data_source is select with options "webhook"/"ohbem".

**[weather]** — enable_inference, change_alert, show_altered_pokemon, show_altered_pokemon_max_count (dependsOn show_altered_pokemon), show_altered_pokemon_static_map (dependsOn show_altered_pokemon), enable_forecast, accuweather_day_quota (dependsOn enable_forecast), forecast_refresh_interval (dependsOn enable_forecast), local_first_fetch_hod (dependsOn enable_forecast), smart_forecast (dependsOn enable_forecast). Exclude accuweather_api_keys.

**[area_security]** — enabled, strict_locations (dependsOn enabled). Tables: communities. The communities table is complex with nested sub-objects:

```go
ConfigTableDef{
	Name:        "communities",
	Title:       "Communities",
	Description: "Each community defines a group of users with shared area access, registration channels, and role requirements",
	Fields: []ConfigFieldDef{
		{Name: "name", Type: "string", Description: "Community identifier (e.g., \"newyork\")"},
		{Name: "allowed_areas", Type: "string[]", Description: "Geofence area names users in this community can select with !area"},
		{Name: "location_fence", Type: "string[]", Description: "Geofence used for strict_locations enforcement — alerts outside this fence are blocked"},
		{Name: "discord_channels", Type: "string[]", Description: "Discord channel IDs where !poracle registers users into this community", Resolve: "discord:channel"},
		{Name: "discord_user_role", Type: "string[]", Description: "Discord role IDs that grant membership in this community", Resolve: "discord:role"},
		{Name: "telegram_channels", Type: "string[]", Description: "Telegram group/channel IDs that qualify users for this community", Resolve: "telegram:chat"},
		{Name: "telegram_admins", Type: "string[]", Description: "Telegram user IDs with admin rights for this community", Resolve: "telegram:chat"},
	},
}
```

Note: The Go struct has nested `Discord` and `Telegram` sub-structs, but for the JSON API schema we flatten them with prefixed names (`discord_channels`, `discord_user_role`, `telegram_channels`, `telegram_admins`) to avoid nested objects inside a table row. The `ApplyOverrides` function needs to handle this flattening when writing back to the config struct.

**[geocoding]** — provider (select: none/nominatim/google), provider_url (dependsOn provider=nominatim), cache_detail, forward_only, static_provider (select: none/tileservercache/google/osm/mapbox), static_provider_url (dependsOn static_provider), width, height, zoom, type, day_style, dawn_style, dusk_style, night_style. Exclude geocoding_key, static_key.

**[tuning]** — all fields, all hotReload: false (restart required).

**[alert_limits]** — timing_period, dm_limit, channel_limit, max_limits_before_stop, disable_on_stop, shame_channel (resolve: discord:channel). Tables: overrides. All hotReload: true.

**[tracking]** — everything_flag_permissions (select with 4 options), default_distance, max_distance, enable_gym_battle, default_user_tracking_level_cap. All hotReload: true.

**[reconciliation.discord]** — update_user_names, remove_invalid_users, register_new_users, update_channel_names, update_channel_notes, unregister_missing_channels. All hotReload: false.

**[reconciliation.telegram]** — update_user_names, remove_invalid_users. All hotReload: false.

**[stats]** — min_sample_size, window_hours, refresh_interval_mins, rarity_group_2_uncommon through rarity_group_5_ultra_rare. All hotReload: false.

**[fallbacks]** — static_map, img_url, img_url_weather, img_url_egg, img_url_gym, img_url_pokestop, pokestop_url. All hotReload: false.

**[logging]** — level (select: debug/verbose/info/warn), file_logging_enabled, filename, max_size, max_age, max_backups, compress. All hotReload: false.

**[webhookLogging]** — enabled, filename, max_size, max_age, max_backups, compress, rotate_interval. All hotReload: false.

**[ai]** — enabled, suggest_on_dm (dependsOn enabled). All hotReload: false.

- [ ] **Step 2: Build and verify**

```bash
cd processor && go build ./cmd/processor/
```

- [ ] **Step 3: Commit**

```bash
git add processor/internal/api/config_schema.go
git commit -m "feat: add config schema definitions for config editor"
```

---

## Task 3: Config Values GET/POST Handlers

**Files:**
- Create: `processor/internal/api/config_values.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Create config_values.go**

```go
package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
		configDir := filepath.Join(deps.Cfg.BaseDir, "config")
		if err := config.SaveOverrides(configDir, updates); err != nil {
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
```

**Note:** The `validateUpdates` function needs `fmt` imported. Add it to the import block.

- [ ] **Step 2: Register routes in main.go**

In `processor/cmd/processor/main.go`, add the config routes. Find the appropriate location near the other config endpoints:

```go
configDeps := api.ConfigDeps{
	Cfg:       cfg,
	ConfigDir: filepath.Join(cfg.BaseDir, "config"),
	ReloadFn: func() {
		// Re-run computed config fields after override changes
		// (delegated admin maps, etc.)
	},
}
apiGroup.GET("/config/schema", api.HandleConfigSchema())
apiGroup.GET("/config/values", api.HandleConfigValues(configDeps))
apiGroup.POST("/config/values", api.HandleConfigSave(configDeps))
```

- [ ] **Step 3: Build and verify**

```bash
cd processor && go build ./cmd/processor/
```

- [ ] **Step 4: Commit**

```bash
git add processor/internal/api/config_values.go processor/cmd/processor/main.go
git commit -m "feat: add config values GET/POST endpoints with override save"
```

---

## Task 4: Batch Resolution Endpoint

**Files:**
- Create: `processor/internal/api/config_resolve.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Create config_resolve.go**

```go
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jellydator/ttlcache/v3"
)

// ResolveDeps holds dependencies for the resolve handler.
type ResolveDeps struct {
	DiscordSession *discordgo.Session // nil if Discord not configured
	TelegramAPI    *tgbotapi.BotAPI   // nil if Telegram not configured
	Cache          *ttlcache.Cache[string, any]
}

// NewResolveCache creates a ttlcache for resolved IDs with 10 minute TTL.
func NewResolveCache() *ttlcache.Cache[string, any] {
	cache := ttlcache.New[string, any](
		ttlcache.WithTTL[string, any](10 * time.Minute),
	)
	go cache.Start()
	return cache
}

// HandleResolve batch-resolves Discord/Telegram IDs to names.
// POST /api/resolve
func HandleResolve(deps ResolveDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Discord *struct {
				Users    []string `json:"users"`
				Roles    []string `json:"roles"`
				Channels []string `json:"channels"`
				Guilds   []string `json:"guilds"`
			} `json:"discord"`
			Telegram *struct {
				Chats []string `json:"chats"`
			} `json:"telegram"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		result := gin.H{"status": "ok"}

		// Discord resolution
		if req.Discord != nil && deps.DiscordSession != nil {
			discord := make(map[string]any)

			if len(req.Discord.Users) > 0 {
				users := make(map[string]any)
				for _, id := range req.Discord.Users {
					if resolved := resolveDiscordUser(deps, id); resolved != nil {
						users[id] = resolved
					}
				}
				discord["users"] = users
			}

			if len(req.Discord.Roles) > 0 {
				roles := make(map[string]any)
				for _, id := range req.Discord.Roles {
					if resolved := resolveDiscordRole(deps, id); resolved != nil {
						roles[id] = resolved
					}
				}
				discord["roles"] = roles
			}

			if len(req.Discord.Channels) > 0 {
				channels := make(map[string]any)
				for _, id := range req.Discord.Channels {
					if resolved := resolveDiscordChannel(deps, id); resolved != nil {
						channels[id] = resolved
					}
				}
				discord["channels"] = channels
			}

			if len(req.Discord.Guilds) > 0 {
				guilds := make(map[string]any)
				for _, id := range req.Discord.Guilds {
					if resolved := resolveDiscordGuild(deps, id); resolved != nil {
						guilds[id] = resolved
					}
				}
				discord["guilds"] = guilds
			}

			result["discord"] = discord
		}

		// Telegram resolution
		if req.Telegram != nil && deps.TelegramAPI != nil {
			telegram := make(map[string]any)

			if len(req.Telegram.Chats) > 0 {
				chats := make(map[string]any)
				for _, id := range req.Telegram.Chats {
					if resolved := resolveTelegramChat(deps, id); resolved != nil {
						chats[id] = resolved
					}
				}
				telegram["chats"] = chats
			}

			result["telegram"] = telegram
		}

		c.JSON(http.StatusOK, result)
	}
}

func cached[T any](deps ResolveDeps, key string, fetch func() T) T {
	if item := deps.Cache.Get(key); item != nil {
		return item.Value().(T)
	}
	val := fetch()
	deps.Cache.Set(key, val, ttlcache.DefaultTTL)
	return val
}

func resolveDiscordUser(deps ResolveDeps, id string) map[string]any {
	return cached(deps, "discord:user:"+id, func() map[string]any {
		user, err := deps.DiscordSession.User(id)
		if err != nil {
			return nil
		}
		result := map[string]any{"name": user.Username}
		if user.GlobalName != "" {
			result["globalName"] = user.GlobalName
		}
		return result
	})
}

func resolveDiscordRole(deps ResolveDeps, id string) map[string]any {
	return cached(deps, "discord:role:"+id, func() map[string]any {
		// Search all guilds the bot is in
		for _, guild := range deps.DiscordSession.State.Guilds {
			roles, err := deps.DiscordSession.GuildRoles(guild.ID)
			if err != nil {
				continue
			}
			for _, r := range roles {
				if r.ID == id {
					guildName := guild.Name
					if guildName == "" {
						if g, err := deps.DiscordSession.Guild(guild.ID); err == nil {
							guildName = g.Name
						}
					}
					return map[string]any{
						"name":    r.Name,
						"guild":   guildName,
						"guildId": guild.ID,
					}
				}
			}
		}
		return nil
	})
}

func resolveDiscordChannel(deps ResolveDeps, id string) map[string]any {
	return cached(deps, "discord:channel:"+id, func() map[string]any {
		ch, err := deps.DiscordSession.Channel(id)
		if err != nil {
			return nil
		}
		result := map[string]any{
			"name": ch.Name,
			"type": channelTypeName(ch.Type),
		}
		if ch.GuildID != "" {
			if g, err := deps.DiscordSession.Guild(ch.GuildID); err == nil {
				result["guild"] = g.Name
				result["guildId"] = ch.GuildID
			}
		}
		if ch.ParentID != "" {
			if parent, err := deps.DiscordSession.Channel(ch.ParentID); err == nil {
				result["categoryName"] = parent.Name
			}
		}
		return result
	})
}

func resolveDiscordGuild(deps ResolveDeps, id string) map[string]any {
	return cached(deps, "discord:guild:"+id, func() map[string]any {
		g, err := deps.DiscordSession.Guild(id)
		if err != nil {
			return nil
		}
		return map[string]any{"name": g.Name}
	})
}

func resolveTelegramChat(deps ResolveDeps, id string) map[string]any {
	return cached(deps, "telegram:chat:"+id, func() map[string]any {
		chatID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil
		}
		chatCfg := tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}
		chat, err := deps.TelegramAPI.GetChat(chatCfg)
		if err != nil {
			return nil
		}
		name := chat.FirstName
		if chat.LastName != "" {
			name += " " + chat.LastName
		}
		if name == "" {
			name = chat.Title
		}
		if name == "" {
			name = chat.UserName
		}
		result := map[string]any{"name": name, "type": string(chat.Type)}
		return result
	})
}

func channelTypeName(t discordgo.ChannelType) string {
	switch t {
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	case discordgo.ChannelTypeGuildNews:
		return "news"
	case discordgo.ChannelTypeGuildStageVoice:
		return "stage"
	default:
		return fmt.Sprintf("type_%d", t)
	}
}
```

- [ ] **Step 2: Register route and wire dependencies in main.go**

```go
// Resolution cache (shared across requests)
resolveCache := api.NewResolveCache()

// ... after Discord and Telegram bots are initialized:
resolveDeps := api.ResolveDeps{
	Cache: resolveCache,
}
// Set DiscordSession if available (check how the discord bot exposes its session)
if discordBot != nil {
	resolveDeps.DiscordSession = discordBot.Session()
}
// Set TelegramAPI if available
if telegramBot != nil {
	resolveDeps.TelegramAPI = telegramBot.API()
}

apiGroup.POST("/resolve", api.HandleResolve(resolveDeps))
```

**Note for implementing agent:** Check how the Discord and Telegram bots expose their sessions. The Discord bot likely has a `Session()` method returning `*discordgo.Session`. The Telegram bot needs an `API()` accessor returning `*tgbotapi.BotAPI` — add one if it doesn't exist.

- [ ] **Step 3: Build and verify**

```bash
cd processor && go build ./cmd/processor/
```

- [ ] **Step 4: Commit**

```bash
git add processor/internal/api/config_resolve.go processor/cmd/processor/main.go
git commit -m "feat: add batch ID resolution endpoint with ttlcache"
```

---

## Task 5: Update API.md Documentation

**Files:**
- Modify: `API.md`

- [ ] **Step 1: Add config editor endpoints to API.md**

Add a new "Config Editor" section after the existing "DTS Editor" section:

```markdown
## Config Editor

### GET /api/config/schema

Returns the config schema with field metadata for the editor. Excludes TOML-only fields (database, tokens, bind addresses).

Each field includes: `name`, `type` (string/int/float/bool/string[]/select/map), `default`, `description`, `hotReload`, and optional `options` (for select), `resolve` (ID resolution hint), `dependsOn` (visibility dependency), `sensitive` (masked in values).

### GET /api/config/values

Returns current merged config values (TOML + overrides). Only web-editable fields.

| Parameter | Description |
|-----------|-------------|
| `section` | (optional) Return only this section |

### POST /api/config/values

Save config changes. Partial updates — only changed fields. Writes to `config/overrides.json`. Hot-reloadable settings are applied immediately.

Response includes `restart_required` (bool) and `restart_fields` (list of fields that need restart).

### POST /api/resolve

Batch resolve Discord/Telegram IDs to human-readable names. Results cached for 10 minutes.

Request: `{"discord": {"users": [...], "roles": [...], "channels": [...], "guilds": [...]}, "telegram": {"chats": [...]}}`
```

- [ ] **Step 2: Commit**

```bash
git add API.md
git commit -m "docs: add config editor and resolve endpoints to API.md"
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Override file load/save/apply | `config/overrides.go`, `config/config.go` |
| 2 | Config schema definitions | `api/config_schema.go` |
| 3 | Config values GET/POST | `api/config_values.go`, `main.go` |
| 4 | Batch ID resolution | `api/config_resolve.go`, `main.go` |
| 5 | API documentation | `API.md` |

**Important notes for the implementing agent:**
- Task 2 (schema) is the largest — must include ALL sections from the config inventory. Use config.example.toml comments as descriptions. Set `resolve` hints on fields containing Discord/Telegram IDs.
- The `cached` generic function in Task 4 requires Go 1.18+. If the project uses an older version, replace with a non-generic version.
- The Telegram bot may not have an `API()` accessor — check `processor/internal/telegrambot/bot.go` and add one if needed (`func (b *Bot) API() *tgbotapi.BotAPI { return b.api }`).
- The Discord bot's `Session()` accessor should already exist — verify at `processor/internal/discordbot/bot.go`.
- For the hot-reload `ReloadFn` in Task 3, check what the existing `/api/reload` does and wire the same function. This likely calls `state.Load(stateMgr, database)` or similar.
