package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	Processor      ProcessorConfig      `toml:"processor"`
	General        GeneralConfig        `toml:"general"`
	Database       DatabaseConfig       `toml:"database"`
	Geofence       GeofenceConfig       `toml:"geofence"`
	PVP            PVPConfig            `toml:"pvp"`
	Weather        WeatherConfig        `toml:"weather"`
	Tuning         TuningConfig         `toml:"tuning"`
	Stats          StatsConfig          `toml:"stats"`
	Area           AreaConfig           `toml:"area_security"`
	Locale         LocaleConfig         `toml:"locale"`
	Logging        LoggingConfig        `toml:"logging"`
	WebhookLogging WebhookLoggingConfig `toml:"webhookLogging"`
	AlertLimits    AlertLimitsConfig    `toml:"alert_limits"`
	Alerter        AlerterConfig        `toml:"alerter"`
	Discord        DiscordConfig        `toml:"discord"`
	Telegram       TelegramConfig       `toml:"telegram"`
	Reconciliation ReconciliationConfig `toml:"reconciliation"`
	Geocoding      GeocodingConfig      `toml:"geocoding"`
	Fallbacks      FallbacksConfig      `toml:"fallbacks"`
	Tracking       TrackingConfig       `toml:"tracking"`
	AI             AIConfig             `toml:"ai"`

	// BaseDir is the directory containing the config file, used to resolve relative paths.
	BaseDir string `toml:"-"`

	// OverrideStatus describes how config/overrides.json layers on top of
	// config.toml. Populated during Load(). The caller logs it via
	// LogOverrideStatus after logging.Setup has run.
	OverrideStatus *OverrideStatus `toml:"-"`
}

// AIConfig holds settings for the NLP command assistant.
type AIConfig struct {
	Enabled     bool `toml:"enabled"`       // enables !ask command
	SuggestOnDM bool `toml:"suggest_on_dm"` // suggest commands for unrecognised DMs
}

// ReconciliationConfig holds settings from the [reconciliation] section.
type ReconciliationConfig struct {
	Discord  ReconciliationDiscordConfig  `toml:"discord"`
	Telegram ReconciliationTelegramConfig `toml:"telegram"`
}

// ReconciliationDiscordConfig holds settings from [reconciliation.discord].
type ReconciliationDiscordConfig struct {
	UpdateUserNames           bool `toml:"update_user_names"`
	RemoveInvalidUsers        bool `toml:"remove_invalid_users"`
	RegisterNewUsers          bool `toml:"register_new_users"`
	UpdateChannelNames        bool `toml:"update_channel_names"`
	UpdateChannelNotes        bool `toml:"update_channel_notes"`
	UnregisterMissingChannels bool `toml:"unregister_missing_channels"`
}

// ReconciliationTelegramConfig holds settings from [reconciliation.telegram].
type ReconciliationTelegramConfig struct {
	UpdateUserNames    bool `toml:"update_user_names"`
	RemoveInvalidUsers bool `toml:"remove_invalid_users"`
}

// RoleSubscriptionEntry represents a [[discord.role_subscriptions]] TOML entry.
type RoleSubscriptionEntry struct {
	Guild          string              `toml:"guild" json:"guild"`
	Roles          map[string]string   `toml:"roles" json:"roles"`
	ExclusiveRoles []map[string]string `toml:"exclusive_roles" json:"exclusive_roles"`
}

// TrackingConfig holds settings from the [tracking] section.
type TrackingConfig struct {
	EverythingFlagPermissions      string `toml:"everything_flag_permissions"` // "deny", "allow-any", "allow-and-always-individually", "allow-and-ignore-individually"
	DefaultDistance                int    `toml:"default_distance"`
	MaxDistance                    int    `toml:"max_distance"`
	EnableGymBattle                bool   `toml:"enable_gym_battle"`
	DefaultUserTrackingLevelCap    int    `toml:"default_user_tracking_level_cap"`
}

// GeneralConfig holds settings from the [general] section used by the processor
// for map URL generation and other enrichment features.
type GeneralConfig struct {
	Locale               string   `toml:"locale"`                // default language code (e.g. "en", "pl")
	RoleCheckMode        string   `toml:"role_check_mode"`       // "ignore", "disable-user", "delete"
	DefaultTemplateName  any      `toml:"default_template_name"` // default DTS template (typically 1 or "1")
	DisabledCommands     []string `toml:"disabled_commands"`     // list of command names to disable (e.g. ["lure", "nest"])
	RdmURL               string   `toml:"rdm_url"`
	ReactMapURL          string   `toml:"react_map_url"`
	RocketMadURL         string   `toml:"rocket_mad_url"`
	ImgURL               string   `toml:"img_url"`
	ImgURLAlt            string   `toml:"img_url_alt"`
	StickerURL           string   `toml:"sticker_url"`
	RequestShinyImages   bool     `toml:"request_shiny_images"`
	PopulatePokestopName bool     `toml:"populate_pokestop_name"`
	AlertMinimumTime     int            `toml:"alert_minimum_time"`        // seconds before expiry inside which alerts are dropped
	IgnoreLongRaids      bool           `toml:"ignore_long_raids"`         // skip raids/eggs with TTH > 47 minutes
	ShortlinkProvider    string         `toml:"shortlink_provider"`        // "shlink" or empty
	ShortlinkProviderURL string         `toml:"shortlink_provider_url"`    // Shlink instance URL
	ShortlinkProviderKey string         `toml:"shortlink_provider_key"`    // Shlink API key
	ShortlinkDomain      string         `toml:"shortlink_provider_domain"` // Shlink domain override
	DTSDictionary        map[string]any `toml:"dts_dictionary"`            // custom key-value pairs for DTS templates
	AvailableLanguages   map[string]LanguageEntry `toml:"available_languages"` // lang code → {poracle, help}

	// Webhook type disable flags — used by /api/config/poracleWeb to report disabledHooks.
	DisablePokemon   bool `toml:"disable_pokemon"`
	DisableRaid      bool `toml:"disable_raid"`
	DisablePokestop  bool `toml:"disable_pokestop"`
	DisableInvasion  bool `toml:"disable_invasion"`
	DisableLure      bool `toml:"disable_lure"`
	DisableQuest     bool `toml:"disable_quest"`
	DisableWeather   bool `toml:"disable_weather"`
	DisableNest      bool `toml:"disable_nest"`
	DisableGym       bool `toml:"disable_gym"`
	DisableMaxBattle    bool `toml:"disable_max_battle"`
	DisableFortUpdate  bool `toml:"disable_fort_update"`
}

// LanguageEntry defines the command aliases for a language variant.
// Used by available_languages to register language-specific !poracle and !help commands.
type LanguageEntry struct {
	Poracle string `toml:"poracle"` // registration command word (e.g. "dasporacle")
	Help    string `toml:"help"`    // help command word (e.g. "hilfe")
}

type LocaleConfig struct {
	TimeFormat    string `toml:"timeformat"`
	Time          string `toml:"time"`
	Date          string `toml:"date"`
	AddressFormat string `toml:"address_format"`
	Language      string `toml:"language"` // alt language for DTS helpers (pokemonNameAlt, moveNameAlt, etc.) — default "en"
}

type LoggingConfig struct {
	Level              string `toml:"level"`
	LogLevel           string `toml:"log_level"`
	ConsoleLogLevel    string `toml:"console_log_level"`
	FileLoggingEnabled bool   `toml:"file_logging_enabled"`
	Filename           string `toml:"filename"`
	MaxSize            int    `toml:"max_size"`
	MaxAge             int    `toml:"max_age"`
	MaxBackups         int    `toml:"max_backups"`
	Compress           bool   `toml:"compress"`
}

type ProcessorConfig struct {
	Host        string   `toml:"host"`
	Port        int      `toml:"port"`
	IPWhitelist []string `toml:"ip_whitelist"`
	IPBlacklist []string `toml:"ip_blacklist"`
	APISecret   string   `toml:"api_secret"` // API secret for X-Poracle-Secret header authentication
}

// AlerterConfig reads the [alerter] section for backward-compatible api_secret configuration.
type AlerterConfig struct {
	APISecret string `toml:"api_secret"`
}

// DiscordConfig reads the [discord] section for fields the processor needs.
type DiscordConfig struct {
	Enabled                 bool                 `toml:"enabled"` // false = disable bot even if token is set
	Token                   any                  `toml:"token"`   // string or []string
	Prefix                  string               `toml:"prefix"`
	Activity                string               `toml:"activity"` // bot activity/status text
	Channels                []string             `toml:"channels"` // registration channel IDs
	Guilds                  []string             `toml:"guilds"`
	UserRole                []string             `toml:"user_role"`
	CheckRole               bool                 `toml:"check_role"`
	CheckRoleInterval       int                  `toml:"check_role_interval"` // hours between periodic reconciliation
	LostRoleMessage         string               `toml:"lost_role_message"`
	DisableAutoGreetings    bool                 `toml:"disable_auto_greetings"`
	DmLogChannelID              string `toml:"dm_log_channel_id"`
	DmLogChannelDeletionTime    int    `toml:"dm_log_channel_deletion_time"` // minutes, 0 = don't delete
	UnrecognisedCommandMessage  string `toml:"unrecognised_command_message"` // custom reply, overrides i18n
	UnregisteredUserMessage     string `toml:"unregistered_user_message"`    // custom reply, overrides i18n
	IvColors                []string             `toml:"iv_colors"`
	Admins                  []string             `toml:"admins"`
	UploadEmbedImages       bool                 `toml:"upload_embed_images"`
	MessageDeleteDelay      int                  `toml:"message_delete_delay"` // extra ms for clean TTH on channels
	RoleSubscriptions       []RoleSubscriptionEntry `toml:"role_subscriptions"`
	CommandSecurity    map[string][]string `toml:"command_security"`

	// Delegated administration — TOML array-of-tables format
	DelegatedAdmins    []DelegatedAdminEntry `toml:"delegated_admins"`    // [[discord.delegated_admins]]
	WebhookAdmins      []DelegatedAdminEntry `toml:"webhook_admins"`     // [[discord.webhook_admins]]
	UserTrackingAdmins []string              `toml:"user_tracking_admins"`

	// Internal computed maps (populated from TOML entries after load)
	DelegatedAdministration DelegatedAdminConfig `toml:"-"`
}

// DelegatedAdminEntry represents a [[delegated_admins]] TOML array-of-tables entry.
type DelegatedAdminEntry struct {
	Target string   `toml:"target" json:"target"`
	Admins []string `toml:"admins" json:"admins"`
}

// DelegatedAdminConfig is the internal representation used by permissions code.
type DelegatedAdminConfig struct {
	ChannelTracking map[string][]string // targetID → allowed userIDs/roleIDs
	WebhookTracking map[string][]string // webhookName → allowed userIDs
	UserTracking    []string            // user/role IDs that can manage other users' tracking
}

// buildDelegatedAdmin converts TOML array-of-tables entries into the internal map format.
func buildDelegatedAdmin(entries []DelegatedAdminEntry) map[string][]string {
	if len(entries) == 0 {
		return nil
	}
	m := make(map[string][]string, len(entries))
	for _, e := range entries {
		if e.Target != "" {
			m[e.Target] = append(m[e.Target], e.Admins...)
		}
	}
	return m
}

// DiscordTokens returns the discord tokens as a string slice.
func (c DiscordConfig) DiscordTokens() []string {
	return tomlTokens(c.Token)
}

// RoleSubscriptionMap converts the [[discord.role_subscriptions]] TOML array
// into a guild-keyed map, matching the userRoleSubscription configuration format.
func (c DiscordConfig) RoleSubscriptionMap() map[string]RoleSubscriptionEntry {
	m := make(map[string]RoleSubscriptionEntry, len(c.RoleSubscriptions))
	for _, entry := range c.RoleSubscriptions {
		if entry.Guild != "" {
			m[entry.Guild] = entry
		}
	}
	return m
}

// TelegramConfig reads the [telegram] section for fields the processor needs.
type TelegramConfig struct {
	Enabled                 bool                      `toml:"enabled"` // false = disable bot even if token is set
	Token                   any                       `toml:"token"`   // string or []string
	Channels                []string                  `toml:"channels"` // registration channel/group IDs
	Admins                  []string                  `toml:"admins"`
	CheckRole               bool                      `toml:"check_role"`
	CheckRoleInterval       int                       `toml:"check_role_interval"` // hours between periodic reconciliation
	BotWelcomeText              string `toml:"bot_welcome_text"`              // DM sent on registration
	BotGoodbyeMessage          string `toml:"bot_goodbye_message"`           // DM sent when user loses access
	GroupWelcomeText           string `toml:"group_welcome_text"`            // sent to group on registration, {user} replaced
	UnregisteredUserMessage    string `toml:"unregistered_user_message"`     // custom reply, overrides i18n
	UnrecognisedCommandMessage string `toml:"unrecognised_command_message"`  // custom reply, overrides i18n
	RegisterOnStart            bool   `toml:"register_on_start"`            // auto-register users on /start
	DisableAutoGreetings       bool   `toml:"disable_auto_greetings"`
	// Delegated administration — TOML array-of-tables format
	DelegatedAdmins    []DelegatedAdminEntry `toml:"delegated_admins"`    // [[telegram.delegated_admins]]
	UserTrackingAdmins []string              `toml:"user_tracking_admins"`

	// Internal computed maps (populated from TOML entries after load)
	DelegatedAdministration TelegramDelegatedAdminConfig `toml:"-"`
}

// TelegramDelegatedAdminConfig is the internal representation used by permissions code.
type TelegramDelegatedAdminConfig struct {
	ChannelTracking map[string][]string // targetID → allowed userIDs
	UserTracking    []string            // user IDs that can manage other users' tracking
}

// TelegramTokens returns the telegram tokens as a string slice.
func (c TelegramConfig) TelegramTokens() []string {
	return tomlTokens(c.Token)
}

// tomlTokens normalizes a TOML token field (bare string or array) into a string slice.
func tomlTokens(v any) []string {
	switch t := v.(type) {
	case string:
		if t != "" {
			return []string{t}
		}
	case []any:
		var tokens []string
		for _, elem := range t {
			if s, ok := elem.(string); ok && s != "" {
				tokens = append(tokens, s)
			}
		}
		return tokens
	}
	return nil
}

// ListenAddr returns the host:port listen address.
func (p ProcessorConfig) ListenAddr() string {
	return fmt.Sprintf("%s:%d", p.Host, p.Port)
}

type DatabaseConfig struct {
	Host     string          `toml:"host"`
	Port     int             `toml:"port"`
	User     string          `toml:"user"`
	Password string          `toml:"password"`
	Database string          `toml:"database"`
	Scanner  ScannerDBConfig `toml:"scanner"`
}

// ScannerDBConfig holds configuration for the scanner database connection.
type ScannerDBConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Database string `toml:"database"`
	Type     string `toml:"type"` // "golbat" (default) or "rdm"
}

// DSN returns a MySQL DSN string for the scanner database.
func (s ScannerDBConfig) DSN() string {
	host := s.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.Port
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true", s.User, s.Password, host, port, s.Database)
}

// Configured returns true if the scanner database has been configured with at least a user and database.
func (s ScannerDBConfig) Configured() bool {
	return s.User != "" && s.Database != ""
}

// DSN returns a MySQL DSN string built from the individual fields.
func (d DatabaseConfig) DSN() string {
	host := d.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := d.Port
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true", d.User, d.Password, host, port, d.Database)
}

type GeofenceConfig struct {
	Paths       []string    `toml:"paths"`
	DefaultName string      `toml:"default_name"` // prefix for unnamed fences (e.g. "Fence" → Fence1, Fence2)
	Koji        KojiOptions `toml:"koji"`
}

type KojiOptions struct {
	BearerToken string `toml:"bearer_token"`
	CacheDir    string `toml:"cache_dir"`
}

type PVPConfig struct {
	PVPQueryMaxRank            int   `toml:"pvp_query_max_rank"`
	PVPFilterMaxRank           int   `toml:"filter_max_rank"`
	PVPEvolutionDirectTracking bool  `toml:"evolution_direct_tracking"`
	LevelCaps                  []int `toml:"level_caps"`
	PVPFilterGreatMinCP        int   `toml:"filter_great_min_cp"`
	PVPFilterUltraMinCP        int   `toml:"filter_ultra_min_cp"`
	PVPFilterLittleMinCP       int   `toml:"filter_little_min_cp"`
	IncludeMegaEvolution       bool  `toml:"include_mega_evolution"`
	DisplayMaxRank             int   `toml:"display_max_rank"`
	DisplayGreatMinCP          int   `toml:"display_great_min_cp"`
	DisplayUltraMinCP          int   `toml:"display_ultra_min_cp"`
	DisplayLittleMinCP         int   `toml:"display_little_min_cp"`
	FilterByTrack              bool   `toml:"filter_by_track"`
	ForceMinCP                 bool   `toml:"force_min_cp"`
	DataSource                 string `toml:"data_source"` // "webhook" (default) or "ohbem"
}

type WeatherConfig struct {
	EnableInference            bool   `toml:"enable_inference"`
	ChangeAlert                bool   `toml:"change_alert"`
	ShowAlteredPokemon              bool `toml:"show_altered_pokemon"`
	ShowAlteredPokemonMaxCount      int  `toml:"show_altered_pokemon_max_count"`
	ShowAlteredPokemonStaticMap     bool `toml:"show_altered_pokemon_static_map"`

	// AccuWeather forecast
	EnableForecast          bool     `toml:"enable_forecast"`
	AccuWeatherAPIKeys      []string `toml:"accuweather_api_keys"`
	AccuWeatherDayQuota     int      `toml:"accuweather_day_quota"`
	ForecastRefreshInterval int      `toml:"forecast_refresh_interval"` // hours between API calls
	LocalFirstFetchHOD      int      `toml:"local_first_fetch_hod"`    // first fetch hour of day
	SmartForecast           bool     `toml:"smart_forecast"`            // pull on demand if no data
}

type StatsConfig struct {
	MinSampleSize       int     `toml:"min_sample_size"`
	WindowHours         int     `toml:"window_hours"`
	RefreshIntervalMins int     `toml:"refresh_interval_mins"`
	Uncommon            float64 `toml:"rarity_group_2_uncommon"`
	Rare                float64 `toml:"rarity_group_3_rare"`
	VeryRare            float64 `toml:"rarity_group_4_very_rare"`
	UltraRare           float64 `toml:"rarity_group_5_ultra_rare"`
}

type TuningConfig struct {
	ReloadIntervalSecs         int `toml:"reload_interval_secs"`
	EncounterCacheTTL          int `toml:"encounter_cache_ttl"`
	WorkerPoolSize             int `toml:"worker_pool_size"`
	BatchSize                  int `toml:"batch_size"`
	FlushIntervalMillis        int `toml:"flush_interval_millis"`
	TileserverConcurrency      int `toml:"tileserver_concurrency"`       // tile worker goroutines (default 2)
	TileserverTimeout          int `toml:"tileserver_timeout"`           // HTTP POST timeout ms (default 10000)
	TileserverFailureThreshold int `toml:"tileserver_failure_threshold"` // circuit breaker threshold (default 5)
	TileserverCooldownMs       int `toml:"tileserver_cooldown_ms"`      // circuit breaker cooldown ms (default 30000)
	TileserverQueueSize        int `toml:"tileserver_queue_size"`       // async tile queue depth (default 100)
	TileserverDeadlineMs       int `toml:"tileserver_deadline"`         // max wait for tile before fallback ms (default 5000)
	TileserverPregenTTL        int `toml:"tileserver_pregen_ttl"`       // seconds for pregenerated tile TTL (0 = no TTL hint)
	GeocodingConcurrency       int `toml:"geocoding_concurrency"`
	GeocodingTimeout           int `toml:"geocoding_timeout"`            // ms
	GeocodingFailureThreshold  int `toml:"geocoding_failure_threshold"`
	GeocodingCooldownMs        int `toml:"geocoding_cooldown_ms"`
	RenderPoolSize             int `toml:"render_pool_size"`
	RenderQueueSize            int `toml:"render_queue_size"`

	// Delivery tuning
	ConcurrentDiscordDestinations  int `toml:"concurrent_discord_destinations"`
	ConcurrentTelegramDestinations int `toml:"concurrent_telegram_destinations"`
	ConcurrentDiscordWebhooks      int `toml:"concurrent_discord_webhooks"`
	DeliveryQueueSize              int `toml:"delivery_queue_size"`
}

type AreaConfig struct {
	Enabled         bool              `toml:"enabled"`
	StrictLocations bool              `toml:"strict_locations"`
	Communities     []CommunityConfig `toml:"communities"`
}

// CommunityConfig represents a community entry under [[area_security.communities]].
type CommunityConfig struct {
	Name          string   `toml:"name" json:"name"`
	AllowedAreas  []string `toml:"allowed_areas" json:"allowed_areas"`
	LocationFence []string `toml:"location_fence" json:"location_fence"`
	Discord       struct {
		Channels []string `toml:"channels" json:"channels"`
		UserRole []string `toml:"user_role" json:"user_role"`
	} `toml:"discord" json:"discord"`
	Telegram struct {
		Channels []string `toml:"channels" json:"channels"`
		Admins   []string `toml:"admins" json:"admins"`
	} `toml:"telegram" json:"telegram"`
}

type AlertLimitsConfig struct {
	TimingPeriod        int                  `toml:"timing_period"`
	DMLimit             int                  `toml:"dm_limit"`
	ChannelLimit        int                  `toml:"channel_limit"`
	MaxLimitsBeforeStop int                  `toml:"max_limits_before_stop"`
	DisableOnStop       bool                 `toml:"disable_on_stop"`
	ShameChannel        string               `toml:"shame_channel"`
	Overrides           []AlertLimitOverride `toml:"overrides"`
}

type AlertLimitOverride struct {
	Target string `toml:"target" json:"target"`
	Limit  int    `toml:"limit" json:"limit"`
}

type WebhookLoggingConfig struct {
	Enabled        bool   `toml:"enabled"`
	Filename       string `toml:"filename"`
	MaxSize        int    `toml:"max_size"`    // MB per file
	MaxAge         int    `toml:"max_age"`     // days to keep old files (0 = use max_backups only)
	MaxBackups     int    `toml:"max_backups"` // number of old files to keep
	Compress       bool   `toml:"compress"`
	RotateInterval int    `toml:"rotate_interval"` // minutes between forced rotations (0 = size only)
}

// GeocodingConfig holds settings from the [geocoding] section for static map generation
// and address geocoding.
type GeocodingConfig struct {
	// Address geocoding provider
	Provider     string   `toml:"provider"`      // "none", "nominatim", "google"
	ProviderURL  string   `toml:"provider_url"`  // nominatim URL
	GeocodingKey []string `toml:"geocoding_key"` // google API keys
	CacheDetail  int      `toml:"cache_detail"`  // decimal places for cache key rounding (default 3)
	ForwardOnly  bool     `toml:"forward_only"`  // if true, skip reverse geocoding

	// Static map tile provider
	StaticProvider    string                       `toml:"static_provider"`
	StaticProviderURL string                       `toml:"static_provider_url"`
	StaticKey         []string                     `toml:"static_key"`
	Width             int                          `toml:"width"`
	Height            int                          `toml:"height"`
	Zoom              int                          `toml:"zoom"`
	MapType           string                       `toml:"type"`
	DayStyle          string                       `toml:"day_style"`
	DawnStyle         string                       `toml:"dawn_style"`
	DuskStyle         string                       `toml:"dusk_style"`
	NightStyle        string                       `toml:"night_style"`
	TileserverSettings map[string]TileserverConfig `toml:"tileserver_settings"`
	StaticMapType     map[string]string            `toml:"static_map_type"`
}

// TileserverConfig holds per-tile-type settings under [geocoding.tileserver_settings.*].
// Booleans use *bool so empty TOML sections don't override defaults.
type TileserverConfig struct {
	Type         string `toml:"type"`
	IncludeStops *bool  `toml:"include_stops"`
	Width        int    `toml:"width"`
	Height       int    `toml:"height"`
	Zoom         int    `toml:"zoom"`
	Pregenerate  *bool  `toml:"pregenerate"`
	TTL          int    `toml:"ttl"` // seconds, 0 = use global tileserver_pregen_ttl
}

// FallbacksConfig holds fallback URLs from the [fallbacks] section.
type FallbacksConfig struct {
	StaticMap    string `toml:"static_map"`
	ImgURL       string `toml:"img_url"`        // fallback pokemon icon
	ImgURLWeather string `toml:"img_url_weather"` // fallback weather icon
	ImgURLEgg    string `toml:"img_url_egg"`     // fallback egg icon
	ImgURLGym    string `toml:"img_url_gym"`     // fallback gym icon
	ImgURLPokestop string `toml:"img_url_pokestop"` // fallback pokestop icon
	PokestopURL  string `toml:"pokestop_url"`    // fallback pokestop URL (for pokestop_url field)
}

// ResolvePath resolves a path relative to the config file's directory.
// Absolute paths are returned as-is.
func (c *Config) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.BaseDir, p)
}

func Load(baseDir string) (*Config, error) {
	absDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(absDir, "config", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	cfg := &Config{
		BaseDir: absDir,
		Processor: ProcessorConfig{
			Host: "0.0.0.0",
			Port: 3030,
		},
		Reconciliation: ReconciliationConfig{
			Discord: ReconciliationDiscordConfig{
				RemoveInvalidUsers: true,
				UpdateChannelNames: true,
			},
			Telegram: ReconciliationTelegramConfig{
				RemoveInvalidUsers: true,
			},
		},
		PVP: PVPConfig{
			PVPQueryMaxRank:      100,
			PVPFilterMaxRank:     100,
			PVPFilterGreatMinCP:  1400,
			PVPFilterUltraMinCP:  2350,
			PVPFilterLittleMinCP: 450,
			LevelCaps:            []int{50},
			DisplayMaxRank:       10,
			DisplayGreatMinCP:    1400,
			DisplayUltraMinCP:    2350,
			DisplayLittleMinCP:   450,
			DataSource:           "webhook",
		},
		Tuning: TuningConfig{
			ReloadIntervalSecs:             60,
			EncounterCacheTTL:              3600,
			WorkerPoolSize:                 4,
			BatchSize:                      50,
			FlushIntervalMillis:            100,
			RenderPoolSize:                 8,
			RenderQueueSize:                100,
			ConcurrentDiscordDestinations:  10,
			ConcurrentTelegramDestinations: 10,
			ConcurrentDiscordWebhooks:      10,
			DeliveryQueueSize:              200,
		},
		Stats: StatsConfig{
			MinSampleSize:       10000,
			WindowHours:         8,
			RefreshIntervalMins: 5,
			Uncommon:            1.0,
			Rare:                0.5,
			VeryRare:            0.03,
			UltraRare:           0.01,
		},
		Locale: LocaleConfig{
			TimeFormat:    "en-gb",
			Time:          "LTS",
			Date:          "L",
			Language:      "en",
			AddressFormat: "{{{streetName}}} {{streetNumber}}",
		},
		Weather: WeatherConfig{
			ShowAlteredPokemonMaxCount: 10,
			AccuWeatherDayQuota:        500,
			ForecastRefreshInterval:    8,
			LocalFirstFetchHOD:         3,
		},
		Logging: LoggingConfig{
			Filename:           "logs/processor.log",
			FileLoggingEnabled: true,
			MaxSize:            50,
			MaxAge:             7,
			MaxBackups:         5,
			Compress:           true,
		},
		WebhookLogging: WebhookLoggingConfig{
			Filename:       "logs/webhooks.log",
			MaxSize:        100,
			MaxAge:         1,
			MaxBackups:     12,
			Compress:       true,
			RotateInterval: 60, // hourly
		},
		Discord: DiscordConfig{
			Enabled:           true,
			Prefix:            "!",
			IvColors:          []string{"#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"},
			CheckRoleInterval: 6,
			Activity:          "PoracleNG",
		},
		AlertLimits: AlertLimitsConfig{
			TimingPeriod:        240,
			DMLimit:             20,
			ChannelLimit:        40,
			MaxLimitsBeforeStop: 10,
		},
		Database: DatabaseConfig{
			Scanner: ScannerDBConfig{
				Type: "golbat",
			},
		},
		Geocoding: GeocodingConfig{
			CacheDetail: 3,
			Width:       320,
			Height:      200,
			Zoom:        15,
			MapType:     "klokantech-basic",
		},
		General: GeneralConfig{
			ImgURL:              "https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS",
			StickerURL:          "https://raw.githubusercontent.com/bbdoc/tgUICONS/main/Shuffle",
			Locale:              "en",
			RoleCheckMode:       "ignore",
			DefaultTemplateName: "1",
			AlertMinimumTime:    120,
		},
		Telegram: TelegramConfig{
			Enabled:           true,
			CheckRoleInterval: 6,
			BotWelcomeText:    "You are now registered with Poracle",
			GroupWelcomeText:  "Welcome {user}, remember to click on me and 'start bot' to be able to receive messages",
		},
		Fallbacks: FallbacksConfig{
			StaticMap:      "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/staticMap.png",
			ImgURL:         "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/mon.png",
			ImgURLWeather:  "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/weather.png",
			ImgURLEgg:      "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/uni.png",
			ImgURLGym:      "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/gym.png",
			ImgURLPokestop: "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png",
			PokestopURL:    "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png",
		},
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Copy api_secret from [alerter] section for backward compatibility
	if cfg.Alerter.APISecret != "" && cfg.Processor.APISecret == "" {
		cfg.Processor.APISecret = cfg.Alerter.APISecret
	}

	// Convert delegated admin TOML array-of-tables to internal map format
	cfg.Discord.DelegatedAdministration = DelegatedAdminConfig{
		ChannelTracking: buildDelegatedAdmin(cfg.Discord.DelegatedAdmins),
		WebhookTracking: buildDelegatedAdmin(cfg.Discord.WebhookAdmins),
		UserTracking:    cfg.Discord.UserTrackingAdmins,
	}
	cfg.Telegram.DelegatedAdministration = TelegramDelegatedAdminConfig{
		ChannelTracking: buildDelegatedAdmin(cfg.Telegram.DelegatedAdmins),
		UserTracking:    cfg.Telegram.UserTrackingAdmins,
	}

	// Validate required fields
	if cfg.Database.User == "" || cfg.Database.Database == "" {
		return nil, fmt.Errorf("[database] user and database are required")
	}

	// Default geofence path if none specified
	if len(cfg.Geofence.Paths) == 0 {
		cfg.Geofence.Paths = []string{"geofences/geofence.json"}
	}

	// Resolve relative geofence paths and cache dir relative to config directory
	configDir := filepath.Join(cfg.BaseDir, "config")
	for i, p := range cfg.Geofence.Paths {
		if !filepath.IsAbs(p) && !isHTTP(p) {
			cfg.Geofence.Paths[i] = filepath.Join(configDir, p)
		}
	}
	if cfg.Geofence.Koji.CacheDir != "" && !filepath.IsAbs(cfg.Geofence.Koji.CacheDir) {
		cfg.Geofence.Koji.CacheDir = filepath.Join(configDir, cfg.Geofence.Koji.CacheDir)
	}

	// Apply JSON overrides (from config editor). LoadOverrides stays silent
	// because logging isn't set up yet — main.go logs the status afterwards.
	overrides, status, err := LoadOverrides(configDir)
	if err != nil {
		log.Warnf("config: %v", err)
	} else if overrides != nil {
		ApplyOverrides(cfg, overrides)
		cfg.OverrideStatus = status
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

	// Conditional defaults that depend on other config values
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = cfg.PVP.PVPFilterMaxRank
	}
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = 100
	}

	// Logging level fallback chain: level → log_level → console_log_level (migrated configs)
	if cfg.Logging.Level == "" {
		if cfg.Logging.LogLevel != "" {
			cfg.Logging.Level = cfg.Logging.LogLevel
		} else if cfg.Logging.ConsoleLogLevel != "" {
			cfg.Logging.Level = cfg.Logging.ConsoleLogLevel
		}
	}

	// Resolve log filenames relative to project root (BaseDir)
	if !filepath.IsAbs(cfg.Logging.Filename) {
		cfg.Logging.Filename = filepath.Join(cfg.BaseDir, cfg.Logging.Filename)
	}
	if cfg.WebhookLogging.Filename != "" && !filepath.IsAbs(cfg.WebhookLogging.Filename) {
		cfg.WebhookLogging.Filename = filepath.Join(cfg.BaseDir, cfg.WebhookLogging.Filename)
	}

	return cfg, nil
}

func isHTTP(s string) bool {
	return len(s) >= 7 && (s[:7] == "http://" || (len(s) >= 8 && s[:8] == "https://"))
}
