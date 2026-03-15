package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Processor      ProcessorConfig      `toml:"processor"`
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

	// BaseDir is the directory containing the config file, used to resolve relative paths.
	BaseDir string `toml:"-"`
}

type LocaleConfig struct {
	Time string `toml:"time"`
	Date string `toml:"date"`
}

type LoggingConfig struct {
	Level              string `toml:"level"`
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
}

// ListenAddr returns the host:port listen address.
func (p ProcessorConfig) ListenAddr() string {
	return fmt.Sprintf("%s:%d", p.Host, p.Port)
}

type DatabaseConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Database string `toml:"database"`
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
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", d.User, d.Password, host, port, d.Database)
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
}

type WeatherConfig struct {
	EnableInference            bool   `toml:"enable_inference"`
	ChangeAlert                bool   `toml:"change_alert"`
	ShowAlteredPokemon         bool   `toml:"show_altered_pokemon"`
	ShowAlteredPokemonMaxCount int `toml:"show_altered_pokemon_max_count"`

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
	ReloadIntervalSecs  int `toml:"reload_interval_secs"`
	EncounterCacheTTL   int `toml:"encounter_cache_ttl"`
	WorkerPoolSize      int `toml:"worker_pool_size"`
	BatchSize           int `toml:"batch_size"`
	FlushIntervalMillis int `toml:"flush_interval_millis"`
}

type AreaConfig struct {
	Enabled         bool `toml:"enabled"`
	StrictLocations bool `toml:"strict_locations"`
}

type WebhookLoggingConfig struct {
	Enabled    bool   `toml:"enabled"`
	Filename   string `toml:"filename"`
	MaxSize    int    `toml:"max_size"`
	MaxAge     int    `toml:"max_age"`
	MaxBackups int    `toml:"max_backups"`
	Compress   bool   `toml:"compress"`
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
			PVPQueryMaxRank:  100,
			PVPFilterMaxRank: 100,
			LevelCaps:        []int{50},
		},
		Tuning: TuningConfig{
			ReloadIntervalSecs:  60,
			EncounterCacheTTL:   3600,
			WorkerPoolSize:      4,
			BatchSize:           50,
			FlushIntervalMillis: 100,
		},
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
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

	if cfg.Stats.MinSampleSize == 0 {
		cfg.Stats.MinSampleSize = 10000
	}
	if cfg.Stats.WindowHours == 0 {
		cfg.Stats.WindowHours = 8
	}
	if cfg.Stats.RefreshIntervalMins == 0 {
		cfg.Stats.RefreshIntervalMins = 5
	}
	if cfg.Stats.Uncommon == 0 {
		cfg.Stats.Uncommon = 1.0
	}
	if cfg.Stats.Rare == 0 {
		cfg.Stats.Rare = 0.5
	}
	if cfg.Stats.VeryRare == 0 {
		cfg.Stats.VeryRare = 0.03
	}
	if cfg.Stats.UltraRare == 0 {
		cfg.Stats.UltraRare = 0.01
	}
	if cfg.Locale.Time == "" {
		cfg.Locale.Time = "HH:mm:ss"
	}
	if cfg.Locale.Date == "" {
		cfg.Locale.Date = "DD/MM/YYYY"
	}
	if cfg.Weather.ShowAlteredPokemonMaxCount == 0 {
		cfg.Weather.ShowAlteredPokemonMaxCount = 10
	}
	if cfg.Weather.AccuWeatherDayQuota == 0 {
		cfg.Weather.AccuWeatherDayQuota = 50
	}
	if cfg.Weather.ForecastRefreshInterval == 0 {
		cfg.Weather.ForecastRefreshInterval = 8
	}
	if cfg.Weather.LocalFirstFetchHOD == 0 {
		cfg.Weather.LocalFirstFetchHOD = 3
	}
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = cfg.PVP.PVPFilterMaxRank
	}
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = 100
	}

	// Logging defaults — file logging is on by default for the processor
	if cfg.Logging.Filename == "" {
		cfg.Logging.Filename = "logs/processor.log"
		cfg.Logging.FileLoggingEnabled = true
	}
	if cfg.Logging.MaxSize == 0 {
		cfg.Logging.MaxSize = 50
	}
	if cfg.Logging.MaxAge == 0 {
		cfg.Logging.MaxAge = 30
	}
	if cfg.Logging.MaxBackups == 0 {
		cfg.Logging.MaxBackups = 5
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
