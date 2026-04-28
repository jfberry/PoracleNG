package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ConfigFieldDef describes a single config field for the editor.
type ConfigFieldDef struct {
	Name        string               `json:"name"`
	Type        string               `json:"type"` // string, int, float, bool, string[], color[], select, map
	Default     any                  `json:"default,omitempty"`
	Description string               `json:"description"`
	HotReload   bool                 `json:"hotReload,omitempty"`
	Sensitive   bool                 `json:"sensitive,omitempty"`
	Deprecated  bool                 `json:"deprecated,omitempty"`  // editor should warn / hide unless already set
	Advanced    bool                 `json:"advanced,omitempty"`    // editor hides behind "show advanced" toggle
	HideDefault bool                 `json:"hideDefault,omitempty"` // editor should not pre-fill the default
	// Nullable marks a scalar field (typically bool) as tri-state: null is a
	// meaningful third value distinct from the zero value, and the backend
	// relies on the distinction — usually to layer per-row overrides on top
	// of a base/default entry. The editor MUST render null as a distinct
	// "inherit / unset" state and preserve null on save (not coerce to false
	// or the type's zero value).
	Nullable    bool                 `json:"nullable,omitempty"`
	MinLength   int                  `json:"minLength,omitempty"`   // for arrays: minimum number of entries
	MaxLength   int                  `json:"maxLength,omitempty"`   // for arrays: maximum number of entries
	Resolve     string               `json:"resolve,omitempty"`
	Options     []ConfigSelectOption `json:"options,omitempty"`
	DependsOn   *ConfigDependency    `json:"dependsOn,omitempty"`
}

// ConfigSelectOption is a constrained option for select-type fields.
type ConfigSelectOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Deprecated  bool   `json:"deprecated,omitempty"`
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

// configSchema defines all web-editable config sections, fields, types, defaults, and metadata.
// Built from processor/internal/config/config.go struct definitions and config/config.example.toml comments.
// Excluded: [processor], [database], [database.scanner], tokens, API keys, bearer tokens.
var configSchema = []ConfigSection{
	// ---- general ----
	{
		Name:  "general",
		Title: "General Settings",
		Fields: []ConfigFieldDef{
			{Name: "locale", Type: "string", Default: "en", Description: "Default language for new users and system messages (e.g., en, fr, de, it, ru)", HotReload: true},
			{Name: "role_check_mode", Type: "select", Default: "ignore", Description: "Action when a user loses their required Discord/Telegram role", HotReload: true, Options: []ConfigSelectOption{
				{Value: "ignore", Label: "Ignore", Description: "Log the event but take no action — user keeps their registration"},
				{Value: "disable-user", Label: "Disable User", Description: "Set admin_disable flag, remove subscription roles, send goodbye message — user must re-register"},
				{Value: "delete", Label: "Delete", Description: "Permanently remove all tracking data and human record"},
			}},
			{Name: "default_template_name", Type: "string", Default: "1", Description: "Default DTS template ID for new tracking rules", HotReload: true},
			{Name: "disabled_commands", Type: "string[]", Default: []string{}, Description: "Commands to disable globally (e.g., [\"lure\", \"nest\"])", HotReload: true},
			{Name: "rdm_url", Type: "string", Default: "", Description: "RDM map instance URL for {{rdmUrl}} in DTS templates", HotReload: true, Deprecated: true},
			{Name: "react_map_url", Type: "string", Default: "", Description: "ReactMap instance URL for {{reactMapUrl}} in DTS templates", HotReload: true},
			{Name: "rocket_mad_url", Type: "string", Default: "", Description: "RocketMAD instance URL for {{rocketMadUrl}} in DTS templates", HotReload: true, Deprecated: true},
			{Name: "img_url", Type: "string", Default: "https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS", Description: "Base URL for pokemon icon images (uicons repository)", HotReload: false},
			{Name: "img_url_alt", Type: "string", Default: "", Description: "Alternative icon URL for {{imgUrlAlt}} in DTS templates", HotReload: false},
			{Name: "sticker_url", Type: "string", Default: "https://raw.githubusercontent.com/bbdoc/tgUICONS/main/Shuffle", Description: "Base URL for Telegram sticker icons (webp format)", HotReload: false},
			{Name: "request_shiny_images", Type: "bool", Default: false, Description: "Request shiny variants from icon repositories", HotReload: false},
			{Name: "populate_pokestop_name", Type: "bool", Default: false, Description: "Look up nearby pokestop names from scanner database", HotReload: false},
			{Name: "alert_minimum_time", Type: "int", Default: 120, Description: "Minimum seconds before expiry — alerts with less time remaining are dropped", HotReload: true},
			{Name: "ignore_long_raids", Type: "bool", Default: false, Description: "Skip raids/eggs with time-to-hatch over 47 minutes to reduce event spam", HotReload: true},
			{Name: "shortlink_provider", Type: "select", Default: "", Description: "URL shortener for <S< >S> markers in DTS templates", HotReload: false, Options: []ConfigSelectOption{
				{Value: "", Label: "None", Description: "No URL shortening"},
				{Value: "shlink", Label: "Shlink", Description: "Self-hosted Shlink instance"},
				{Value: "hideuri", Label: "HideURI", Description: "Not currently supported in PoracleNG", Deprecated: true},
				{Value: "yourls", Label: "YOURLS", Description: "Not currently supported in PoracleNG", Deprecated: true},
			}},
			{Name: "shortlink_provider_url", Type: "string", Default: "", Description: "Shortlink service URL", HotReload: false, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "shortlink_provider_key", Type: "string", Default: "", Description: "Shortlink API key", HotReload: false, Sensitive: true, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "shortlink_provider_domain", Type: "string", Default: "", Description: "Shortlink custom domain override", HotReload: false, DependsOn: &ConfigDependency{Field: "shortlink_provider", Value: "shlink"}},
			{Name: "dts_dictionary", Type: "map", Default: nil, Description: "Custom key-value pairs available in DTS templates via the dtsDict layer", HotReload: true},
			{Name: "disable_pokemon", Type: "bool", Default: false, Description: "Disable pokemon webhook processing entirely", HotReload: false},
			{Name: "disable_raid", Type: "bool", Default: false, Description: "Disable raid webhook processing", HotReload: false},
			{Name: "disable_pokestop", Type: "bool", Default: false, Description: "Disable pokestop/invasion processing from scanner", HotReload: false},
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

	// ---- locale ----
	{
		Name:  "locale",
		Title: "Locale & Formatting",
		Fields: []ConfigFieldDef{
			{Name: "timeformat", Type: "string", Default: "en-gb", Description: "Locale used for time formatting (e.g., en-gb, en-us)", HotReload: true},
			{Name: "time", Type: "string", Default: "LTS", Description: "Time display format using Moment.js tokens (e.g., LTS for localized time with seconds)", HotReload: true},
			{Name: "date", Type: "string", Default: "L", Description: "Date display format using Moment.js tokens (e.g., L for localized short date)", HotReload: true},
			{Name: "address_format", Type: "string", Default: "{{{streetName}}} {{streetNumber}}", Description: "Handlebars template for constructing the {{addr}} tag from geocoding results", HotReload: true},
			{Name: "language", Type: "string", Default: "en", Description: "Secondary language for 'alt' translation fields in DTS (e.g., pokemonNameAlt, moveNameAlt)", HotReload: true},
		},
	},

	// ---- discord ----
	{
		Name:  "discord",
		Title: "Discord",
		Fields: []ConfigFieldDef{
			{Name: "enabled", Type: "bool", Default: true, Description: "Enable the Discord bot — set to false to disable even if a token is configured", HotReload: false},
			{Name: "prefix", Type: "string", Default: "!", Description: "Command prefix for Discord messages (e.g., ! for !track)", HotReload: false},
			{Name: "activity", Type: "string", Default: "PoracleNG", Description: "Bot activity/status text shown in Discord", HotReload: false},
			{Name: "channels", Type: "string[]", Default: []string{}, Description: "Discord channel IDs where !poracle registration is allowed", Resolve: "discord:channel"},
			{Name: "guilds", Type: "string[]", Default: []string{}, Description: "Discord guild (server) IDs that this bot operates in", Resolve: "discord:guild"},
			{Name: "user_role", Type: "string[]", Default: []string{}, Description: "Discord role IDs that automatically grant Poracle access without registration", Resolve: "discord:role"},
			{Name: "admins", Type: "string[]", Default: []string{}, Description: "Discord user IDs with full admin privileges", Resolve: "discord:user", HotReload: true},
			{Name: "check_role", Type: "bool", Default: false, Description: "Periodically verify all users still hold a required role (role_check_mode controls the action)"},
			{Name: "check_role_interval", Type: "int", Default: 6, Description: "Hours between periodic role membership checks", DependsOn: &ConfigDependency{Field: "check_role", Value: true}},
			{Name: "lost_role_message", Type: "string", Default: "", Description: "Custom message sent when a user loses role-based access (empty = no message)", DependsOn: &ConfigDependency{Field: "check_role", Value: true}},
			{Name: "disable_auto_greetings", Type: "bool", Default: false, Description: "Suppress automatic greeting messages when users gain role access"},
			{Name: "dm_log_channel_id", Type: "string", Default: "", Description: "Discord channel ID to log all Poracle DM commands to (empty = disabled)", Resolve: "discord:channel"},
			{Name: "dm_log_channel_deletion_time", Type: "int", Default: 0, Description: "Minutes after which logged DM messages are deleted (0 = never delete)", DependsOn: &ConfigDependency{Field: "dm_log_channel_id", Value: ""}},
			{Name: "unrecognised_command_message", Type: "string", Default: "", Description: "Custom reply to unrecognised commands in DM (empty = default i18n message)"},
			{Name: "unregistered_user_message", Type: "string", Default: "", Description: "Custom reply to unregistered users (empty = shrug emoji)"},
			{Name: "iv_colors", Type: "color[]", Default: []string{"#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"}, Description: "Six hex color codes for pokemon IV ranking tiers (0-5 stars). Must contain exactly 6 entries.", MinLength: 6, MaxLength: 6},
			{Name: "upload_embed_images", Type: "bool", Default: false, Description: "Download and re-upload embed images directly to Discord CDN"},
			{Name: "message_delete_delay", Type: "int", Default: 0, Description: "Extra milliseconds added to message clean TTH for channel messages"},
			{Name: "user_tracking_admins", Type: "string[]", Default: []string{}, Description: "User or role IDs that can manage other users' tracking via user: override", Resolve: "discord:user|role", HotReload: true},
			{Name: "command_security", Type: "map", Default: nil, Description: "Map of command name to array of Discord role or user IDs allowed to use that command", Resolve: "discord:user|role"},
		},
		Tables: []ConfigTableDef{
			{
				Name:        "delegated_admins",
				Title:       "Delegated Channel Admins",
				Description: "Grant users/roles admin rights over specific channels, guilds, or categories",
				Fields: []ConfigFieldDef{
					{Name: "target", Type: "string", Description: "Channel, guild, or category ID to delegate", Resolve: "discord:target"},
					{Name: "admins", Type: "string[]", Description: "User or role IDs who can admin the target", Resolve: "discord:user|role"},
				},
			},
			{
				Name:        "webhook_admins",
				Title:       "Webhook Admins",
				Description: "Grant users admin rights over specific named webhooks",
				Fields: []ConfigFieldDef{
					{Name: "target", Type: "string", Description: "Webhook name to delegate"},
					{Name: "admins", Type: "string[]", Description: "User IDs who can admin the webhook", Resolve: "discord:user"},
				},
			},
			{
				Name:        "role_subscriptions",
				Title:       "Role Subscriptions",
				Description: "Self-service role assignment via !role command — users can add/remove roles from these lists",
				Fields: []ConfigFieldDef{
					{Name: "guild", Type: "string", Description: "Guild ID this subscription applies to", Resolve: "discord:guild"},
					{Name: "roles", Type: "map", Description: "Independent roles users can freely add/remove (name → role ID)", Resolve: "discord:role"},
					{Name: "exclusive_roles", Type: "map[]", Description: "Array of exclusive role groups — each group is a map of name → role ID, user can only hold one role per group", Resolve: "discord:role"},
				},
			},
		},
	},

	// ---- telegram ----
	{
		Name:  "telegram",
		Title: "Telegram",
		Fields: []ConfigFieldDef{
			{Name: "enabled", Type: "bool", Default: false, Description: "Enable the Telegram bot — set to false to disable even if a token is configured", HotReload: false},
			{Name: "channels", Type: "string[]", Default: []string{}, Description: "Telegram group/channel IDs where registration is allowed", Resolve: "telegram:chat"},
			{Name: "admins", Type: "string[]", Default: []string{}, Description: "Telegram user IDs with full admin privileges", Resolve: "telegram:chat", HotReload: true},
			{Name: "check_role", Type: "bool", Default: false, Description: "Periodically verify all users still belong to a registration group"},
			{Name: "check_role_interval", Type: "int", Default: 6, Description: "Hours between periodic membership checks", DependsOn: &ConfigDependency{Field: "check_role", Value: true}},
			{Name: "bot_welcome_text", Type: "string", Default: "You are now registered with Poracle", Description: "DM sent to user upon successful registration"},
			{Name: "bot_goodbye_message", Type: "string", Default: "", Description: "DM sent when user loses access through reconciliation", DependsOn: &ConfigDependency{Field: "check_role", Value: true}},
			{Name: "group_welcome_text", Type: "string", Default: "Welcome {user}, remember to click on me and 'start bot' to be able to receive messages", Description: "Message posted in channel when someone registers — {user} is replaced with mention"},
			{Name: "unregistered_user_message", Type: "string", Default: "", Description: "Custom reply to unregistered users (empty = shrug emoji)"},
			{Name: "unrecognised_command_message", Type: "string", Default: "", Description: "Custom reply to unrecognised commands (empty = default i18n message)"},
			{Name: "register_on_start", Type: "bool", Default: false, Description: "Automatically register users when they send /start to the bot"},
			{Name: "disable_auto_greetings", Type: "bool", Default: false, Description: "Suppress automatic greeting messages on registration"},
			{Name: "user_tracking_admins", Type: "string[]", Default: []string{}, Description: "User IDs that can manage other users' tracking", Resolve: "telegram:chat"},
		},
		Tables: []ConfigTableDef{
			{
				Name:        "delegated_admins",
				Title:       "Delegated Channel Admins",
				Description: "Grant users admin rights over specific Telegram groups/channels",
				Fields: []ConfigFieldDef{
					{Name: "target", Type: "string", Description: "Group or channel ID to delegate"},
					{Name: "admins", Type: "string[]", Description: "User IDs who can admin the target", Resolve: "telegram:chat"},
				},
			},
		},
	},

	// ---- geofence ----
	{
		Name:  "geofence",
		Title: "Geofence",
		Fields: []ConfigFieldDef{
			{Name: "paths", Type: "string[]", Default: []string{"geofences/geofence.json"}, Description: "Paths to geofence JSON/GeoJSON files. Each entry must be a relative path under the config directory (e.g., \"geofences/london.json\") or an http(s):// URL. Validated at startup — invalid paths log a warning."},
			{Name: "default_name", Type: "string", Default: "city", Description: "Default prefix for unnamed fences (e.g., \"city\" produces city1, city2)"},
		},
	},

	// ---- geofence.koji ----
	{
		Name:  "geofence.koji",
		Title: "Koji Geofence Source",
		Fields: []ConfigFieldDef{
			{Name: "bearer_token", Type: "string", Default: "", Description: "Koji API bearer token (required for HTTP geofence downloads from Koji)", Sensitive: true, HotReload: false},
		},
	},

	// ---- pvp ----
	{
		Name:  "pvp",
		Title: "PVP Settings",
		Fields: []ConfigFieldDef{
			{Name: "data_source", Type: "select", Default: "webhook", Description: "Source of PVP rank data", Deprecated: true, Advanced: true, Options: []ConfigSelectOption{
				{Value: "webhook", Label: "Webhook", Description: "PVP data comes from Golbat webhooks (only supported source)"},
				{Value: "ohbem", Label: "Ohbem", Description: "Not currently supported in PoracleNG", Deprecated: true},
			}},
			{Name: "level_caps", Type: "int[]", Default: []int{50}, Description: "Level caps to include in PVP rank calculations (e.g., [50] or [50, 51])"},
			{Name: "include_mega_evolution", Type: "bool", Default: false, Description: "Include mega evolutions in PVP rank calculations"},
			{Name: "evolution_direct_tracking", Type: "bool", Default: false, Description: "Allow users to track PVP evolutions directly (e.g., tracking Vaporeon matches an Eevee)"},
			{Name: "filter_by_track", Type: "bool", Default: false, Description: "Auto-filter PVP display listings by the user's tracking requirements"},
			{Name: "force_min_cp", Type: "bool", Default: false, Description: "Enforce minimum CP filters even when not explicitly set by the user"},
			{Name: "filter_max_rank", Type: "int", Default: 10, Description: "Maximum PVP rank users can track — prevents unexpectedly broad tracking"},
			{Name: "filter_great_min_cp", Type: "int", Default: 1400, Description: "Minimum CP floor for Great League PVP tracking commands"},
			{Name: "filter_ultra_min_cp", Type: "int", Default: 2350, Description: "Minimum CP floor for Ultra League PVP tracking commands"},
			{Name: "filter_little_min_cp", Type: "int", Default: 450, Description: "Minimum CP floor for Little League PVP tracking commands"},
			{Name: "display_max_rank", Type: "int", Default: 10, Description: "Maximum PVP rank shown in DTS template PVP ranking lists"},
			{Name: "display_great_min_cp", Type: "int", Default: 1400, Description: "Minimum CP for Great League entries in DTS PVP display"},
			{Name: "display_ultra_min_cp", Type: "int", Default: 2350, Description: "Minimum CP for Ultra League entries in DTS PVP display"},
			{Name: "display_little_min_cp", Type: "int", Default: 450, Description: "Minimum CP for Little League entries in DTS PVP display"},
			{Name: "pvp_query_max_rank", Type: "int", Default: 100, Description: "Maximum rank included in PVP queries from webhook data (defaults to filter_max_rank)"},
		},
	},

	// ---- weather ----
	{
		Name:  "weather",
		Title: "Weather",
		Fields: []ConfigFieldDef{
			{Name: "enable_inference", Type: "bool", Default: false, Description: "Infer weather conditions from pokemon boost patterns"},
			{Name: "change_alert", Type: "bool", Default: false, Description: "Enable weather change alert notifications"},
			{Name: "show_altered_pokemon", Type: "bool", Default: false, Description: "Track weather-changed pokemon to show in DTS weather alerts"},
			{Name: "show_altered_pokemon_max_count", Type: "int", Default: 10, Description: "Maximum number of changed pokemon per weather alert", DependsOn: &ConfigDependency{Field: "show_altered_pokemon", Value: true}},
			{Name: "show_altered_pokemon_static_map", Type: "bool", Default: false, Description: "Show weather-changed pokemon on the static map tile", DependsOn: &ConfigDependency{Field: "show_altered_pokemon", Value: true}},
			{Name: "enable_forecast", Type: "bool", Default: false, Description: "Enable AccuWeather forecast for next-hour weather prediction"},
			{Name: "accuweather_api_keys", Type: "string[]", Default: []string{}, Description: "AccuWeather API keys (rotated through array as quotas are exhausted)", Sensitive: true, HotReload: false, DependsOn: &ConfigDependency{Field: "enable_forecast", Value: true}},
			{Name: "accuweather_day_quota", Type: "int", Default: 500, Description: "Maximum AccuWeather API calls per key per day (free tier is 50/day, paid tiers are higher)", DependsOn: &ConfigDependency{Field: "enable_forecast", Value: true}},
			{Name: "forecast_refresh_interval", Type: "int", Default: 8, Description: "Hours between forecast API calls per weather cell", DependsOn: &ConfigDependency{Field: "enable_forecast", Value: true}},
			{Name: "local_first_fetch_hod", Type: "int", Default: 3, Description: "First forecast fetch hour of the day in local time (e.g., 3 = 3am)", DependsOn: &ConfigDependency{Field: "enable_forecast", Value: true}},
			{Name: "smart_forecast", Type: "bool", Default: false, Description: "Pull forecast data on demand if no cached data exists for a cell", DependsOn: &ConfigDependency{Field: "enable_forecast", Value: true}},
		},
	},

	// ---- area_security ----
	{
		Name:  "area_security",
		Title: "Area Security",
		Fields: []ConfigFieldDef{
			{Name: "enabled", Type: "bool", Default: false, Description: "Enable community-based area security — restricts access based on role/group membership"},
			{Name: "strict_locations", Type: "bool", Default: false, Description: "Enforce that alerts originate within the community's location_fence for every alert", DependsOn: &ConfigDependency{Field: "enabled", Value: true}},
		},
		Tables: []ConfigTableDef{
			{
				Name:        "communities",
				Title:       "Communities",
				Description: "Each community defines a group of users with shared area access, registration channels, and role requirements",
				Fields: []ConfigFieldDef{
					{Name: "name", Type: "string", Description: "Community identifier (e.g., \"newyork\")"},
					{Name: "allowed_areas", Type: "string[]", Description: "Geofence area names users in this community can select with !area", Resolve: "geofence:area"},
					{Name: "location_fence", Type: "string[]", Description: "Geofence used for strict_locations enforcement — alerts outside this fence are blocked", Resolve: "geofence:area"},
					{Name: "discord_channels", Type: "string[]", Description: "Discord channel IDs where !poracle registers users into this community", Resolve: "discord:channel"},
					{Name: "discord_user_role", Type: "string[]", Description: "Discord role IDs that grant membership in this community", Resolve: "discord:role"},
					{Name: "telegram_channels", Type: "string[]", Description: "Telegram group/channel IDs that qualify users for this community", Resolve: "telegram:chat"},
					{Name: "telegram_admins", Type: "string[]", Description: "Telegram user IDs with admin rights for this community", Resolve: "telegram:chat"},
				},
			},
		},
	},

	// ---- geocoding ----
	{
		Name:  "geocoding",
		Title: "Geocoding & Static Maps",
		Fields: []ConfigFieldDef{
			{Name: "provider", Type: "select", Default: "none", Description: "Address geocoding provider for reverse/forward lookups", Options: []ConfigSelectOption{
				{Value: "none", Label: "None", Description: "Disable address geocoding — {{addr}} will be empty"},
				{Value: "nominatim", Label: "Nominatim", Description: "Self-hosted Nominatim instance (recommended)"},
				{Value: "google", Label: "Google", Description: "Google Maps Geocoding API (requires API key in config.toml)"},
			}},
			{Name: "provider_url", Type: "string", Default: "", Description: "Nominatim instance URL for address lookups", DependsOn: &ConfigDependency{Field: "provider", Value: "nominatim"}},
			{Name: "geocoding_key", Type: "string[]", Default: []string{}, Description: "Google Geocoding API keys (rotated through array)", Sensitive: true, HotReload: true, DependsOn: &ConfigDependency{Field: "provider", Value: "google"}},
			{Name: "cache_detail", Type: "int", Default: 3, Description: "Decimal places of lat/lon for geocoding cache key rounding (3 or 4 for 100x more detail)"},
			{Name: "forward_only", Type: "bool", Default: false, Description: "Disable reverse geocoding — only forward lookups will be performed"},
			{Name: "static_provider", Type: "select", Default: "none", Description: "Static map tile provider for generating map images in alerts", Options: []ConfigSelectOption{
				{Value: "none", Label: "None", Description: "Disable static map tiles"},
				{Value: "tileservercache", Label: "TileserverCache", Description: "SwiftTileserverCache (recommended, self-hosted)"},
				{Value: "google", Label: "Google", Description: "Google Static Maps API"},
				{Value: "osm", Label: "OSM", Description: "OpenStreetMap tile rendering"},
				{Value: "mapbox", Label: "Mapbox", Description: "Mapbox Static Images API"},
			}},
			{Name: "static_provider_url", Type: "string", Default: "", Description: "Static map tile provider URL"},
			{Name: "static_internal_url", Type: "string", Default: "", Description: "Private URL the processor uses for its own tileserver calls (render, pregenerate fetch, upload-images pre-fetch). Leave empty to reuse static_provider_url.", Advanced: true},
			{Name: "static_key", Type: "string[]", Default: []string{}, Description: "API keys for the static map provider (Google/Mapbox), rotated through array", Sensitive: true, HotReload: true},
			{Name: "width", Type: "int", Default: 320, Description: "Static map image width in pixels"},
			{Name: "height", Type: "int", Default: 200, Description: "Static map image height in pixels"},
			{Name: "zoom", Type: "int", Default: 15, Description: "Static map default zoom level"},
			{Name: "type", Type: "string", Default: "klokantech-basic", Description: "Map style/type for the static map provider"},
			{Name: "day_style", Type: "string", Default: "", Description: "TileserverCache style name for daytime maps"},
			{Name: "dawn_style", Type: "string", Default: "", Description: "TileserverCache style name for dawn maps"},
			{Name: "dusk_style", Type: "string", Default: "", Description: "TileserverCache style name for dusk maps"},
			{Name: "night_style", Type: "string", Default: "", Description: "TileserverCache style name for nighttime maps"},
		},
		Tables: []ConfigTableDef{
			{
				Name:        "tileserver_settings",
				Title:       "Tileserver Settings",
				Description: "Per-alert-type tile overrides. \"default\" applies to any alert type without its own entry. Known maptypes: default, monster, raid, pokestop, quest, weather, location, nest, gym.",
				Fields: []ConfigFieldDef{
					{Name: "maptype", Type: "string", Description: "Alert type this entry applies to (e.g. default, monster, raid, gym)"},
					{Name: "type", Type: "select", Default: "staticMap", Description: "TileserverCache endpoint to call", Options: []ConfigSelectOption{
						{Value: "staticMap", Label: "staticMap", Description: "Single-marker endpoint"},
						{Value: "multiStaticMap", Label: "multiStaticMap", Description: "Multi-marker endpoint (required for overlays like pokestops/gyms)"},
					}},
					{Name: "include_stops", Type: "bool", Nullable: true, Description: "Overlay nearby pokestops and gyms on the tile (requires multiStaticMap type and a scanner DB). On non-default rows, leave unset to inherit from the default entry."},
					{Name: "width", Type: "int", Default: 500, Description: "Image width in pixels"},
					{Name: "height", Type: "int", Default: 250, Description: "Image height in pixels"},
					{Name: "zoom", Type: "int", Default: 15, Description: "Map zoom level"},
					{Name: "pregenerate", Type: "bool", Nullable: true, Description: "Pregenerate the tile on the tileserver before sending the URL. On non-default rows, leave unset to inherit from the default entry."},
					{Name: "ttl", Type: "int", Default: 0, Description: "Tile cache TTL in seconds (0 = use global tileserver_pregen_ttl)"},
				},
			},
		},
	},

	// ---- tuning ----
	{
		Name:  "tuning",
		Title: "Performance Tuning",
		Fields: []ConfigFieldDef{
			{Name: "reload_interval_secs", Type: "int", Default: 60, Description: "Seconds between periodic database state reloads", Advanced: true},
			{Name: "encounter_cache_ttl", Type: "int", Default: 3600, Description: "Seconds to cache encounter data for deduplication", Advanced: true},
			{Name: "worker_pool_size", Type: "int", Default: 4, Description: "Number of webhook processing goroutines", Advanced: true},
			{Name: "batch_size", Type: "int", Default: 50, Description: "Maximum webhooks processed per batch", Advanced: true},
			{Name: "flush_interval_millis", Type: "int", Default: 100, Description: "Milliseconds between batch flushes when batch is not full", Advanced: true},
			{Name: "tileserver_concurrency", Type: "int", Default: 2, Description: "Number of tile worker goroutines for parallel tileserver POSTs", Advanced: true},
			{Name: "tileserver_timeout", Type: "int", Default: 10000, Description: "HTTP timeout per tile POST in milliseconds", Advanced: true},
			{Name: "tileserver_failure_threshold", Type: "int", Default: 5, Description: "Consecutive tile errors before circuit breaker opens", Advanced: true},
			{Name: "tileserver_cooldown_ms", Type: "int", Default: 30000, Description: "Milliseconds the circuit breaker stays open before retrying", Advanced: true},
			{Name: "tileserver_queue_size", Type: "int", Default: 100, Description: "Maximum pending tile requests — overflow uses fallback URL", Advanced: true},
			{Name: "tileserver_deadline", Type: "int", Default: 10000, Description: "Maximum milliseconds a payload waits for its tile before using fallback", Advanced: true},
			{Name: "geocoding_concurrency", Type: "int", Default: 0, Description: "Number of concurrent geocoding worker goroutines (0 = default)", Advanced: true},
			{Name: "geocoding_timeout", Type: "int", Default: 0, Description: "HTTP timeout per geocoding request in milliseconds (0 = default)", Advanced: true},
			{Name: "geocoding_failure_threshold", Type: "int", Default: 0, Description: "Consecutive geocoding errors before circuit breaker opens (0 = default)", Advanced: true},
			{Name: "geocoding_cooldown_ms", Type: "int", Default: 0, Description: "Milliseconds the geocoding circuit breaker stays open (0 = default)", Advanced: true},
			{Name: "render_pool_size", Type: "int", Default: 8, Description: "Number of concurrent DTS render goroutines", Advanced: true},
			{Name: "render_queue_size", Type: "int", Default: 100, Description: "Maximum buffered render jobs before backpressure", Advanced: true},
			{Name: "concurrent_discord_destinations", Type: "int", Default: 10, Description: "Concurrent Discord DM/channel sends per bot", Advanced: true},
			{Name: "concurrent_telegram_destinations", Type: "int", Default: 10, Description: "Concurrent Telegram sends per bot", Advanced: true},
			{Name: "concurrent_discord_webhooks", Type: "int", Default: 10, Description: "Concurrent Discord webhook sends", Advanced: true},
			{Name: "delivery_queue_size", Type: "int", Default: 200, Description: "Maximum buffered delivery jobs", Advanced: true},
			{Name: "validation_timeout_ms", Type: "int", Default: 1500, Description: "External validation hook: per-call HTTP timeout in milliseconds", Advanced: true},
			{Name: "validation_max_concurrent", Type: "int", Default: 16, Description: "External validation hook: cap on parallel validator calls per webhook event", Advanced: true},
		},
	},

	// ---- validation ----
	{
		Name:  "validation",
		Title: "External Validation Hook",
		Fields: []ConfigFieldDef{
			{Name: "url", Type: "string", Default: "", Description: "Validator endpoint POSTed once per matched user (after rate-limit pre-filter, before enrichment). Empty disables the hook entirely. See API.md \"Validation Hook\" for the request/response shape.", Advanced: true},
			{Name: "fail_mode", Type: "select", Default: "open", Description: "Behaviour when the validator times out, errors, or returns non-2xx", Advanced: true, Options: []ConfigSelectOption{
				{Value: "open", Label: "Open (allow)", Description: "Treat failures as success — alerts continue to flow if the validator is down (recommended for soft gating)"},
				{Value: "closed", Label: "Closed (deny)", Description: "Treat failures as deny — alerts are dropped when the validator is unreachable (use for hard licensing where un-validated alerts are unacceptable)"},
			}},
		},
	},

	// ---- alert_limits ----
	{
		Name:  "alert_limits",
		Title: "Alert Rate Limits",
		Fields: []ConfigFieldDef{
			{Name: "timing_period", Type: "int", Default: 240, Description: "Seconds over which alert rate limits are calculated", HotReload: true},
			{Name: "dm_limit", Type: "int", Default: 20, Description: "Maximum messages a user can receive in one timing period", HotReload: true},
			{Name: "channel_limit", Type: "int", Default: 40, Description: "Maximum messages a channel/group can receive in one timing period", HotReload: true},
			{Name: "max_limits_before_stop", Type: "int", Default: 10, Description: "Times a user can hit the rate limit within 24 hours before being stopped", HotReload: true},
			{Name: "disable_on_stop", Type: "bool", Default: false, Description: "Admin-disable stopped users (requires admin to restart) instead of soft stop", HotReload: true},
			{Name: "shame_channel", Type: "string", Default: "", Description: "Discord channel ID to publicly log stopped/disabled users", Resolve: "discord:channel", HotReload: true},
		},
		Tables: []ConfigTableDef{
			{
				Name:        "overrides",
				Title:       "Limit Overrides",
				Description: "Override the default alert limit for specific channels, users, or webhooks",
				Fields: []ConfigFieldDef{
					{Name: "target", Type: "string", Description: "Channel ID, user ID, or webhook name to override", Resolve: "destination"},
					{Name: "limit", Type: "int", Description: "Custom message limit for this target during the timing period"},
				},
			},
		},
	},

	// ---- tracking ----
	{
		Name:  "tracking",
		Title: "Tracking Restrictions",
		Fields: []ConfigFieldDef{
			{Name: "everything_flag_permissions", Type: "select", Default: "allow-any", Description: "How the 'everything' keyword is handled in !track", HotReload: true, Options: []ConfigSelectOption{
				{Value: "deny", Label: "Deny", Description: "Users must track individual pokemon — 'everything' keyword is not available"},
				{Value: "allow-any", Label: "Allow Any", Description: "Unrestricted — recorded as wildcard, users can use 'individually' to expand"},
				{Value: "allow-and-always-individually", Label: "Always Individual", Description: "Allowed but always recorded as individual rows per pokemon"},
				{Value: "allow-and-ignore-individually", Label: "Ignore Individual", Description: "Allowed but 'individually' keyword is hidden from users"},
			}},
			{Name: "default_distance", Type: "int", Default: 0, Description: "Default distance in meters for distance-only tracking when no areas are set (0 = disabled)", HotReload: true},
			{Name: "max_distance", Type: "int", Default: 0, Description: "Maximum allowed tracking circle radius in meters (0 = no limit)", HotReload: true},
			{Name: "enable_gym_battle", Type: "bool", Default: false, Description: "Allow the battle_changes option in !gym tracking command", HotReload: true},
			{Name: "default_user_tracking_level_cap", Type: "int", Default: 0, Description: "Default PVP tracking level cap for new users (0 = use all configured caps)", HotReload: true},
		},
	},

	// ---- reconciliation.discord ----
	{
		Name:  "reconciliation.discord",
		Title: "Discord Reconciliation",
		Fields: []ConfigFieldDef{
			{Name: "update_user_names", Type: "bool", Default: false, Description: "Follow Discord username changes and update stored names"},
			{Name: "remove_invalid_users", Type: "bool", Default: true, Description: "De-register users who lose required roles during periodic role check"},
			{Name: "register_new_users", Type: "bool", Default: false, Description: "Automatically register users who gain required roles during role check"},
			{Name: "update_channel_names", Type: "bool", Default: true, Description: "Keep registered channel names in sync with Discord"},
			{Name: "update_channel_notes", Type: "bool", Default: false, Description: "Update channel notes with guild name and category information"},
			{Name: "unregister_missing_channels", Type: "bool", Default: false, Description: "Remove channels that have been deleted from Discord"},
		},
	},

	// ---- reconciliation.telegram ----
	{
		Name:  "reconciliation.telegram",
		Title: "Telegram Reconciliation",
		Fields: []ConfigFieldDef{
			{Name: "update_user_names", Type: "bool", Default: false, Description: "Follow Telegram username changes and update stored names"},
			{Name: "remove_invalid_users", Type: "bool", Default: true, Description: "Automatically remove users who lose access to registration groups"},
		},
	},

	// ---- stats ----
	{
		Name:  "stats",
		Title: "Rarity Statistics",
		Fields: []ConfigFieldDef{
			{Name: "min_sample_size", Type: "int", Default: 10000, Description: "Minimum total pokemon sightings before rarity is calculated", Advanced: true},
			{Name: "window_hours", Type: "int", Default: 8, Description: "Rolling window hours for rarity and shiny rate statistics", Advanced: true},
			{Name: "refresh_interval_mins", Type: "int", Default: 5, Description: "Minutes between rarity statistic recalculations", Advanced: true},
			{Name: "rarity_group_2_uncommon", Type: "float", Default: 1.0, Description: "Percentage threshold of total sightings for Uncommon rarity group", Advanced: true},
			{Name: "rarity_group_3_rare", Type: "float", Default: 0.5, Description: "Percentage threshold for Rare rarity group", Advanced: true},
			{Name: "rarity_group_4_very_rare", Type: "float", Default: 0.03, Description: "Percentage threshold for Very Rare rarity group", Advanced: true},
			{Name: "rarity_group_5_ultra_rare", Type: "float", Default: 0.01, Description: "Percentage threshold for Ultra Rare rarity group", Advanced: true},
		},
	},

	// ---- fallbacks ----
	{
		Name:  "fallbacks",
		Title: "Fallback URLs",
		Fields: []ConfigFieldDef{
			{Name: "static_map", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/staticMap.png", Description: "Fallback static map image when tile generation fails", Advanced: true, HideDefault: true},
			{Name: "img_url", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/mon.png", Description: "Fallback pokemon icon image", Advanced: true, HideDefault: true},
			{Name: "img_url_weather", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/weather.png", Description: "Fallback weather icon image", Advanced: true, HideDefault: true},
			{Name: "img_url_egg", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/uni.png", Description: "Fallback egg icon image", Advanced: true, HideDefault: true},
			{Name: "img_url_gym", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/gym.png", Description: "Fallback gym icon image", Advanced: true, HideDefault: true},
			{Name: "img_url_pokestop", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png", Description: "Fallback pokestop icon image", Advanced: true, HideDefault: true},
			{Name: "pokestop_url", Type: "string", Default: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png", Description: "Fallback pokestop URL for the pokestop_url DTS field", Advanced: true, HideDefault: true},
		},
	},

	// ---- logging ----
	{
		Name:  "logging",
		Title: "Logging",
		Fields: []ConfigFieldDef{
			{Name: "level", Type: "select", Default: "verbose", Description: "Log verbosity level", Options: []ConfigSelectOption{
				{Value: "debug", Label: "Debug", Description: "Most verbose — includes internal debug details"},
				{Value: "verbose", Label: "Verbose", Description: "Recommended starting point — detailed operational logging"},
				{Value: "info", Label: "Info", Description: "Standard operational messages only"},
				{Value: "warn", Label: "Warn", Description: "Warnings and errors only"},
			}},
			{Name: "file_logging_enabled", Type: "bool", Default: true, Description: "Write log output to a file in addition to console"},
			{Name: "filename", Type: "string", Default: "logs/processor.log", Description: "Log file path (relative to project root)"},
			{Name: "max_size", Type: "int", Default: 50, Description: "Maximum log file size in megabytes before rotation"},
			{Name: "max_age", Type: "int", Default: 7, Description: "Days to keep old rotated log files"},
			{Name: "max_backups", Type: "int", Default: 5, Description: "Number of old rotated log files to keep"},
			{Name: "compress", Type: "bool", Default: true, Description: "Gzip compress rotated log files to save disk space"},
		},
	},

	// ---- webhookLogging ----
	{
		Name:  "webhookLogging",
		Title: "Webhook Logging",
		Fields: []ConfigFieldDef{
			{Name: "enabled", Type: "bool", Default: false, Description: "Log raw webhook bodies received from Golbat (useful for debugging and creating test data)"},
			{Name: "filename", Type: "string", Default: "logs/webhooks.log", Description: "Webhook log file path (relative to project root)"},
			{Name: "max_size", Type: "int", Default: 100, Description: "Maximum log file size in megabytes before rotation"},
			{Name: "max_age", Type: "int", Default: 1, Description: "Days to keep old rotated webhook log files"},
			{Name: "max_backups", Type: "int", Default: 12, Description: "Number of old rotated files to keep (12 = ~12 hours at hourly rotation)"},
			{Name: "compress", Type: "bool", Default: true, Description: "Gzip compress rotated webhook log files"},
			{Name: "rotate_interval", Type: "int", Default: 60, Description: "Minutes between forced log rotations (0 = size-based rotation only)"},
		},
	},

	// ---- ai ----
	{
		Name:  "ai",
		Title: "AI Assistant",
		Fields: []ConfigFieldDef{
			{Name: "enabled", Type: "bool", Default: false, Description: "Enable the AI command assistant (!ask command and NLP suggestions)"},
			{Name: "suggest_on_dm", Type: "bool", Default: false, Description: "Suggest commands for unrecognised DM messages using NLP", DependsOn: &ConfigDependency{Field: "enabled", Value: true}},
		},
	},
}

// HandleConfigSchema returns the config schema for the editor UI.
// GET /api/config/schema
func HandleConfigSchema() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "sections": configSchema})
	}
}
