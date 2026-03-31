package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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
	Area           AreaConfig           `toml:"areaSecurity"`
	Locale         LocaleConfig         `toml:"locale"`
	Logging        LoggingConfig        `toml:"logging"`
	WebhookLogging WebhookLoggingConfig `toml:"webhookLogging"`
	AlertLimits    AlertLimitsConfig    `toml:"alert_limits"`
	Alerter        AlerterConfig        `toml:"alerter"`
	Discord        DiscordConfig        `toml:"discord"`
	Telegram       TelegramConfig       `toml:"telegram"`
	Geocoding      GeocodingConfig      `toml:"geocoding"`
	Fallbacks      FallbacksConfig      `toml:"fallbacks"`

	// BaseDir is the directory containing the config file, used to resolve relative paths.
	BaseDir string `toml:"-"`
}

// GeneralConfig holds settings from the [general] section used by the processor
// for map URL generation and other enrichment features.
type GeneralConfig struct {
	Locale               string `toml:"locale"`                // default language code (e.g. "en", "pl")
	DefaultTemplateName  any    `toml:"default_template_name"` // default DTS template (typically 1 or "1")
	RdmURL               string `toml:"rdm_url"`
	ReactMapURL          string `toml:"react_map_url"`
	RocketMadURL         string `toml:"rocket_mad_url"`
	ImgURL               string `toml:"img_url"`
	ImgURLAlt            string `toml:"img_url_alt"`
	StickerURL           string `toml:"sticker_url"`
	RequestShinyImages   bool   `toml:"request_shiny_images"`
	PopulatePokestopName bool   `toml:"populate_pokestop_name"`
}

type LocaleConfig struct {
	TimeFormat    string `toml:"timeformat"`
	Time          string `toml:"time"`
	Date          string `toml:"date"`
	AddressFormat string `toml:"address_format"`
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
	AlerterURL  string   `toml:"alerter_url"`
	IPWhitelist []string `toml:"ip_whitelist"`
	APISecret   string   `toml:"api_secret"` // Alerter API secret (read from [alerter] section)
}

// AlerterConfig reads the [alerter] section so the processor can authenticate to alerter APIs.
type AlerterConfig struct {
	APISecret string `toml:"api_secret"`
}

// DiscordConfig reads the [discord] section for fields the processor needs.
type DiscordConfig struct {
	Token                   any                  `toml:"token"` // string or []string
	Prefix                  string               `toml:"prefix"`
	IvColors                []string             `toml:"iv_colors"`
	Admins                  []string             `toml:"admins"`
	UploadEmbedImages       bool                 `toml:"upload_embed_images"`
	MessageDeleteDelay      int                  `toml:"message_delete_delay"` // extra ms for clean TTH on channels
	CommandSecurity         map[string][]string  `toml:"command_security"`
	DelegatedAdministration DelegatedAdminConfig `toml:"delegated_administration"`
}

// DelegatedAdminConfig controls who can manage tracking for channels and webhooks.
type DelegatedAdminConfig struct {
	ChannelTracking map[string][]string `toml:"channel_tracking"` // channelID/guildID/categoryID → allowed userIDs/roleIDs
	WebhookTracking map[string][]string `toml:"webhook_tracking"` // webhookName → allowed userIDs
}

// DiscordTokens returns the discord tokens as a string slice.
func (c DiscordConfig) DiscordTokens() []string {
	return tomlTokens(c.Token)
}

// TelegramConfig reads the [telegram] section for fields the processor needs.
type TelegramConfig struct {
	Token  any      `toml:"token"` // string or []string
	Admins []string `toml:"admins"`
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
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", s.User, s.Password, host, port, s.Database)
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
	Paths []string    `toml:"paths"`
	Koji  KojiOptions `toml:"koji"`
}

type KojiOptions struct {
	BearerToken string `toml:"bearer_token"`
	CacheDir    string `toml:"cache_dir"`
}

type PVPConfig struct {
	PVPQueryMaxRank            int   `toml:"pvp_query_max_rank"`
	PVPFilterMaxRank           int   `toml:"pvp_filter_max_rank"`
	PVPEvolutionDirectTracking bool  `toml:"pvp_evolution_direct_tracking"`
	LevelCaps                  []int `toml:"level_caps"`
	PVPFilterGreatMinCP        int   `toml:"pvp_filter_great_min_cp"`
	PVPFilterUltraMinCP        int   `toml:"pvp_filter_ultra_min_cp"`
	PVPFilterLittleMinCP       int   `toml:"pvp_filter_little_min_cp"`
	IncludeMegaEvolution       bool  `toml:"include_mega_evolution"`
	DisplayMaxRank             int   `toml:"display_max_rank"`
	DisplayGreatMinCP          int   `toml:"display_great_min_cp"`
	DisplayUltraMinCP          int   `toml:"display_ultra_min_cp"`
	DisplayLittleMinCP         int   `toml:"display_little_min_cp"`
	FilterByTrack              bool  `toml:"filter_by_track"`
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
	Name          string   `toml:"name"`
	AllowedAreas  []string `toml:"allowed_areas"`
	LocationFence []string `toml:"location_fence"`
	Discord       struct {
		Channels []string `toml:"channels"`
		UserRole []string `toml:"user_role"`
	} `toml:"discord"`
	Telegram struct {
		Channels []string `toml:"channels"`
		Admins   []string `toml:"admins"`
	} `toml:"telegram"`
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
	Target string `toml:"target"`
	Limit  int    `toml:"limit"`
}

type WebhookLoggingConfig struct {
	Enabled    bool   `toml:"enabled"`
	Filename   string `toml:"filename"`
	MaxSize    int    `toml:"max_size"`
	MaxAge     int    `toml:"max_age"`
	MaxBackups int    `toml:"max_backups"`
	Compress   bool   `toml:"compress"`
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
}

// FallbacksConfig holds fallback URLs from the [fallbacks] section.
type FallbacksConfig struct {
	StaticMap string `toml:"static_map"`
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
			Host:       "0.0.0.0",
			Port:       3030,
			AlerterURL: "http://localhost:3031",
		},
		PVP: PVPConfig{
			PVPQueryMaxRank:    100,
			PVPFilterMaxRank:   100,
			LevelCaps:          []int{50},
			DisplayMaxRank:     10,
			DisplayGreatMinCP:  1400,
			DisplayUltraMinCP:  2350,
			DisplayLittleMinCP: 450,
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
			AddressFormat: "{{{streetName}}} {{streetNumber}}",
		},
		Weather: WeatherConfig{
			ShowAlteredPokemonMaxCount: 10,
			AccuWeatherDayQuota:        50,
			ForecastRefreshInterval:    8,
			LocalFirstFetchHOD:         3,
		},
		Logging: LoggingConfig{
			Filename:           "logs/processor.log",
			FileLoggingEnabled: true,
			MaxSize:            50,
			MaxAge:             30,
			MaxBackups:         5,
		},
		Discord: DiscordConfig{
			Prefix:   "!",
			IvColors: []string{"#9D9D9D", "#FFFFFF", "#1EFF00", "#0070DD", "#A335EE", "#FF8000"},
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
		},
		General: GeneralConfig{
			ImgURL:     "https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS",
			StickerURL: "https://raw.githubusercontent.com/bbdoc/tgUICONS/main/Shuffle",
		},
		Fallbacks: FallbacksConfig{
			StaticMap: "https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/staticMap.png",
		},
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Copy alerter api_secret to processor config for API authentication
	if cfg.Alerter.APISecret != "" && cfg.Processor.APISecret == "" {
		cfg.Processor.APISecret = cfg.Alerter.APISecret
	}

	// Validate required fields
	if cfg.Processor.AlerterURL == "" {
		return nil, fmt.Errorf("[processor] alerter_url is required")
	}
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
