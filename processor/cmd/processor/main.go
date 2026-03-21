package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	_ "time/tzdata" // embed IANA timezone database as fallback
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/logging"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	"github.com/pokemon/poracleng/processor/internal/resources"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func main() {
	baseDir := flag.String("basedir", "..", "path to project root directory")
	flag.Parse()

	cfg, err := config.Load(*baseDir)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	// Setup logging (must be after config load)
	logging.Setup(logging.Config{
		Level:              cfg.Logging.Level,
		FileLoggingEnabled: cfg.Logging.FileLoggingEnabled,
		Filename:           cfg.Logging.Filename,
		MaxSize:            cfg.Logging.MaxSize,
		MaxAge:             cfg.Logging.MaxAge,
		MaxBackups:         cfg.Logging.MaxBackups,
		Compress:           cfg.Logging.Compress,
	})

	// Download game resources (monsters, moves, locales, etc.)
	if err := resources.Download(cfg.BaseDir); err != nil {
		log.Warnf("Resource download had errors: %s", err)
	}

	database, err := db.OpenDB(cfg.Database.DSN())
	if err != nil {
		log.Fatalf("Failed to open database: %s", err)
	}
	defer database.Close()

	// Database migrations: adopt existing Knex DB if needed, then run pending migrations
	if err := db.AdoptExistingDatabase(database.DB); err != nil {
		log.Fatalf("Failed to adopt database: %s", err)
	}
	if err := db.RunMigrations(database.DB); err != nil {
		log.Fatalf("Failed to run migrations: %s", err)
	}

	stateMgr := state.NewManager()

	// Initial load
	if err := state.Load(stateMgr, database, cfg.Geofence); err != nil {
		log.Fatalf("Failed to load initial state: %s", err)
	}

	// Create processor
	metrics.WorkerPoolCapacity.Set(float64(cfg.Tuning.WorkerPoolSize))
	proc := NewProcessorService(cfg, stateMgr, database)

	// Weather change consumer
	if cfg.Weather.ChangeAlert {
		go proc.consumeWeatherChanges()
		log.Infof("Weather change alerts enabled")
	}

	// Profile auto-switch scheduler
	go proc.runProfileScheduler()
	log.Infof("Profile scheduler enabled (10-minute interval)")

	// HTTP server
	mux := http.NewServeMux()

	// Webhook receiver
	var webhookLogger io.Writer
	log.Infof("Webhook logging config: enabled=%v filename=%q", cfg.WebhookLogging.Enabled, cfg.WebhookLogging.Filename)
	if cfg.WebhookLogging.Enabled && cfg.WebhookLogging.Filename != "" {
		maxSize := cfg.WebhookLogging.MaxSize
		if maxSize == 0 {
			maxSize = 100
		}
		webhookLogger = &lumberjack.Logger{
			Filename:   cfg.WebhookLogging.Filename,
			MaxSize:    maxSize,
			MaxAge:     cfg.WebhookLogging.MaxAge,
			MaxBackups: cfg.WebhookLogging.MaxBackups,
			Compress:   cfg.WebhookLogging.Compress,
		}
		log.Infof("Webhook logging enabled: %s", cfg.WebhookLogging.Filename)
	}
	webhookHandler := webhook.NewHandler(proc, webhookLogger)
	mux.Handle("/", webhookHandler)

	// API endpoints
	mux.HandleFunc("/api/reload", api.HandleReload(func() error {
		return state.Load(stateMgr, database, cfg.Geofence)
	}))
	mux.HandleFunc("/api/weather", api.HandleWeather(proc.weather))
	mux.HandleFunc("/api/stats/rarity", api.HandleStats(func() any { return proc.stats.ExportGroups() }))
	mux.HandleFunc("/api/stats/shiny", api.HandleStats(func() any { return proc.stats.ExportShinyStats() }))
	mux.HandleFunc("/api/stats/shiny-possible", api.HandleStats(func() any { return proc.stats.ExportShinyPossible() }))
	mux.HandleFunc("/health", api.HandleHealth())

	// Proxy unhandled /api/ requests to the alerter (tracking, config, humans, etc.)
	mux.Handle("/api/", api.NewAlerterProxy(cfg.Processor.AlerterURL))

	// Prometheus metrics
	mux.Handle("/metrics", promhttp.Handler())

	// pprof endpoints (cpu, heap, goroutine, etc.)
	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)

	server := &http.Server{
		Addr:    cfg.Processor.ListenAddr(),
		Handler: mux,
	}

	// Periodic reload
	go func() {
		interval := time.Duration(cfg.Tuning.ReloadIntervalSecs) * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			log.Debugf("Periodic reload triggered")
			start := time.Now()
			if err := state.Load(stateMgr, database, cfg.Geofence); err != nil {
				log.Errorf("Periodic reload failed: %s", err)
				metrics.StateReloads.WithLabelValues("error").Inc()
			} else {
				metrics.StateReloads.WithLabelValues("success").Inc()
			}
			metrics.StateReloadDuration.Observe(time.Since(start).Seconds())
		}
	}()

	// Start server
	go func() {
		log.Infof("Processor starting on %s", cfg.Processor.ListenAddr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %s", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Infof("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Shutdown(ctx)
	proc.Close()
	log.Infof("Shutdown complete")
}

// ProcessorService ties together all matching/tracking components.
type ProcessorService struct {
	cfg             *config.Config
	stateMgr        *state.Manager
	database        *sqlx.DB
	sender          *webhook.Sender
	weather         *tracker.WeatherTracker
	weatherCares    *tracker.WeatherCareTracker
	encounters      *tracker.EncounterTracker
	duplicates      *tracker.DuplicateCache
	stats           *tracker.StatsTracker
	gymState        *tracker.GymStateTracker
	pokemonMatcher  *matching.PokemonMatcher
	raidMatcher     *matching.RaidMatcher
	invasionMatcher *matching.InvasionMatcher
	questMatcher    *matching.QuestMatcher
	lureMatcher     *matching.LureMatcher
	gymMatcher      *matching.GymMatcher
	nestMatcher     *matching.NestMatcher
	fortMatcher       *matching.FortMatcher
	maxbattleMatcher  *matching.MaxbattleMatcher
	pvpCfg          *pvp.Config
	activePokemon   *tracker.ActivePokemonTracker
	pokemonTypes    *gamedata.PokemonTypes
	enricher        *enrichment.Enricher
	rateLimiter     *ratelimit.Limiter
	translations    *i18n.Bundle
	workerPool      chan struct{}
	wg              sync.WaitGroup
}

func NewProcessorService(cfg *config.Config, stateMgr *state.Manager, database *sqlx.DB) *ProcessorService {
	pvpCfg := &pvp.Config{
		LevelCaps:                  cfg.PVP.LevelCaps,
		PVPFilterMaxRank:           cfg.PVP.PVPFilterMaxRank,
		PVPEvolutionDirectTracking: cfg.PVP.PVPEvolutionDirectTracking,
		IncludeMegaEvolution:       cfg.PVP.IncludeMegaEvolution,
		PVPFilterGreatMinCP:        cfg.PVP.PVPFilterGreatMinCP,
		PVPFilterUltraMinCP:        cfg.PVP.PVPFilterUltraMinCP,
		PVPFilterLittleMinCP:       cfg.PVP.PVPFilterLittleMinCP,
	}

	strictAreas := cfg.Area.Enabled && cfg.Area.StrictLocations

	var activePokemon *tracker.ActivePokemonTracker
	var pokemonTypes *gamedata.PokemonTypes
	if cfg.Weather.ShowAlteredPokemon {
		monstersPath := cfg.ResolvePath("resources/data/monsters.json")
		pt, err := gamedata.LoadPokemonTypes(monstersPath)
		if err != nil {
			log.Errorf("Failed to load pokemon types from %s: %s (active pokemon tracking disabled)", monstersPath, err)
		} else {
			pokemonTypes = pt
			activePokemon = tracker.NewActivePokemonTracker(50)
			log.Infof("Active pokemon tracking enabled with %s", monstersPath)
		}
	}

	if !geo.IsLocaleSupported(cfg.Locale.TimeFormat) {
		log.Warnf("Unsupported locale.timeformat %q — Moment.js shortcuts (LTS, L, etc.) will fall back to en-gb. Supported locales: %v",
			cfg.Locale.TimeFormat, geo.SupportedLocales())
	}

	weatherTracker := tracker.NewWeatherTracker()
	timeLayout := geo.ConvertTimeFormat(cfg.Locale.Time, cfg.Locale.TimeFormat)
	eventChecker := enrichment.NewPogoEventChecker(timeLayout)

	enricher := enrichment.New(
		timeLayout,
		geo.ConvertTimeFormat(cfg.Locale.Date, cfg.Locale.TimeFormat),
		weatherTracker,
		eventChecker,
	)

	// Stats tracker (rarity + shiny, shared rolling window)
	statsTracker := tracker.NewStatsTracker(tracker.StatsConfig{
		MinSampleSize:       cfg.Stats.MinSampleSize,
		WindowHours:         cfg.Stats.WindowHours,
		RefreshIntervalMins: cfg.Stats.RefreshIntervalMins,
		Uncommon:            cfg.Stats.Uncommon,
		Rare:                cfg.Stats.Rare,
		VeryRare:            cfg.Stats.VeryRare,
		UltraRare:           cfg.Stats.UltraRare,
	})
	enricher.ShinyProvider = statsTracker

	// AccuWeather forecast integration
	if cfg.Weather.EnableForecast && len(cfg.Weather.AccuWeatherAPIKeys) > 0 {
		awClient := tracker.NewAccuWeatherClient(tracker.AccuWeatherConfig{
			APIKeys:                 cfg.Weather.AccuWeatherAPIKeys,
			DayQuota:                cfg.Weather.AccuWeatherDayQuota,
			ForecastRefreshInterval: cfg.Weather.ForecastRefreshInterval,
			LocalFirstFetchHOD:      cfg.Weather.LocalFirstFetchHOD,
			SmartForecast:           cfg.Weather.SmartForecast,
		}, weatherTracker)
		enricher.ForecastProvider = awClient
		log.Infof("AccuWeather forecast enabled with %d API keys", len(cfg.Weather.AccuWeatherAPIKeys))
	}

	// Load translations: embedded defaults → resources/locale → alerter/locale → config/custom.*.json
	translations := i18n.Load(cfg.BaseDir)

	// Build rate limiter overrides map from config array
	overrides := make(map[string]int, len(cfg.AlertLimits.Overrides))
	for _, o := range cfg.AlertLimits.Overrides {
		overrides[o.Target] = o.Limit
	}
	rateLimiter := ratelimit.New(ratelimit.Config{
		TimingPeriod:        cfg.AlertLimits.TimingPeriod,
		DMLimit:             cfg.AlertLimits.DMLimit,
		ChannelLimit:        cfg.AlertLimits.ChannelLimit,
		MaxLimitsBeforeStop: cfg.AlertLimits.MaxLimitsBeforeStop,
		DisableOnStop:       cfg.AlertLimits.DisableOnStop,
		Overrides:           overrides,
		ShameChannel:        cfg.AlertLimits.ShameChannel,
	})

	return &ProcessorService{
		cfg:      cfg,
		stateMgr: stateMgr,
		database: database,
		enricher: enricher,
		sender:       webhook.NewSender(cfg.Processor.AlerterURL, cfg.Tuning.BatchSize, cfg.Tuning.FlushIntervalMillis),
		weather:      weatherTracker,
		weatherCares: tracker.NewWeatherCareTracker(),
		encounters:   tracker.NewEncounterTracker(),
		duplicates:   tracker.NewDuplicateCache(),
		stats:        statsTracker,
		gymState:     tracker.NewGymStateTracker(),
		pokemonMatcher: &matching.PokemonMatcher{
			PVPQueryMaxRank:            cfg.PVP.PVPQueryMaxRank,
			PVPEvolutionDirectTracking: cfg.PVP.PVPEvolutionDirectTracking,
			StrictLocations:            cfg.Area.StrictLocations,
			AreaSecurityEnabled:        cfg.Area.Enabled,
		},
		raidMatcher:     &matching.RaidMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: cfg.Area.Enabled},
		invasionMatcher: &matching.InvasionMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		questMatcher:    &matching.QuestMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		lureMatcher:     &matching.LureMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		gymMatcher:      &matching.GymMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		nestMatcher:     &matching.NestMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		fortMatcher:       &matching.FortMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		maxbattleMatcher:  &matching.MaxbattleMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		pvpCfg:          pvpCfg,
		activePokemon:   activePokemon,
		pokemonTypes:    pokemonTypes,
		rateLimiter:     rateLimiter,
		translations:    translations,
		workerPool:      make(chan struct{}, cfg.Tuning.WorkerPoolSize),
	}
}

func (ps *ProcessorService) Close() {
	ps.wg.Wait()
	ps.sender.Close()
	ps.duplicates.Close()
	ps.rateLimiter.Close()
}

// Ensure ProcessorService implements webhook.Processor
var _ webhook.Processor = (*ProcessorService)(nil)
