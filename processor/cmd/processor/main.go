package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/debug"
	"path/filepath"
	_ "time/tzdata" // embed IANA timezone database as fallback
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
	"github.com/pokemon/poracleng/processor/internal/telegrambot"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/logging"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/resources"
	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func main() {
	baseDir := flag.String("basedir", "..", "path to project root directory")
	flag.Parse()

	// Register build info from Go's embedded VCS metadata
	buildVersion, buildCommit, buildDate := readBuildInfo()
	metrics.BuildInfo.WithLabelValues(buildVersion, buildCommit, buildDate).Set(1)

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

	log.Infof("Poracle processor %s (commit %s, built %s)", buildVersion, buildCommit, buildDate)

	// Download game resources (monsters, moves, locales, etc.)
	if err := resources.Download(cfg.BaseDir); err != nil {
		log.Warnf("Resource download had errors: %s", err)
	}

	database, err := db.OpenDB(cfg.Database.DSN())
	if err != nil {
		log.Fatalf("Failed to open database: %s", err)
	}
	defer database.Close()

	humanStore := store.NewSQLHumanStore(database)
	trackingStores := store.NewTrackingStores(database)

	// Database migrations: adopt existing Knex DB if needed, drop FK constraints, run pending
	if err := db.AdoptExistingDatabase(database.DB); err != nil {
		log.Fatalf("Failed to adopt database: %s", err)
	}
	db.DropForeignKeys(database.DB)
	if err := db.RunMigrations(database.DB); err != nil {
		log.Fatalf("Failed to run migrations: %s", err)
	}

	stateMgr := state.NewManager()

	// Initial load (includes geofences)
	if err := state.LoadWithGeofences(stateMgr, database, cfg.Geofence); err != nil {
		log.Fatalf("Failed to load initial state: %s", err)
	}

	// Create processor
	metrics.WorkerPoolCapacity.Set(float64(cfg.Tuning.WorkerPoolSize))
	proc := NewProcessorService(cfg, stateMgr, database)

	// Restore gym state cache from previous run
	if err := proc.gymState.Load(); err != nil {
		log.Warnf("Failed to load gym state cache: %v", err)
	}

	// Start render pool workers
	poolSize := cfg.Tuning.RenderPoolSize
	if poolSize < 1 {
		poolSize = 8
	}
	for i := 0; i < poolSize; i++ {
		proc.renderWg.Add(1)
		go proc.renderWorker()
	}
	log.Infof("Render pool started: %d workers, queue size %d", poolSize, cfg.Tuning.RenderQueueSize)

	// Initialize delivery dispatcher
	discordToken := ""
	if tokens := cfg.Discord.DiscordTokens(); len(tokens) > 0 {
		discordToken = tokens[0]
	}
	telegramToken := ""
	if tokens := cfg.Telegram.TelegramTokens(); len(tokens) > 0 {
		telegramToken = tokens[0]
	}

	if discordToken != "" || telegramToken != "" {
		var err error
		proc.dispatcher, err = delivery.NewDispatcher(delivery.DispatcherConfig{
			DiscordToken:  discordToken,
			TelegramToken: telegramToken,
			UploadImages:  cfg.Discord.UploadEmbedImages,
			DeleteDelayMs: cfg.Discord.MessageDeleteDelay,
			QueueSize:     cfg.Tuning.DeliveryQueueSize,
			CacheDir:      filepath.Join(cfg.BaseDir, "config", ".cache"),
			Queue: delivery.QueueConfig{
				ConcurrentDiscord:  cfg.Tuning.ConcurrentDiscordDestinations,
				ConcurrentWebhook:  cfg.Tuning.ConcurrentDiscordWebhooks,
				ConcurrentTelegram: cfg.Tuning.ConcurrentTelegramDestinations,
			},
		})
		if err != nil {
			log.Warnf("Delivery dispatcher init failed: %s", err)
		} else {
			proc.dispatcher.Start()
			log.Infof("Delivery dispatcher started: discord=%d webhook=%d telegram=%d queue=%d",
				cfg.Tuning.ConcurrentDiscordDestinations,
				cfg.Tuning.ConcurrentDiscordWebhooks,
				cfg.Tuning.ConcurrentTelegramDestinations,
				cfg.Tuning.DeliveryQueueSize)
		}
	}

	// Weather change consumer
	if cfg.Weather.ChangeAlert {
		go proc.consumeWeatherChanges()
		log.Infof("Weather change alerts enabled")
	}

	// Profile auto-switch scheduler
	go proc.runProfileScheduler()
	log.Infof("Profile scheduler enabled (10-minute interval)")

	// HTTP server — Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(api.RequestLogger())

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

	// Public endpoints (no auth)
	r.POST("/", webhookHandler)
	r.GET("/health", api.HandleHealth())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// pprof endpoints
	pprofGroup := r.Group("/debug/pprof")
	{
		pprofGroup.GET("/", gin.WrapF(http.DefaultServeMux.ServeHTTP))
		pprofGroup.GET("/:name", gin.WrapF(http.DefaultServeMux.ServeHTTP))
	}

	// Authenticated API group
	apiGroup := r.Group("/api")
	apiGroup.Use(api.RequireSecretGin(cfg.Processor.APISecret))

	// Reload
	apiGroup.POST("/reload", api.HandleReload(func() error {
		return state.Load(stateMgr, database)
	}))
	apiGroup.GET("/reload", api.HandleReload(func() error {
		return state.Load(stateMgr, database)
	}))
	apiGroup.POST("/geofence/reload", api.HandleReload(func() error {
		return state.LoadWithGeofences(stateMgr, database, cfg.Geofence)
	}))
	apiGroup.GET("/geofence/reload", api.HandleReload(func() error {
		return state.LoadWithGeofences(stateMgr, database, cfg.Geofence)
	}))

	// Weather, stats, geocode, test
	apiGroup.GET("/weather", api.HandleWeather(proc.weather))
	apiGroup.GET("/stats/rarity", api.HandleStats(func() any { return proc.stats.ExportGroups() }))
	apiGroup.GET("/stats/shiny", api.HandleStats(func() any { return proc.stats.ExportShinyStats() }))
	apiGroup.GET("/stats/shiny-possible", api.HandleStats(func() any { return proc.stats.ExportShinyPossible() }))
	apiGroup.POST("/test", api.HandleTest(proc))
	apiGroup.GET("/geocode/forward", api.HandleGeocode(proc.enricher.Geocoder))

	// Geofence data and tile generation endpoints
	tileDeps := api.TileDeps{
		StaticMap: proc.enricher.StaticMap,
		StateMgr:  stateMgr,
		ImgUicons: proc.enricher.ImgUicons,
		Weather:   proc.weather,
	}
	geofence := apiGroup.Group("/geofence")
	geofence.GET("/all/hash", api.HandleGeofenceHash(stateMgr))
	geofence.GET("/all/geojson", api.HandleGeofenceGeoJSON(stateMgr))
	geofence.GET("/all", api.HandleGeofenceAll(stateMgr))
	geofence.GET("/weatherMap/:lat/:lon", api.HandleWeatherMap(tileDeps))
	geofence.GET("/locationMap/:lat/:lon", api.HandleLocationMap(tileDeps))
	geofence.GET("/distanceMap/:lat/:lon/:distance", api.HandleDistanceMap(tileDeps))
	geofence.POST("/overviewMap", api.HandleOverviewMap(tileDeps))
	geofence.GET("/:area/map", api.HandleGeofenceAreaMap(tileDeps))

	// Tracking CRUD endpoints (registered after proc is created so enricher/scanner are available)
	defaultTemplate := "1"
	if cfg.General.DefaultTemplateName != nil {
		defaultTemplate = fmt.Sprintf("%v", cfg.General.DefaultTemplateName)
	}
	trackingDeps := &api.TrackingDeps{
		DB:           database,
		Tracking:     trackingStores,
		StateMgr:     stateMgr,
		RowText: &rowtext.Generator{
			GD:                  proc.enricher.GameData,
			Translations:        proc.enricher.Translations,
			DefaultTemplateName: defaultTemplate,
			Scanner:             proc.scanner,
		},
		Config:       cfg,
		Translations: proc.enricher.Translations,
		Dispatcher:   proc.dispatcher,
		ReloadFunc:   proc.triggerReload,
	}
	tracking := apiGroup.Group("/tracking")
	// Pokemon (monster) tracking
	tracking.GET("/pokemon/:id", api.HandleGetMonster(trackingDeps))
	tracking.POST("/pokemon/:id", api.HandleCreateMonster(trackingDeps))
	tracking.DELETE("/pokemon/:id/byUid/:uid", api.HandleDeleteMonster(trackingDeps))
	tracking.POST("/pokemon/:id/delete", api.HandleBulkDeleteMonster(trackingDeps))
	tracking.GET("/pokemon/refresh", api.HandleReload(func() error {
		return state.Load(stateMgr, database)
	}))
	// Raid tracking
	tracking.GET("/raid/:id", api.HandleGetRaid(trackingDeps))
	tracking.POST("/raid/:id", api.HandleCreateRaid(trackingDeps))
	tracking.DELETE("/raid/:id/byUid/:uid", api.HandleDeleteRaid(trackingDeps))
	tracking.POST("/raid/:id/delete", api.HandleBulkDeleteRaid(trackingDeps))
	// Egg tracking
	tracking.GET("/egg/:id", api.HandleGetEgg(trackingDeps))
	tracking.POST("/egg/:id", api.HandleCreateEgg(trackingDeps))
	tracking.DELETE("/egg/:id/byUid/:uid", api.HandleDeleteEgg(trackingDeps))
	tracking.POST("/egg/:id/delete", api.HandleBulkDeleteEgg(trackingDeps))
	// Quest tracking
	tracking.GET("/quest/:id", api.HandleGetQuest(trackingDeps))
	tracking.POST("/quest/:id", api.HandleCreateQuest(trackingDeps))
	tracking.DELETE("/quest/:id/byUid/:uid", api.HandleDeleteQuest(trackingDeps))
	tracking.POST("/quest/:id/delete", api.HandleBulkDeleteQuest(trackingDeps))
	// Invasion tracking
	tracking.GET("/invasion/:id", api.HandleGetInvasion(trackingDeps))
	tracking.POST("/invasion/:id", api.HandleCreateInvasion(trackingDeps))
	tracking.DELETE("/invasion/:id/byUid/:uid", api.HandleDeleteInvasion(trackingDeps))
	tracking.POST("/invasion/:id/delete", api.HandleBulkDeleteInvasion(trackingDeps))
	// Lure tracking
	tracking.GET("/lure/:id", api.HandleGetLure(trackingDeps))
	tracking.POST("/lure/:id", api.HandleCreateLure(trackingDeps))
	tracking.DELETE("/lure/:id/byUid/:uid", api.HandleDeleteLure(trackingDeps))
	tracking.POST("/lure/:id/delete", api.HandleBulkDeleteLure(trackingDeps))
	// Nest tracking
	tracking.GET("/nest/:id", api.HandleGetNest(trackingDeps))
	tracking.POST("/nest/:id", api.HandleCreateNest(trackingDeps))
	tracking.DELETE("/nest/:id/byUid/:uid", api.HandleDeleteNest(trackingDeps))
	tracking.POST("/nest/:id/delete", api.HandleBulkDeleteNest(trackingDeps))
	// Gym tracking
	tracking.GET("/gym/:id", api.HandleGetGym(trackingDeps))
	tracking.POST("/gym/:id", api.HandleCreateGym(trackingDeps))
	tracking.DELETE("/gym/:id/byUid/:uid", api.HandleDeleteGym(trackingDeps))
	tracking.POST("/gym/:id/delete", api.HandleBulkDeleteGym(trackingDeps))
	// Fort tracking
	tracking.GET("/fort/:id", api.HandleGetFort(trackingDeps))
	tracking.POST("/fort/:id", api.HandleCreateFort(trackingDeps))
	tracking.DELETE("/fort/:id/byUid/:uid", api.HandleDeleteFort(trackingDeps))
	tracking.POST("/fort/:id/delete", api.HandleBulkDeleteFort(trackingDeps))
	// Maxbattle tracking
	tracking.GET("/maxbattle/:id", api.HandleGetMaxbattle(trackingDeps))
	tracking.POST("/maxbattle/:id", api.HandleCreateMaxbattle(trackingDeps))
	tracking.DELETE("/maxbattle/:id/byUid/:uid", api.HandleDeleteMaxbattle(trackingDeps))
	tracking.POST("/maxbattle/:id/delete", api.HandleBulkDeleteMaxbattle(trackingDeps))
	// Aggregate tracking endpoints
	tracking.GET("/all/:id", api.HandleGetAllTracking(trackingDeps))
	tracking.GET("/allProfiles/:id", api.HandleGetAllProfilesTracking(trackingDeps))

	// Human endpoints — Gin handles path parameter conflicts natively
	var discordBotRef *discordbot.Bot
	roleDeps := &api.RoleDeps{
		SessionFunc: func() *discordgo.Session {
			if discordBotRef != nil {
				return discordBotRef.Session()
			}
			return nil
		},
		Config: cfg,
		DB:     database,
	}
	humans := apiGroup.Group("/humans")
	humans.GET("/one/:id", api.HandleGetOneHuman(trackingDeps))
	humans.GET("/:id", api.HandleGetHumanAreas(trackingDeps))
	humans.GET("/:id/roles", api.HandleGetRoles(roleDeps))
	humans.GET("/:id/getAdministrationRoles", api.HandleGetAdministrationRoles(roleDeps))
	humans.GET("/:id/checkLocation/:lat/:lon", api.HandleCheckLocation(trackingDeps))
	humans.POST("", api.HandleCreateHuman(trackingDeps))
	humans.POST("/:id/start", api.HandleStartHuman(trackingDeps))
	humans.POST("/:id/stop", api.HandleStopHuman(trackingDeps))
	humans.POST("/:id/adminDisabled", api.HandleAdminDisabled(trackingDeps))
	humans.POST("/:id/switchProfile/:profile", api.HandleSwitchProfile(trackingDeps))
	humans.POST("/:id/setLocation/:lat/:lon", api.HandleSetLocation(trackingDeps))
	humans.POST("/:id/setAreas", api.HandleSetAreas(trackingDeps))
	humans.POST("/:id/roles/add/:roleId", api.HandleAddRole(roleDeps))
	humans.POST("/:id/roles/remove/:roleId", api.HandleRemoveRole(roleDeps))

	// Profile endpoints
	profiles := apiGroup.Group("/profiles")
	profiles.GET("/:id", api.HandleGetProfiles(trackingDeps))
	profiles.DELETE("/:id/byProfileNo/:profile_no", api.HandleDeleteProfile(trackingDeps))
	profiles.POST("/:id/add", api.HandleAddProfile(trackingDeps))
	profiles.POST("/:id/update", api.HandleUpdateProfile(trackingDeps))
	profiles.POST("/:id/copy/:from/:to", api.HandleCopyProfile(trackingDeps))

	// DTS template endpoints
	if proc.dtsRenderer != nil {
		apiGroup.GET("/config/templates", api.HandleTemplateConfig(proc.dtsRenderer.Templates()))
		apiGroup.POST("/dts/render", api.HandleDTSRender(proc.dtsRenderer.Templates()))
	}

	// Config and master data endpoints
	apiGroup.GET("/config/poracleWeb", api.HandleConfigPoracleWeb(cfg))
	apiGroup.GET("/masterdata/monsters", api.HandleMasterdataMonsters(proc.enricher.GameData, proc.enricher.Translations))
	apiGroup.GET("/masterdata/grunts", api.HandleMasterdataGrunts(proc.enricher.GameData))

	// Delivery endpoint — accepts pre-rendered jobs
	apiGroup.POST("/deliverMessages", api.HandleDeliverMessages(proc.dispatcher))
	apiGroup.POST("/postMessage", api.HandleDeliverMessages(proc.dispatcher)) // legacy alias

	// Command framework — shared by API endpoint and Discord/Telegram bots
	cmdLanguages := cfg.General.AvailableLanguages
	if len(cmdLanguages) == 0 {
		cmdLanguages = []string{"en"}
	}
	cmdPrefix := cfg.Discord.Prefix
	if cmdPrefix == "" {
		cmdPrefix = "!"
	}
	cmdParser := bot.NewParser(cmdPrefix, proc.enricher.Translations, cmdLanguages)
	tgParser := bot.NewParser("/", proc.enricher.Translations, cmdLanguages)
	cmdResolver := bot.NewPokemonResolver(proc.enricher.GameData, proc.enricher.Translations, cmdLanguages, nil)
	cmdArgMatcher := bot.NewArgMatcher(proc.enricher.Translations, proc.enricher.GameData, cmdResolver, cmdLanguages)
	cmdRegistry := bot.NewRegistry()
	cmdRegistry.Register(&commands.StartCommand{})
	cmdRegistry.Register(&commands.StopCommand{})
	cmdRegistry.Register(&commands.EggCommand{})
	cmdRegistry.Register(&commands.RaidCommand{})
	cmdRegistry.Register(&commands.TrackCommand{})
	cmdRegistry.Register(&commands.TrackedCommand{})
	cmdRegistry.Register(&commands.UntrackCommand{})
	cmdRegistry.Register(&commands.GymCommand{})
	cmdRegistry.Register(&commands.InvasionCommand{})
	cmdRegistry.Register(&commands.NestCommand{})
	cmdRegistry.Register(&commands.FortCommand{})
	cmdRegistry.Register(&commands.MaxbattleCommand{})
	cmdRegistry.Register(&commands.QuestCommand{})
	cmdRegistry.Register(&commands.LureCommand{})
	cmdRegistry.Register(&commands.WeatherCommand{})
	cmdRegistry.Register(&commands.LanguageCommand{})
	cmdRegistry.Register(&commands.ProfileCommand{})
	cmdRegistry.Register(&commands.LocationCommand{})
	cmdRegistry.Register(&commands.AreaCommand{})
	cmdRegistry.Register(&commands.ScriptCommand{})
	cmdRegistry.Register(&commands.VersionCommand{})
	cmdRegistry.Register(&commands.EnableCommand{})
	cmdRegistry.Register(&commands.DisableCommand{})
	cmdRegistry.Register(&commands.HelpCommand{})
	cmdRegistry.Register(&commands.InfoCommand{})
	cmdRegistry.Register(&commands.PoracleTestCommand{})
	cmdRegistry.Register(&commands.UserlistCommand{})
	cmdRegistry.Register(&commands.PoracleCommand{})
	cmdRegistry.Register(&commands.UnregisterCommand{})
	cmdRegistry.Register(&commands.CommunityCommand{})
	cmdRegistry.Register(&commands.AskCommand{})
	cmdRegistry.Register(&commands.BackupCommand{})
	cmdRegistry.Register(&commands.RestoreCommand{})
	cmdRegistry.Register(&commands.BroadcastCommand{})
	cmdRegistry.Register(&commands.ApplyCommand{})

	// NLP parser for !ask command and suggest_on_dm
	var nlpParser *nlp.Parser
	if cfg.AI.Enabled {
		enTr := proc.enricher.Translations.For("en")
		invasionEvents := make(map[string]bool)
		if proc.enricher.GameData != nil && proc.enricher.GameData.Util != nil {
			for _, event := range proc.enricher.GameData.Util.PokestopEvent {
				invasionEvents[strings.ToLower(event.Name)] = true
			}
		}
		nlpParser = nlp.NewParser(enTr, cfg.BaseDir, invasionEvents)
		log.Info("NLP command parser initialized")
	}

	// Command API endpoint (for testing commands without bots)
	var cmdDTS *dts.TemplateStore
	var cmdEmoji *dts.EmojiLookup
	if proc.dtsRenderer != nil {
		cmdDTS = proc.dtsRenderer.Templates()
		cmdEmoji = proc.dtsRenderer.Emoji()
	}
	cmdDeps := &api.CommandDeps{
		DB:           database,
		Humans:       humanStore,
		Tracking:     trackingStores,
		Config:       cfg,
		StateMgr:     stateMgr,
		GameData:     proc.enricher.GameData,
		Translations: proc.enricher.Translations,
		Dispatcher:   proc.dispatcher,
		RowText:      trackingDeps.RowText,
		Resolver:     cmdResolver,
		ArgMatcher:   cmdArgMatcher,
		Parser:       cmdParser,
		Registry:     cmdRegistry,
		Weather:      proc.weather,
		Stats:        proc.stats,
		DTS:          cmdDTS,
		Emoji:        cmdEmoji,
		NLPParser:    nlpParser,
		ReloadFunc:   proc.triggerReload,
	}
	apiGroup.POST("/command", api.HandleCommand(cmdDeps))

	server := &http.Server{
		Addr:    cfg.Processor.ListenAddr(),
		Handler: r,
	}

	// Periodic reload
	periodicDone := make(chan struct{})
	go func() {
		interval := time.Duration(cfg.Tuning.ReloadIntervalSecs) * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-periodicDone:
				return
			case <-ticker.C:
			}
			log.Debugf("Periodic reload triggered")
			start := time.Now()
			if err := state.Load(stateMgr, database); err != nil {
				log.Errorf("Periodic reload failed: %s", err)
				metrics.StateReloads.WithLabelValues("error").Inc()
			} else {
				metrics.StateReloads.WithLabelValues("success").Inc()
			}
			metrics.StateReloadDuration.Observe(time.Since(start).Seconds())

			// Periodic status summary
			webhooks := metrics.IntervalWebhooks.Swap(0)
			matched := metrics.IntervalMatched.Swap(0)
			intervalSecs := float64(cfg.Tuning.ReloadIntervalSecs)
			webhooksPerMin := float64(webhooks) / intervalSecs * 60
			matchedPerMin := float64(matched) / intervalSecs * 60

			statusParts := []string{
				fmt.Sprintf("Webhooks: %.0f/min", webhooksPerMin),
				fmt.Sprintf("Matched: %.0f/min", matchedPerMin),
			}
			if proc.enricher.StaticMap != nil {
				ts := proc.enricher.StaticMap.GetStats()
				statusParts = append(statusParts, fmt.Sprintf("Tiles: %d calls avg:%dms err:%d", ts.Calls, ts.AvgMs(), ts.Errors))
				proc.enricher.StaticMap.ResetStats()
			}
			if proc.enricher.Geocoder != nil {
				gs := proc.enricher.Geocoder.GetStats()
				statusParts = append(statusParts, fmt.Sprintf("Geo: %d calls avg:%dms hits:%d err:%d", gs.Calls, gs.AvgMs(), gs.Hits, gs.Errors))
				proc.enricher.Geocoder.ResetStats()
			}
			if proc.renderCh != nil {
				depth := len(proc.renderCh)
				statusParts = append(statusParts, fmt.Sprintf("RenderQ: %d/%d", depth, cap(proc.renderCh)))
				metrics.RenderQueueDepth.Set(float64(depth))
			}
			if proc.dispatcher != nil {
				statusParts = append(statusParts, fmt.Sprintf("Delivery: Discord:%d+%d Telegram:%d Tracked:%d RateLimited:%d",
					proc.dispatcher.DiscordDepth(),
					proc.dispatcher.WebhookDepth(),
					proc.dispatcher.TelegramDepth(),
					proc.dispatcher.TrackerSize(),
					proc.dispatcher.RateLimitWaiting()))
				metrics.DeliveryQueueDepth.Set(float64(proc.dispatcher.QueueDepth()))
				metrics.DeliveryDiscordQueueDepth.Set(float64(proc.dispatcher.DiscordDepth()))
				metrics.DeliveryWebhookQueueDepth.Set(float64(proc.dispatcher.WebhookDepth()))
				metrics.DeliveryTelegramQueueDepth.Set(float64(proc.dispatcher.TelegramDepth()))
				metrics.DeliveryTrackerSize.Set(float64(proc.dispatcher.TrackerSize()))
			}
			log.Infof("[Status] %s", strings.Join(statusParts, " | "))
		}
	}()

	// Start Discord bot (if token configured)
	var discordBot *discordbot.Bot
	// Shared bot dependencies — constructed once, passed to both Discord and Telegram bots.
	sharedBotDeps := bot.BotDeps{
		DB:           database,
		Humans:       humanStore,
		Tracking:     trackingStores,
		Cfg:          cfg,
		StateMgr:     stateMgr,
		GameData:     proc.enricher.GameData,
		Translations: proc.enricher.Translations,
		Dispatcher:   proc.dispatcher,
		RowText:      trackingDeps.RowText,
		Registry:     cmdRegistry,
		ArgMatcher:   cmdArgMatcher,
		Resolver:     cmdResolver,
		Geocoder:     proc.enricher.Geocoder,
		StaticMap:    proc.enricher.StaticMap,
		Weather:      proc.weather,
		Stats:        proc.stats,
		DTS:          cmdDTS,
		Emoji:        cmdEmoji,
		NLPParser:    nlpParser,
		ReloadFunc:   proc.triggerReload,
	}

	discordTokens := cfg.Discord.DiscordTokens()
	if len(discordTokens) > 0 && discordTokens[0] != "" {
		deps := sharedBotDeps
		deps.Parser = cmdParser
		dbot, err := discordbot.New(discordbot.Config{
			Token:   discordTokens[0],
			BotDeps: deps,
		})
		if err != nil {
			log.Warnf("Discord bot failed to start: %v", err)
		} else {
			discordBot = dbot
			discordBotRef = dbot
		}
	}

	// Start Telegram bot (if token configured)
	var telegramBot *telegrambot.Bot
	telegramTokens := cfg.Telegram.TelegramTokens()
	if len(telegramTokens) > 0 && telegramTokens[0] != "" {
		deps := sharedBotDeps
		deps.Parser = tgParser
		tbot, err := telegrambot.New(telegrambot.Config{
			Token:   telegramTokens[0],
			BotDeps: deps,
		})
		if err != nil {
			log.Warnf("Telegram bot failed to start: %v", err)
		} else {
			telegramBot = tbot
		}
	}

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

	// 1. Stop accepting new requests — no more webhooks enter the pipeline
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Shutdown(ctx)
	log.Infof("HTTP server stopped")

	// 2. Stop bots (no more command processing)
	close(periodicDone)
	if discordBot != nil {
		discordBot.Close()
		log.Infof("Discord bot disconnected")
	}
	if telegramBot != nil {
		telegramBot.Close()
		log.Infof("Telegram bot disconnected")
	}

	// 3. Drain webhook workers → render queue → delivery queue
	proc.Close()
	log.Infof("Shutdown complete")
}

func readBuildInfo() (version, commit, date string) {
	version, commit, date = "dev", "unknown", "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 8 {
				commit = s.Value[:8]
			} else {
				commit = s.Value
			}
		case "vcs.time":
			date = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				commit += "-dirty"
			}
		}
	}
	return
}

// ProcessorService ties together all matching/tracking components.
type ProcessorService struct {
	cfg             *config.Config
	stateMgr        *state.Manager
	database        *sqlx.DB
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
	dtsRenderer     *dts.Renderer
	dispatcher      *delivery.Dispatcher
	scanner         scanner.Scanner
	rateLimiter     *ratelimit.Limiter
	translations    *i18n.Bundle
	renderCh        chan RenderJob
	renderWg        sync.WaitGroup
	reloadMu        sync.Mutex
	reloadTimer     *time.Timer
	workerPool      chan struct{}
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
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

	// Load full game data from raw masterfile + util.json
	gd, err := gamedata.Load(cfg.BaseDir)
	if err != nil {
		log.Fatalf("Failed to load game data: %s — ensure resources are downloaded (check network on first run)", err)
	}
	log.Infof("Game data loaded: %d monsters, %d moves, %d types", len(gd.Monsters), len(gd.Moves), len(gd.Types))

	// Initialize weather type boost from util.json (replaces hardcoded fallback)
	if gd != nil && gd.Util != nil {
		gamedata.InitWeatherTypeBoost(gd.Util)
	}

	var activePokemon *tracker.ActivePokemonTracker
	var pokemonTypes *gamedata.PokemonTypes
	if cfg.Weather.ShowAlteredPokemon && gd != nil {
		pokemonTypes = gamedata.PokemonTypesFromGameData(gd.Monsters)
		activePokemon = tracker.NewActivePokemonTracker(50)
		log.Info("Active pokemon tracking enabled (from game data)")
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

	// Wire game data and translations into enricher
	enricher.GameData = gd
	enricher.Translations = i18n.Load(cfg.BaseDir)
	enricher.MapConfig = &enrichment.MapConfig{
		RdmURL:       cfg.General.RdmURL,
		ReactMapURL:  cfg.General.ReactMapURL,
		RocketMadURL: cfg.General.RocketMadURL,
	}
	enricher.IvColors = cfg.Discord.IvColors
	enricher.PVPDisplay = &enrichment.PVPDisplayConfig{
		MaxRank:       cfg.PVP.DisplayMaxRank,
		GreatMinCP:    cfg.PVP.DisplayGreatMinCP,
		UltraMinCP:    cfg.PVP.DisplayUltraMinCP,
		LittleMinCP:   cfg.PVP.DisplayLittleMinCP,
		FilterByTrack: cfg.PVP.FilterByTrack,
	}

	// Icon resolvers
	if cfg.General.ImgURL != "" {
		enricher.ImgUicons = uicons.New(cfg.General.ImgURL, "png")
		log.Infof("Uicons enabled: %s", cfg.General.ImgURL)
	} else {
		log.Warn("No img_url configured in [general] — icon URLs will not be resolved")
	}
	if cfg.General.ImgURLAlt != "" {
		enricher.ImgUiconsAlt = uicons.New(cfg.General.ImgURLAlt, "png")
	}
	if cfg.General.StickerURL != "" {
		enricher.StickerUicons = uicons.New(cfg.General.StickerURL, "webp")
	}
	enricher.DefaultLocale = cfg.General.Locale
	enricher.RequestShinyImages = cfg.General.RequestShinyImages

	// Scanner DB and static map tile resolver
	var scannerInstance scanner.Scanner
	if cfg.Database.Scanner.Configured() {
		var err error
		scannerDSN := cfg.Database.Scanner.DSN()
		switch cfg.Database.Scanner.Type {
		case "rdm":
			scannerInstance, err = scanner.NewRDM(scannerDSN)
		default: // "golbat" or empty
			scannerInstance, err = scanner.NewGolbat(scannerDSN)
		}
		if err != nil {
			log.Warnf("Failed to connect to scanner DB: %s (static maps with stops disabled)", err)
		} else {
			log.Infof("Scanner DB connected (%s)", cfg.Database.Scanner.Type)
		}
	}

	if cfg.Geocoding.StaticProvider != "" && cfg.Geocoding.StaticProvider != "none" {
		smCfg := staticmap.Config{
			Provider:                   cfg.Geocoding.StaticProvider,
			ProviderURL:                cfg.Geocoding.StaticProviderURL,
			StaticKeys:                 cfg.Geocoding.StaticKey,
			Width:                      cfg.Geocoding.Width,
			Height:                     cfg.Geocoding.Height,
			Zoom:                       cfg.Geocoding.Zoom,
			MapType:                    cfg.Geocoding.MapType,
			DayStyle:                   cfg.Geocoding.DayStyle,
			DawnStyle:                  cfg.Geocoding.DawnStyle,
			DuskStyle:                  cfg.Geocoding.DuskStyle,
			NightStyle:                 cfg.Geocoding.NightStyle,
			Scanner:                    scannerInstance,
			ImgUicons:                  enricher.ImgUicons,
			FallbackURL:                cfg.Fallbacks.StaticMap,
			StaticMapType:              cfg.Geocoding.StaticMapType,
			TileserverConcurrency:      cfg.Tuning.TileserverConcurrency,
			TileserverTimeout:          cfg.Tuning.TileserverTimeout,
			TileserverFailureThreshold: cfg.Tuning.TileserverFailureThreshold,
			TileserverCooldownMs:       cfg.Tuning.TileserverCooldownMs,
			TileQueueSize:              cfg.Tuning.TileserverQueueSize,
			TileDeadlineMs:             cfg.Tuning.TileserverDeadlineMs,
		}

		// Convert tileserver settings
		if len(cfg.Geocoding.TileserverSettings) > 0 {
			smCfg.TileserverSettings = make(map[string]staticmap.TileTypeConfig, len(cfg.Geocoding.TileserverSettings))
			for k, v := range cfg.Geocoding.TileserverSettings {
				tc := staticmap.TileTypeConfig{
					Type:   v.Type,
					Width:  v.Width,
					Height: v.Height,
					Zoom:   v.Zoom,
				}
				if v.IncludeStops != nil {
					tc.IncludeStops = v.IncludeStops
				}
				if v.Pregenerate != nil {
					tc.Pregenerate = v.Pregenerate
				}
				smCfg.TileserverSettings[k] = tc
			}
		}

		enricher.StaticMap = staticmap.New(smCfg)
		log.Infof("Static map provider: %s", cfg.Geocoding.StaticProvider)
	}

	// Geocoder (reverse address lookups)
	var geocoder *geocoding.Geocoder
	if cfg.Geocoding.Provider != "" && cfg.Geocoding.Provider != "none" {
		var err error
		geocoder, err = geocoding.New(geocoding.Config{
			Provider:         cfg.Geocoding.Provider,
			ProviderURL:      cfg.Geocoding.ProviderURL,
			GeocodingKeys:    cfg.Geocoding.GeocodingKey,
			CacheDetail:      cfg.Geocoding.CacheDetail,
			CachePath:        filepath.Join(cfg.BaseDir, "config", ".cache", "geocache"),
			ForwardOnly:      cfg.Geocoding.ForwardOnly,
			AddressFormat:    cfg.Locale.AddressFormat,
			Timeout:          cfg.Tuning.GeocodingTimeout,
			FailureThreshold: cfg.Tuning.GeocodingFailureThreshold,
			CooldownMs:       cfg.Tuning.GeocodingCooldownMs,
			Concurrency:      cfg.Tuning.GeocodingConcurrency,
		})
		if err != nil {
			log.Warnf("Geocoder init failed: %s", err)
		} else if geocoder != nil {
			enricher.Geocoder = geocoder
			log.Infof("Geocoder enabled: %s", cfg.Geocoding.Provider)
		}
	}

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
		Overrides:           overrides,
	})

	// DTS renderer — renders templates in Go and delivers via /api/deliverMessages
	var dtsRenderer *dts.Renderer
	var utilEmojis map[string]string
	if gd != nil {
		utilEmojis = gd.Util.Emojis
	}
	// Shortlink URL shortener (for <S< ... >S> markers in DTS templates)
	var shlinkURL, shlinkKey, shlinkDomain string
	if cfg.General.ShortlinkProvider == "shlink" && cfg.General.ShortlinkProviderURL != "" {
		shlinkURL = cfg.General.ShortlinkProviderURL
		shlinkKey = cfg.General.ShortlinkProviderKey
		shlinkDomain = cfg.General.ShortlinkDomain
	}

	dtsDefaultTemplate := "1"
	if cfg.General.DefaultTemplateName != nil {
		dtsDefaultTemplate = fmt.Sprintf("%v", cfg.General.DefaultTemplateName)
	}
	dtsRenderer, err = dts.NewRenderer(dts.RendererConfig{
		ConfigDir:           filepath.Join(cfg.BaseDir, "config"),
		FallbackDir:         filepath.Join(cfg.BaseDir, "fallbacks"),
		GameData:            gd,
		Translations:        enricher.Translations,
		UtilEmojis:          utilEmojis,
		DefaultLocale:       cfg.General.Locale,
		DefaultTemplateName: dtsDefaultTemplate,
		MinAlertTime:        cfg.General.AlertMinimumTime,
		ShlinkURL:           shlinkURL,
		ShlinkKey:           shlinkKey,
		ShlinkDomain:        shlinkDomain,
		DTSDictionary:       cfg.General.DTSDictionary,
	})
	if err != nil {
		log.Warnf("DTS renderer initialization failed: %s", err)
		dtsRenderer = nil
	} else {
		dtsRenderer.Templates().LogSummary()
	}

	// Start render pool for async tile resolution + DTS rendering + delivery
	renderQueueSize := cfg.Tuning.RenderQueueSize
	if renderQueueSize < 1 {
		renderQueueSize = 100
	}
	renderCh := make(chan RenderJob, renderQueueSize)
	metrics.RenderQueueCapacity.Set(float64(renderQueueSize))

	ctx, cancel := context.WithCancel(context.Background())

	return &ProcessorService{
		cfg:      cfg,
		stateMgr: stateMgr,
		database: database,
		ctx:      ctx,
		cancel:   cancel,
		renderCh: renderCh,
		enricher:      enricher,
		dtsRenderer:   dtsRenderer,
		scanner:       scannerInstance,
		weather:      weatherTracker,
		weatherCares: tracker.NewWeatherCareTracker(),
		encounters:   tracker.NewEncounterTracker(),
		duplicates:   tracker.NewDuplicateCache(),
		stats:        statsTracker,
		gymState:     tracker.NewGymStateTracker(filepath.Join(cfg.BaseDir, "config", ".cache")),
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
		translations:    enricher.Translations,
		workerPool:      make(chan struct{}, cfg.Tuning.WorkerPoolSize),
	}
}

func (ps *ProcessorService) Close() {
	ps.cancel()
	ps.wg.Wait()
	log.Info("Webhook workers stopped")

	// Close render channel BEFORE dispatcher — render workers feed into dispatcher.
	// Order: webhook workers → render channel → render workers → dispatcher → delivery
	if ps.renderCh != nil {
		close(ps.renderCh)
		ps.renderWg.Wait()
		log.Info("Render pool stopped")
	}
	if ps.dispatcher != nil {
		log.Info("Stopping delivery dispatcher...")
		ps.dispatcher.Stop()
		log.Info("Delivery dispatcher stopped")
	}
	if ps.enricher.StaticMap != nil {
		ps.enricher.StaticMap.Close()
	}
	ps.duplicates.Close()
	ps.rateLimiter.Close()
	// Persist gym state cache for restart
	if err := ps.gymState.Save(); err != nil {
		log.Warnf("Failed to save gym state cache: %v", err)
	} else {
		log.Info("Gym state cache saved")
	}
	if ps.enricher.Geocoder != nil {
		ps.enricher.Geocoder.Close()
	}
}

// Ensure ProcessorService implements webhook.Processor
var _ webhook.Processor = (*ProcessorService)(nil)
