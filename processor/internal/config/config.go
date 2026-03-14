package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server         ServerConfig         `toml:"server"`
	Database       DatabaseConfig       `toml:"database"`
	Alerter        AlerterConfig        `toml:"alerter"`
	Geofence       GeofenceConfig       `toml:"geofence"`
	PVP            PVPConfig            `toml:"pvp"`
	Weather        WeatherConfig        `toml:"weather"`
	Tuning         TuningConfig         `toml:"tuning"`
	Area           AreaConfig           `toml:"areaSecurity"`
	Locale         LocaleConfig         `toml:"locale"`
	Logging        LoggingConfig        `toml:"logging"`
	WebhookLogging WebhookLoggingConfig `toml:"webhookLogging"`
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

type ServerConfig struct {
	ListenAddr  string   `toml:"listen_addr"`
	IPWhitelist []string `toml:"ip_whitelist"`
}

type DatabaseConfig struct {
	DSN string `toml:"dsn"`
}

type AlerterConfig struct {
	URL string `toml:"url"`
}

type GeofenceConfig struct {
	Paths []string `toml:"paths"`
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
	EnableChangeAlert          bool   `toml:"enable_change_alert"`
	ShowAlteredPokemon         bool   `toml:"show_altered_pokemon"`
	ShowAlteredPokemonMaxCount int    `toml:"show_altered_pokemon_max_count"`
	MonstersJSONPath           string `toml:"monsters_json_path"`
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

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Server: ServerConfig{
			ListenAddr: ":4200",
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
	if cfg.Locale.Time == "" {
		cfg.Locale.Time = "HH:mm:ss"
	}
	if cfg.Locale.Date == "" {
		cfg.Locale.Date = "DD/MM/YYYY"
	}
	if cfg.Weather.ShowAlteredPokemonMaxCount == 0 {
		cfg.Weather.ShowAlteredPokemonMaxCount = 10
	}
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = cfg.PVP.PVPFilterMaxRank
	}
	if cfg.PVP.PVPQueryMaxRank == 0 {
		cfg.PVP.PVPQueryMaxRank = 100
	}
	return cfg, nil
}
