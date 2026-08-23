package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embed IANA timezone database as fallback

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/pokemon/poracleng/processor"
	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/backup"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/bot/commands"
	"github.com/pokemon/poracleng/processor/internal/buttonactions"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/logbuffer"
	"github.com/pokemon/poracleng/processor/internal/logging"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	"github.com/pokemon/poracleng/processor/internal/resources"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/telegrambot"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/validation"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func main() {
	baseDir := flag.String("basedir", "..", "path to project root directory")
	forceSyncSlash := flag.Bool("sync-slash-commands", false, "Force slash command sync regardless of cache")
	clearGlobalSlash := flag.Bool("clear-global-slash-commands", false, "Clear all globally-registered slash commands at startup (use when switching from register_globally=true to false to remove the global duplicates)")
	clearGuildSlash := flag.Bool("clear-guild-slash-commands", false, "Clear all guild-scoped slash commands at startup for the guilds listed in [discord.slash_commands] (use when switching from register_globally=false to true)")
	flag.Parse()

	// Register build info from version.go + ldflag-injected / VCS metadata
	buildVersion, buildCommit, buildBranch, buildDate := readBuildInfo()
	metrics.BuildInfo.WithLabelValues(buildVersion, buildCommit, buildDate).Set(1)
	api.Version = buildVersion
	displayVersion := buildVersion
	if buildBranch != "" {
		displayVersion += "-" + buildBranch
	}

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

	// Construct the in-memory log buffer and register it as a logrus hook
	// immediately after the logger is configured so we capture as much of
	// startup as possible (resource downloads, DB open, migrations, …).
	// startupCap=200 bounds the buffer against a misconfigured deployment
	// that might otherwise flood memory; rollingCap=50 keeps the last 50
	// WARN/ERROR events post-startup for !poracle-admin warnings.
	logBuf := logbuffer.New(200, 50)
	log.AddHook(logbuffer.NewHook(logBuf))

	log.Infof("Poracle processor %s (commit %s, built %s)", displayVersion, buildCommit, buildDate)

	// Now that logging is set up, report any override layering. The status
	// was computed during config.Load() but couldn't be logged before
	// logging.Setup ran.
	config.LogOverrideStatus(cfg.OverrideStatus)

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
	summaryScheduleStore := store.NewSQLSummaryScheduleStore(database)

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
	if err := state.LoadWithGeofences(stateMgr, database, summaryScheduleStore, cfg.Geofence); err != nil {
		log.Fatalf("Failed to load initial state: %s", err)
	}

	// Validate community area names against the loaded fence set so a typo in
	// allowed_areas / location_fence surfaces in the startup banner rather
	// than silently contributing nothing at match time.
	if cfg.Area.Enabled {
		validateCommunityAreas(stateMgr.Get().Fences, cfg.Area.Communities)
	}

	// Create processor
	metrics.WorkerPoolCapacity.Set(float64(cfg.Tuning.WorkerPoolSize))
	proc := NewProcessorService(cfg, stateMgr, database)
	proc.humans = humanStore
	proc.summarySchedules = summaryScheduleStore

	// Restore gym state cache from previous run
	if err := proc.gymState.Load(); err != nil {
		log.Warnf("Failed to load gym state cache: %v", err)
	}

	// Restore summary buffer from previous run. A missing or malformed
	// snapshot is silent inside Load — startup must never block on it.
	if err := proc.summaryBuffer.Load(); err != nil {
		log.Warnf("Failed to load summary buffer: %v", err)
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
			DiscordToken:    discordToken,
			TelegramToken:   telegramToken,
			UploadImages:    cfg.Discord.UploadEmbedImages,
			DeleteDelayMs:   cfg.Discord.MessageDeleteDelay,
			QueueSize:       cfg.Tuning.DeliveryQueueSize,
			CacheDir:        filepath.Join(cfg.BaseDir, "config", ".cache"),
			TileProviderURL: cfg.Geocoding.StaticProviderURL,
			TileInternalURL: cfg.Geocoding.StaticInternalURL,
			Queue: delivery.QueueConfig{
				ConcurrentDiscord:  cfg.Tuning.ConcurrentDiscordDestinations,
				ConcurrentWebhook:  cfg.Tuning.ConcurrentDiscordWebhooks,
				ConcurrentTelegram: cfg.Tuning.ConcurrentTelegramDestinations,
				OnDisabled: func(target, name, jobType string) {
					proc.disableUserForDeliveryFailure(target, name, jobType)
				},
				RateLimiter:    proc.rateLimiter,
				RateLimitHooks: proc,
			},
		})
		if err != nil {
			log.Warnf("Delivery dispatcher init failed: %s", err)
		} else {
			// Wire the snapshot store into the dispatcher before Start so the
			// first delivered message can record its snapshot. Safe to call
			// with nil — the dispatcher treats nil as "snapshots disabled".
			proc.dispatcher.SetSnapshotStore(proc.snapshotStore)
			// Drop snapshots when their host message ages out of the
			// tracker. The hook fires for every TTL-expired entry
			// (clean or not) — snapshots are useless once the alert is
			// no longer in the user's window of interest. The safety
			// sweep is the second line of defence for snapshots that
			// outlive a missed callback.
			if proc.snapshotStore != nil && proc.dispatcher.MessageTracker() != nil {
				store := proc.snapshotStore
				proc.dispatcher.MessageTracker().SetEvictionHook(func(target, sentID string) {
					// MessageTracker stores the composite SentMessage.ID
					// ("bot/channelID:discordMsgID"); snapshots are keyed
					// by the bare platform message id. Extract before
					// computing the key so clean-deletion drops the right
					// entry.
					msgID := delivery.ExtractMessageIDForSnapshot(sentID)
					if err := store.Delete(proc.ctx, snapshots.MakeKey(target, msgID)); err != nil {
						log.Debugf("snapshots: delete on tracker evict %s/%s: %v", target, sentID, err)
					}
				})
			}
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

	// Quest summary scheduler — only constructed when the feature flag is on.
	// The buffer is loaded/saved regardless so toggling the flag back on does
	// not lose entries captured before the toggle.
	if cfg.Tracking.QuestSummaryEnabled {
		proc.summaryScheduler = NewSummaryScheduler(
			schedulerConfig{
				Locale:           cfg.General.Locale,
				BufferMaxAgeSecs: int64(cfg.Tracking.QuestSummaryBufferTTLHours) * 3600,
				DefaultTimezone:  cfg.General.DefaultTimezone,
			},
			stateMgr,
			proc.summaryBuffer,
			proc.DispatchQuestSummary,
		)
		proc.summaryScheduler.Start()
		log.Infof("Summary scheduler enabled")
	} else {
		log.Infof("Summary scheduler disabled (tracking.quest_summary_enabled=false)")
	}

	// HTTP server — Gin router.
	// UseRawPath + UnescapePathValues lets :id capture percent-encoded
	// path params that contain forward slashes (webhook URLs used as
	// human IDs, e.g. /api/humans/https%3A%2F%2Fdiscord.com%2F...).
	// Without this, Go's HTTP layer decodes %2F before gin routes, and
	// the webhook URL fans out into multiple path segments → 404.
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.UseRawPath = true
	r.UnescapePathValues = true
	r.Use(gin.Recovery())
	r.Use(api.CORSMiddleware())
	r.Use(api.RequestLogger())
	r.Use(api.IPFilter(cfg.Processor.IPWhitelist, cfg.Processor.IPBlacklist))

	// Webhook receiver
	var webhookLogger io.Writer
	if cfg.WebhookLogging.Enabled && cfg.WebhookLogging.Filename != "" {
		maxSize := cfg.WebhookLogging.MaxSize
		if maxSize == 0 {
			maxSize = 100
		}
		lj := &lumberjack.Logger{
			Filename:   cfg.WebhookLogging.Filename,
			MaxSize:    maxSize,
			MaxAge:     cfg.WebhookLogging.MaxAge,
			MaxBackups: cfg.WebhookLogging.MaxBackups,
			Compress:   cfg.WebhookLogging.Compress,
		}
		webhookLogger = lj

		// Timer-based rotation (e.g. hourly) in addition to size-based
		if cfg.WebhookLogging.RotateInterval > 0 {
			go func() {
				ticker := time.NewTicker(time.Duration(cfg.WebhookLogging.RotateInterval) * time.Minute)
				defer ticker.Stop()
				for range ticker.C {
					lj.Rotate() //nolint:errcheck
				}
			}()
		}
		log.Infof("Webhook logging enabled: %s (rotate every %dm)", cfg.WebhookLogging.Filename, cfg.WebhookLogging.RotateInterval)
	}
	webhookHandler := webhook.NewHandler(proc, webhookLogger)

	// Public endpoints (no auth)
	r.POST("/", webhookHandler.GinHandler())
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

	// Wire the huma API: serves /openapi.json and /docs publicly, and now serves
	// the migrated in-place endpoints (reloads, with more to come) on the shared
	// instance mounted on the authenticated /api group.
	humaAPI := api.NewHumaAPI(r, apiGroup, buildVersion)

	// NOTE: every api.RegisterX call below also lives in registerAllHumaOpsForTest
	// (internal/api/huma_openapi_golden_test.go), which drives the package-api
	// golden OpenAPI spec test. The six autocreate ops (registerAutocreateHuma /
	// registerAutocreateTemplatesHuma, registered further down) can't live in
	// package api — they depend on *discordbot.Bot, and api → discordbot →
	// bot/commands → api is an import cycle — so they're guarded by a sibling
	// golden in this package (huma_autocreate_golden_test.go). When you add,
	// remove, or rename a huma op here, mirror it in the matching golden test and
	// regenerate:
	//   UPDATE_GOLDEN=1 go test ./internal/api/ -run TestOpenAPIGolden
	//   UPDATE_GOLDEN=1 go test ./cmd/processor/ -run TestAutocreateOpenAPIGolden

	// Reload (migrated to huma, in place — same paths, same {"status":"ok"} body).
	// Each variant carries a distinct summary/description so /docs documents
	// exactly what it reloads. GET and POST share the same text per path.
	const (
		stateReloadSummary = "Reload tracking state from the database"
		stateReloadDesc    = "Reloads tracking state (all registered humans plus every tracking rule across all types) from MySQL into the " +
			"in-memory state snapshot, then atomically swaps it in. Existing geofence data (GeoJSON files / Koji) is preserved and NOT " +
			"re-fetched — use /geofence/reload for that. Returns {\"status\":\"ok\"} once the swap completes."

		geofenceReloadSummary = "Full reload including geofences from disk + Koji"
		geofenceReloadDesc    = "Performs a FULL reload: re-reads geofence GeoJSON files from disk and re-fetches Koji geofences, rebuilds the " +
			"spatial index, then reloads tracking state (humans + all tracking rules) from MySQL and atomically swaps the new snapshot in. " +
			"Heavier than /reload (which skips the geofence step). Returns {\"status\":\"ok\"} on success."

		dtsReloadSummary = "Reload DTS templates and partials from disk"
		dtsReloadDesc    = "Reloads DTS message templates and Handlebars partials from config/dts.json and config/dts/ (falling back to the " +
			"bundled fallbacks/ defaults), rebuilding the in-memory template set. Does NOT touch tracking state or geofences. " +
			"Returns {\"status\":\"ok\"} once templates are reloaded."
	)
	api.RegisterReload(humaAPI, "post-reload", http.MethodPost, "/reload", stateReloadSummary, stateReloadDesc, func() error { return state.Load(stateMgr, database, summaryScheduleStore) })
	api.RegisterReload(humaAPI, "get-reload", http.MethodGet, "/reload", stateReloadSummary, stateReloadDesc, func() error { return state.Load(stateMgr, database, summaryScheduleStore) })
	api.RegisterReload(humaAPI, "post-geofence-reload", http.MethodPost, "/geofence/reload", geofenceReloadSummary, geofenceReloadDesc, func() error { return state.LoadWithGeofences(stateMgr, database, summaryScheduleStore, cfg.Geofence) })
	api.RegisterReload(humaAPI, "get-geofence-reload", http.MethodGet, "/geofence/reload", geofenceReloadSummary, geofenceReloadDesc, func() error { return state.LoadWithGeofences(stateMgr, database, summaryScheduleStore, cfg.Geofence) })

	// Weather, stats, geocode (migrated to huma, in place — same paths, same
	// success JSON, problem+json errors). test stays on gin below.
	api.RegisterWeather(humaAPI, proc.weather)
	api.RegisterStatsRarity(humaAPI, proc.stats.ExportGroups)
	api.RegisterStatsShiny(humaAPI, proc.stats.ExportShinyStats)
	api.RegisterStatsShinyPossible(humaAPI, proc.stats.ExportShinyPossible)
	api.RegisterGeocode(humaAPI, proc.enricher.Geocoder)

	// poracle-test (migrated to huma, in place — same path, same {"status":"ok"}
	// body, open `webhook` request part, problem+json errors).
	api.RegisterTest(humaAPI, proc)

	// Geofence data and tile generation endpoints
	tileDeps := api.TileDeps{
		StaticMap: proc.enricher.StaticMap,
		StateMgr:  stateMgr,
		ImgUicons: proc.enricher.ImgUicons,
		Weather:   proc.weather,
	}
	// Geofence data reads (migrated to huma, in place — same paths, same
	// {status, X} bodies, problem+json errors). Registered at full paths
	// relative to /api. The tile routes below stay on gin.
	api.RegisterGeofenceHash(humaAPI, stateMgr)
	api.RegisterGeofenceGeoJSON(humaAPI, stateMgr)
	api.RegisterGeofenceAll(humaAPI, stateMgr)
	// Geofence tile-URL endpoints (migrated to huma, in place — same paths,
	// same {status, url} bodies, problem+json errors).
	api.RegisterTileEndpoints(humaAPI, api.NewHumaTileDeps(tileDeps))

	// Tracking CRUD endpoints (registered after proc is created so enricher/scanner are available)
	defaultTemplate := "1"
	if cfg.General.DefaultTemplateName != nil {
		defaultTemplate = fmt.Sprintf("%v", cfg.General.DefaultTemplateName)
	}
	trackingDeps := &api.TrackingDeps{
		DB:       database,
		Humans:   humanStore,
		Tracking: trackingStores,
		StateMgr: stateMgr,
		RowText: &rowtext.Generator{
			GD:                  proc.enricher.GameData,
			Translations:        proc.enricher.Translations,
			DefaultTemplateName: defaultTemplate,
			Scanner:             proc.scanner,
		},
		Config:       cfg,
		Translations: proc.enricher.Translations,
		Dispatcher:   proc.dispatcher,
		Summaries:    summaryScheduleStore,
		Mutes:        proc.muteStore,
		ReloadFunc:   proc.triggerReload,
	}
	// Strict v2 tracking surface (huma). v1 gin routes below are left untouched
	// (frozen). The v2 pokemon endpoints are the worked example the other 10
	// tracking types fan out from.
	api.RegisterV2TrackingPokemon(humaAPI, trackingDeps)
	api.RegisterV2TrackingNest(humaAPI, trackingDeps)
	api.RegisterV2TrackingLure(humaAPI, trackingDeps)
	api.RegisterV2TrackingMaxbattle(humaAPI, trackingDeps)
	api.RegisterV2TrackingGym(humaAPI, trackingDeps)
	api.RegisterV2TrackingFort(humaAPI, trackingDeps)
	api.RegisterV2TrackingRaid(humaAPI, trackingDeps)
	api.RegisterV2TrackingEgg(humaAPI, trackingDeps)
	api.RegisterV2TrackingQuest(humaAPI, trackingDeps)
	api.RegisterV2TrackingInvasion(humaAPI, trackingDeps)
	api.RegisterV2TrackingIncident(humaAPI, trackingDeps)
	// MUST follow every per-type register above so the snapshot provider
	// registry is fully populated before the snapshot endpoint reads it.
	api.RegisterV2TrackingSnapshot(humaAPI, trackingDeps)

	// Strict v2 humans surface (huma). Re-exposes v1's human actions with typed
	// inputs + problem+json; v1 gin human routes below stay frozen.
	api.RegisterV2Humans(humaAPI, trackingDeps)

	// Strict v2 saved-locations CRUD (huma). Re-exposes v1's location actions
	// plus a new PUT (update coords); v1 gin location routes stay frozen.
	api.RegisterV2Locations(humaAPI, trackingDeps)

	// Strict v2 profiles surface (huma, sub-resource of human). Re-exposes v1's
	// profile actions with a strict typed active_hours schema; v1 gin profile
	// routes stay frozen.
	api.RegisterV2Profiles(humaAPI, trackingDeps)

	// Strict v2 mutes surface (huma, sub-resource of human) over the in-memory
	// mute store — net-new in v2 (the bot/buttons were the only mute writers).
	api.RegisterV2Mutes(humaAPI, trackingDeps)

	tracking := apiGroup.Group("/tracking")
	tracking.GET("/pokemon/refresh", api.HandleReload(func() error {
		return state.Load(stateMgr, database, summaryScheduleStore)
	}))
	// Pokemon tracking
	tracking.GET("/pokemon/:id", api.HandleGetMonster(trackingDeps))
	tracking.POST("/pokemon/:id", api.HandleCreateMonster(trackingDeps))
	tracking.DELETE("/pokemon/:id/byUid/:uid", api.HandleDeleteMonster(trackingDeps))
	tracking.POST("/pokemon/:id/delete", api.HandleBulkDeleteMonster(trackingDeps))
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
		Humans: humanStore,
	}

	// Strict v2 roles surface (huma): list/add(POST)/remove(DELETE)/admin-roles.
	// Re-exposes v1's role actions with REST-clean verbs; v1 gin role routes
	// below stay frozen.
	api.RegisterV2Roles(humaAPI, roleDeps)

	humans := apiGroup.Group("/humans")
	humans.GET("/one/:id", api.HandleGetOneHuman(trackingDeps))
	humans.GET("/:id", api.HandleGetHumanAreas(trackingDeps))
	humans.GET("/:id/roles", api.HandleGetRoles(roleDeps))
	humans.GET("/:id/getAdministrationRoles", api.HandleGetAdministrationRoles(roleDeps))
	humans.GET("/:id/checkLocation/:lat/:lon", api.HandleCheckLocation(trackingDeps))
	humans.GET("/:id/locations", api.HandleListLocations(trackingDeps))
	humans.GET("/:id/locations/:label", api.HandleGetLocation(trackingDeps))
	humans.POST("/:id/locations/add", api.HandleAddLocation(trackingDeps))
	humans.POST("/:id/locations/:label/delete", api.HandleDeleteLocation(trackingDeps))
	humans.POST("", api.HandleCreateHuman(trackingDeps))
	humans.POST("/:id/start", api.HandleStartHuman(trackingDeps))
	humans.POST("/:id/stop", api.HandleStopHuman(trackingDeps))
	humans.POST("/:id/adminDisabled", api.HandleAdminDisabled(trackingDeps))
	humans.POST("/:id/language", api.HandleSetLanguage(trackingDeps))
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

	// Summary schedule endpoints (CRUD over summary_schedules + trigger).
	// All five sit behind the same x-poracle-secret middleware as the rest
	// of /api. The endpoints are always live — `quest_summary_enabled` only
	// gates the scheduler tick / matcher routing, not the schedule storage,
	// so operators can prepare schedules in advance and flip the feature
	// flag without losing data. Handlers return 503 only when the
	// underlying store wasn't constructed (an early-init failure), not
	// when the feature flag is off.
	summaryDeps := &api.SummaryDeps{
		Schedules:  proc.SummarySchedules(),
		Dispatch:   proc.DispatchQuestSummary,
		ReloadFunc: proc.triggerReload,
	}
	// All summary ops (reads/delete/trigger + the active_hours upsert) run on
	// the shared huma instance.
	api.RegisterSummaries(humaAPI, summaryDeps)
	api.RegisterSummarySet(humaAPI, summaryDeps)

	// reloadDTS reloads DTS templates and returns the number of loaded entries.
	// It is used by both the HTTP /api/dts/reload handlers and the BotDeps closure
	// so the logic lives in exactly one place.
	reloadDTS := func() (int, error) {
		if proc.dtsRenderer == nil {
			return 0, errors.New("DTS renderer not configured")
		}
		ts := proc.dtsRenderer.Templates()
		if err := ts.Reload(
			filepath.Join(cfg.BaseDir, "config"),
			filepath.Join(cfg.BaseDir, "fallbacks"),
		); err != nil {
			return 0, err
		}
		return len(ts.FilteredEntries("", "", "", "")), nil
	}

	// DTS template endpoints
	if proc.dtsRenderer != nil {
		dtsConfigDir := filepath.Join(cfg.BaseDir, "config")
		api.RegisterReload(humaAPI, "post-dts-reload", http.MethodPost, "/dts/reload", dtsReloadSummary, dtsReloadDesc, func() error { _, err := reloadDTS(); return err })
		api.RegisterReload(humaAPI, "get-dts-reload", http.MethodGet, "/dts/reload", dtsReloadSummary, dtsReloadDesc, func() error { _, err := reloadDTS(); return err })

		// DTS render/save/enrich/sendtest + config/templates (migrated to huma,
		// in place — same paths, same freeform success JSON, problem+json
		// errors). Open request bodies (view map, polymorphic template,
		// arbitrary webhook). Stay gated behind dtsRenderer != nil.
		api.RegisterDTSWrites(
			humaAPI,
			proc.dtsRenderer.Templates(),
			proc,
			proc.dispatcher,
			proc.dtsRenderer,
		)

		// DTS editor read endpoints (migrated to huma, in place — same paths,
		// same success JSON, problem+json errors). Stay gated behind
		// dtsRenderer != nil so they don't exist when DTS rendering is disabled.
		api.RegisterDTSReads(
			humaAPI,
			proc.dtsRenderer.Emoji(),
			proc.dtsRenderer.Templates(),
			dtsConfigDir,
			filepath.Join(cfg.BaseDir, "fallbacks"),
		)
	}

	// Config and master data endpoints
	configDeps := api.ConfigDeps{
		Cfg:       cfg,
		ConfigDir: filepath.Join(cfg.BaseDir, "config"),
		ReloadFn: func() {
			// Hot-reloadable config changes are already applied in-memory via ApplyOverrides.
			// Trigger a state reload so matching rules pick up any changed config values.
			proc.triggerReload()
		},
	}
	// Config schema (migrated to huma, in place — same {status, sections} body).
	api.RegisterConfigSchema(humaAPI)
	// Config poracleWeb / values / validate (migrated to huma, in place — same
	// paths, same success JSON, open bodies, problem+json errors). POST
	// /config/values preserves the config.toml rewrite side effect.
	api.RegisterConfigPoracleWeb(humaAPI, cfg)
	api.RegisterConfigValues(humaAPI, configDeps)
	api.RegisterConfigSave(humaAPI, configDeps)
	api.RegisterConfigValidate(humaAPI, configDeps)
	// Masterdata reads (migrated to huma, in place — typed maps re-marshalled to
	// the same JSON the gin handlers produced).
	api.RegisterMasterdataMonsters(humaAPI, proc.enricher.GameData, proc.enricher.Translations)
	api.RegisterMasterdataGrunts(humaAPI, proc.enricher.GameData)

	// Snapshot inspection (migrated to huma, in place) — admin-only via the
	// api_secret middleware. Returns 503 if [snapshots] enabled = false; 404 on
	// a miss. See docs/buttons-and-snapshots/.
	api.RegisterSnapshotGet(humaAPI, proc.snapshotStore)

	// Button action registry — config editor reads this to surface the
	// dropdown of action choices + their accepted scopes/params. Migrated to
	// huma, in place — same path, same {"actions":[...]} body. Registered
	// unconditionally (outside the dtsRenderer block, matching legacy).
	api.RegisterButtonActions(humaAPI, proc.buttonActions)

	// Resolution cache — populated after bot init below
	resolveCache := api.NewResolveCache()

	// Delivery endpoint — accepts pre-rendered jobs (migrated to huma, in place
	// — same paths, same {"status":"ok","queued":N} body, []Job request,
	// problem+json errors). /postMessage is a legacy alias serving identically;
	// only the docs differ so the OpenAPI explains which one to use.
	const (
		deliverMessagesSummary = "Deliver pre-rendered messages"
		deliverMessagesDesc    = "Canonical delivery endpoint. Accepts an array of pre-rendered delivery jobs — each job carries a destination " +
			"(target + type) and a `message` field holding the already-rendered platform payload (arbitrary JSON) — and dispatches them to the " +
			"delivery system. Jobs missing target or type are silently skipped. Returns {\"status\":\"ok\",\"queued\":N} where N is the number of " +
			"jobs accepted. Responds 503 when the delivery dispatcher is not configured."

		postMessageSummary = "Deliver pre-rendered messages (legacy alias)"
		postMessageDesc    = "Legacy/backward-compatibility alias of POST /deliverMessages — identical request body and behaviour (dispatches an " +
			"array of pre-rendered delivery jobs, skips jobs missing target/type, returns {\"status\":\"ok\",\"queued\":N}, 503 when the dispatcher " +
			"is unconfigured). Retained for older clients; new clients should use /deliverMessages."
	)
	api.RegisterDeliverMessages(humaAPI, "post-deliver-messages", "/deliverMessages", deliverMessagesSummary, deliverMessagesDesc, proc.dispatcher)
	api.RegisterDeliverMessages(humaAPI, "post-message", "/postMessage", postMessageSummary, postMessageDesc, proc.dispatcher)

	// Command framework — shared by API endpoint and Discord/Telegram bots
	var cmdLanguages []string
	if len(cfg.General.AvailableLanguages) > 0 {
		for code := range cfg.General.AvailableLanguages {
			cmdLanguages = append(cmdLanguages, code)
		}
		// Ensure English and default locale are included
		hasEn := false
		hasLocale := false
		for _, l := range cmdLanguages {
			if l == "en" {
				hasEn = true
			}
			if l == cfg.General.Locale {
				hasLocale = true
			}
		}
		if !hasEn {
			cmdLanguages = append(cmdLanguages, "en")
		}
		if cfg.General.Locale != "" && !hasLocale {
			cmdLanguages = append(cmdLanguages, cfg.General.Locale)
		}
	} else {
		cmdLanguages = []string{"en"}
		if cfg.General.Locale != "" && cfg.General.Locale != "en" {
			cmdLanguages = append(cmdLanguages, cfg.General.Locale)
		}
	}
	cmdPrefix := cfg.Discord.Prefix
	if cmdPrefix == "" {
		cmdPrefix = "!"
	}
	cmdParser := bot.NewParser(cmdPrefix, proc.enricher.Translations, cmdLanguages, cfg.General.AvailableLanguages)
	tgParser := bot.NewParser("/", proc.enricher.Translations, cmdLanguages, cfg.General.AvailableLanguages)
	pokemonAliases := bot.LoadPokemonAliases(filepath.Join(cfg.BaseDir, "config"), filepath.Join(cfg.BaseDir, "fallbacks"))
	cmdResolver := bot.NewPokemonResolver(proc.enricher.GameData, proc.enricher.Translations, cmdLanguages, pokemonAliases)
	cmdArgMatcher := bot.NewArgMatcher(proc.enricher.Translations, proc.enricher.GameData, cmdResolver, cmdLanguages)
	cmdRegistry := bot.NewRegistry()
	cmdRegistry.Register(&commands.StartCommand{})
	cmdRegistry.Register(&commands.StopCommand{})
	cmdRegistry.Register(&commands.EggCommand{})
	cmdRegistry.Register(&commands.RaidCommand{})
	cmdRegistry.Register(&commands.TrackCommand{})
	cmdRegistry.Register(&commands.TrackedCommand{})
	cmdRegistry.Register(&commands.UntrackCommand{})
	cmdRegistry.Register(&commands.MuteCommand{})
	cmdRegistry.Register(&commands.UnmuteCommand{})
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
	cmdRegistry.Register(&commands.SummaryCommand{})
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
	cmdRegistry.Register(&commands.PoracleAdminCommand{})

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

	// cmdDTS / cmdEmoji are used both by sharedBotDeps (below) and by the
	// /api/command endpoint that routes through it.
	var cmdDTS *dts.TemplateStore
	var cmdEmoji *dts.EmojiLookup
	if proc.dtsRenderer != nil {
		cmdDTS = proc.dtsRenderer.Templates()
		cmdEmoji = proc.dtsRenderer.Emoji()
	}

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
		// Tracks the last-seen delivery.Dispatcher backpressure counter so the
		// lane-summary block below can detect whether it advanced since the
		// previous tick (escalates the status line to WARN when it has).
		var lastBackpressureSeen int64
		for {
			select {
			case <-periodicDone:
				return
			case <-ticker.C:
			}
			log.Debugf("Periodic reload triggered")
			start := time.Now()
			if err := state.Load(stateMgr, database, summaryScheduleStore); err != nil {
				log.Errorf("Periodic reload failed: %s", err)
				metrics.StateReloads.WithLabelValues("error").Inc()
				proc.adminNoticeThrottled(
					"state.reload.fail",
					fmt.Sprintf(":warning: Periodic state reload failed: %s — tracking rule updates from the DB will pause until this clears.", err),
					5*time.Minute,
				)
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
			// Summary buffer state: how many entries are currently
			// held awaiting the next scheduled dispatch. The same value
			// is exposed as the SummaryBufferedTotal Prometheus gauge so
			// it can be charted/alerted on without scraping log lines.
			if proc.summaryBuffer != nil {
				bufSize := proc.summaryBuffer.Size()
				statusParts = append(statusParts, fmt.Sprintf("Summary: %d buffered", bufSize))
				metrics.SummaryBufferedTotal.Set(float64(bufSize))
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

				total, active, maxDepth, deepestTarget, nearCap := proc.dispatcher.LaneStats()
				statusParts = append(statusParts, fmt.Sprintf("Lanes:%d active, %d queued, deepest=%d (%s), %d near-cap",
					active, total, maxDepth, deepestTarget, nearCap))
				metrics.DeliveryActiveLanes.Set(float64(active))
				metrics.DeliveryLaneQueued.Set(float64(total))
				metrics.DeliveryLaneMaxDepth.Set(float64(maxDepth))
				metrics.DeliveryLanesNearCapacity.Set(float64(nearCap))

				// Escalate to WARN when a lane nears capacity OR backpressure
				// advanced since the last sample — the "developing bad
				// situation" signal.
				bp := proc.dispatcher.BackpressureCount()
				buf := proc.dispatcher.PerRouteBuffer()
				if (buf > 0 && maxDepth >= buf*8/10) || bp > lastBackpressureSeen {
					log.Warnf("[Status] delivery backing up: %d lanes, %d queued, deepest lane %d/%d (%s), %d near-cap, backpressure=%d",
						active, total, maxDepth, buf, deepestTarget, nearCap, bp)
				}
				lastBackpressureSeen = bp
			}
			log.Infof("[Status] %s", strings.Join(statusParts, " | "))
			if webhooks == 0 {
				log.Warn("[Status] No webhooks have been received — check your scanner or networking configuration")
			}
		}
	}()

	// Start Discord bot (if token configured)
	var discordBot *discordbot.Bot
	// Shared bot dependencies — constructed once, passed to both Discord and Telegram bots.
	sharedBotDeps := bot.BotDeps{
		DB:                 database,
		Humans:             humanStore,
		Tracking:           trackingStores,
		Cfg:                cfg,
		StateMgr:           stateMgr,
		GameData:           proc.enricher.GameData,
		Translations:       proc.enricher.Translations,
		Dispatcher:         proc.dispatcher,
		RowText:            trackingDeps.RowText,
		Registry:           cmdRegistry,
		ArgMatcher:         cmdArgMatcher,
		Resolver:           cmdResolver,
		Geocoder:           proc.enricher.Geocoder,
		StaticMap:          proc.enricher.StaticMap,
		Weather:            proc.weather,
		Stats:              proc.stats,
		DTS:                cmdDTS,
		Emoji:              cmdEmoji,
		NLPParser:          nlpParser,
		TestProcessor:      proc,
		ReloadFunc:         proc.triggerReload,
		SummarySchedules:   proc.SummarySchedules(),
		SummaryBuffer:      proc.summaryBuffer,
		SummaryBufferCount: proc.SummaryBufferCount,
		SummaryDispatch:    proc.DispatchQuestSummary,
		Scanner:            proc.scanner,
		RecentActivity:     proc.recentActivity,
		MuteStore:          proc.muteStore,
		SnapshotStore:      proc.snapshotStore,
		ButtonActions:      proc.buttonActions,
		DTSRenderer:        proc.dtsRenderer,
		ReloadDTS:          reloadDTS,
		EmojiReload: func() (int, error) {
			if cmdEmoji == nil {
				return 0, errors.New("emoji config not loaded (DTS renderer not configured)")
			}
			return cmdEmoji.Reload()
		},
		EmojiOperation: func(channelID, guildID string, upload, overwrite bool) []bot.Reply {
			b := discordBotRef
			if b == nil {
				return []bot.Reply{{Text: "Discord bot is not configured on this deployment."}}
			}
			return b.RunEmojiOperation(channelID, guildID, upload, overwrite)
		},
		ReloadGeofence: func() error {
			return state.LoadWithGeofences(stateMgr, database, summaryScheduleStore, cfg.Geofence)
		},
		ReloadState: func() error {
			return state.Load(stateMgr, database, summaryScheduleStore)
		},
		WebhookRate:  webhookHandler.RateSnapshot,
		AlertLimiter: proc.rateLimiter,
		DiscordRate:  proc.dispatcher.DiscordRateSnapshot,
		TelegramRate: proc.dispatcher.TelegramRateSnapshot,
		GeocoderStats: func() geocoding.CacheStats {
			if proc.enricher.Geocoder == nil {
				return geocoding.CacheStats{}
			}
			return proc.enricher.Geocoder.CacheStats()
		},
		GeocoderClear: func() int {
			if proc.enricher.Geocoder == nil {
				return 0
			}
			return proc.enricher.Geocoder.ClearCache()
		},
		// Reconciler / RunReconcile are always non-nil. When Discord is not
		// configured, the closures return discordbot.ErrReconciliationDisabled.
		// Command callers use errors.Is to detect.
		Reconciler: func(userID string) error {
			if discordBot == nil {
				return discordbot.ErrReconciliationDisabled
			}
			return discordBot.ReconcileUserNow(userID)
		},
		RunReconcile: func() error {
			if discordBot == nil {
				return discordbot.ErrReconciliationDisabled
			}
			return discordBot.RunReconciliationNow()
		},
		LogBuffer:    logBuf,
		ProcessStart: proc.ProcessStart(),

		// Slash command lifecycle — always non-nil. When the Discord bot is
		// not up or slash commands are disabled, the closures return
		// slash.ErrSlashNotConfigured so the command handler can emit a
		// friendly operator message.
		SlashSync: func() error {
			d := discordBotSlashDispatcher(discordBot)
			if d == nil {
				return slash.ErrSlashNotConfigured
			}
			return d.SyncCommands()
		},
		SlashForceResync: func() error {
			d := discordBotSlashDispatcher(discordBot)
			if d == nil {
				return slash.ErrSlashNotConfigured
			}
			return d.ForceResync()
		},
		SlashClearGlobal: func() error {
			d := discordBotSlashDispatcher(discordBot)
			if d == nil {
				return slash.ErrSlashNotConfigured
			}
			return d.ClearGlobalCommands()
		},
		SlashClearGuild: func(guildID string) error {
			d := discordBotSlashDispatcher(discordBot)
			if d == nil {
				return slash.ErrSlashNotConfigured
			}
			// ClearGuildCommands clears all configured guilds; to target a
			// single guild we temporarily patch the dispatcher's guild list.
			return d.ClearSingleGuild(guildID)
		},
		SlashStatus: func() (bot.SlashScope, []bot.SlashScope, error) {
			d := discordBotSlashDispatcher(discordBot)
			if d == nil {
				return bot.SlashScope{}, nil, slash.ErrSlashNotConfigured
			}
			global, guilds, err := d.CacheStatus()
			if err != nil {
				return bot.SlashScope{}, nil, err
			}
			gscope := bot.SlashScope{
				Name:         "global",
				LastSyncedAt: global.SyncedAt,
				Fingerprint:  global.Fingerprint,
			}
			var guildScopes []bot.SlashScope
			for name, entry := range guilds {
				guildScopes = append(guildScopes, bot.SlashScope{
					Name:         name,
					LastSyncedAt: entry.SyncedAt,
					Fingerprint:  entry.Fingerprint,
				})
			}
			return gscope, guildScopes, nil
		},
	}

	// /api/command — routes through the full BotDeps graph so every admin
	// closure (WebhookRate, AlertLimiter, Reconciler, SlashSync, etc.) is
	// populated. Parser is added on top of sharedBotDeps, matching the
	// per-bot pattern used for Discord and Telegram below.
	{
		apiCmdDeps := sharedBotDeps
		apiCmdDeps.Parser = cmdParser
		api.RegisterCommand(humaAPI, &apiCmdDeps)
	}

	gatewayToken := cfg.Discord.DiscordGatewayToken()
	if cfg.Discord.Enabled && gatewayToken != "" {
		deps := sharedBotDeps
		deps.Parser = cmdParser
		// Log which side of the split we're on so operators can
		// confirm their command_token is being picked up. Mask the
		// token itself (first 8 chars + ellipsis is enough to
		// distinguish, no full secret in logs).
		mode := "single-bot"
		if cfg.Discord.CommandToken != "" {
			mode = "split (using [discord] command_token)"
		}
		log.Infof("Discord bot starting in %s mode", mode)
		dbot, err := discordbot.New(discordbot.Config{
			Token:            gatewayToken,
			BotDeps:          deps,
			ForceSyncSlash:   *forceSyncSlash,
			ClearGlobalSlash: *clearGlobalSlash,
			ClearGuildSlash:  *clearGuildSlash,
		})
		if err != nil {
			// When command_token is set, treat a gateway failure as
			// fatal: the operator explicitly configured a split-bot
			// setup, and silently falling back to "warn and continue"
			// would hide a real misconfiguration (typo, revoked
			// token) behind seemingly-working alerts. The historic
			// behaviour (warn-and-continue) is preserved for the
			// single-bot case where the delivery side can still work
			// even with a broken gateway.
			if cfg.Discord.CommandToken != "" {
				log.Fatalf("Discord command_token failed to start the gateway bot: %v — fix the token or unset [discord] command_token to fall back to single-bot operation", err)
			}
			log.Warnf("Discord bot failed to start: %v", err)
		} else {
			discordBot = dbot
			discordBotRef = dbot
			proc.discordBot = dbot
			// Replay any startup-time issues that happened before the bot
			// was up so the operator sees them in the admin channel.
			if proc.dtsInitErr != nil {
				proc.adminNotice(fmt.Sprintf(
					":rotating_light: DTS renderer initialization failed at startup — alerts will not be sent! Error: %s",
					proc.dtsInitErr,
				))
			}
			// Route DTS render errors to the admin channel, throttled per
			// type|platform|language|template so a broken template doesn't
			// flood the channel on every webhook event.
			if proc.dtsRenderer != nil {
				proc.dtsRenderer.SetErrorNoticer(func(key, msg string) {
					proc.adminNoticeThrottled(key, msg, time.Hour)
				})
			}
		}
	}

	// Start Telegram bot (if token configured)
	var telegramBot *telegrambot.Bot
	telegramTokens := cfg.Telegram.TelegramTokens()
	if cfg.Telegram.Enabled && len(telegramTokens) > 0 && telegramTokens[0] != "" {
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

	// Register resolve endpoint now that bots are initialized
	resolveDeps := api.ResolveDeps{
		Cache:  resolveCache,
		Humans: humanStore,
	}
	if discordBot != nil {
		resolveDeps.DiscordSession = discordBot.Session()
	}
	if telegramBot != nil {
		resolveDeps.TelegramAPI = telegramBot.API()
	}
	// Resolve (migrated to huma, in place — same path, same freeform success
	// body, open request, problem+json errors).
	api.RegisterResolve(humaAPI, resolveDeps)
	// autocreate run / delete-template / templates-schema migrated to huma,
	// in place — same paths, same success JSON, problem+json errors. These six
	// autocreate ops are guarded by huma_autocreate_golden_test.go (this package),
	// not the package-api golden — see the NOTE near the humaAPI setup above.
	registerAutocreateHuma(humaAPI, cfg, discordBot)
	// autocreate templates GET/POST + validate migrated to huma, in place —
	// open (raw-JSON) bodies, same success JSON, problem+json errors.
	registerAutocreateTemplatesHuma(humaAPI, cfg)

	// Backup-cleanup sweeper: walks config/backups/ on startup and every
	// 24h, removes files older than the retention window.
	stopBackupSweeper := backup.StartCleanupSweeper(
		context.Background(),
		filepath.Join(cfg.BaseDir, "config"),
		backup.Retention,
	)

	// Freeze the startup buffer: everything logged from here on goes to
	// the rolling buffer. MarkStartupComplete is placed right before the
	// HTTP server starts accepting requests so all subsystem initialisation
	// (migrations, state load, bots, …) is captured in the startup snapshot.
	logBuf.MarkStartupComplete()

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
	if err := server.Shutdown(ctx); err != nil {
		log.Warnf("HTTP server shutdown: %v", err)
	}
	log.Infof("HTTP server stopped")

	// 2. Stop bots (no more command processing)
	close(periodicDone)
	stopBackupSweeper()
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

func readBuildInfo() (version, commit, branch, date string) {
	// Single source of truth: ldflag-injected build vars (Docker / local build
	// scripts) with a fallback to Go's embedded VCS metadata. See processor.BuildInfo.
	return processor.BuildInfo()
}

// ProcessorService ties together all matching/tracking components.
type ProcessorService struct {
	cfg              *config.Config
	stateMgr         *state.Manager
	database         *sqlx.DB
	weather          *tracker.WeatherTracker
	weatherCares     *tracker.WeatherCareTracker
	encounters       *tracker.EncounterTracker
	duplicates       *tracker.DuplicateCache
	stats            *tracker.StatsTracker
	gymState         *tracker.GymStateTracker
	summaryBuffer    *tracker.SummaryBuffer
	pokemonMatcher   *matching.PokemonMatcher
	raidMatcher      *matching.RaidMatcher
	invasionMatcher  *matching.InvasionMatcher
	questMatcher     *matching.QuestMatcher
	lureMatcher      *matching.LureMatcher
	gymMatcher       *matching.GymMatcher
	nestMatcher      *matching.NestMatcher
	fortMatcher      *matching.FortMatcher
	maxbattleMatcher *matching.MaxbattleMatcher
	pvpCfg           *pvp.Config
	activePokemon    *tracker.ActivePokemonTracker
	recentActivity   *tracker.RecentActivity
	pokemonTypes     *gamedata.PokemonTypes
	enricher         *enrichment.Enricher
	dtsRenderer      *dts.Renderer
	dispatcher       *delivery.Dispatcher
	humans           store.HumanStore
	summarySchedules store.SummaryScheduleStore
	summaryScheduler *SummaryScheduler
	scanner          scanner.Scanner
	rateLimiter      *ratelimit.Limiter
	validator        validation.Validator
	translations     *i18n.Bundle
	renderCh         chan RenderJob
	renderWg         sync.WaitGroup
	reloadMu         sync.Mutex
	reloadTimer      *time.Timer
	workerPool       chan struct{}
	wg               sync.WaitGroup
	ctx              context.Context
	cancel           context.CancelFunc

	// discordBot is set by main after the bot starts so helpers can post
	// to the admin channel. May be nil if Discord is disabled.
	discordBot *discordbot.Bot

	// dtsInitErr remembers a startup-time DTS-renderer init failure so
	// main can post an admin notice once the Discord bot is up. nil
	// when DTS init succeeded.
	dtsInitErr error

	// procStart is captured in NewProcessorService and used by
	// !poracle-admin status to report uptime. ProcessorService is the
	// natural anchor point — it's constructed once at startup and lives
	// for the lifetime of the process.
	procStart time.Time

	// snapshotStore is the opt-in per-delivery enrichment store (#108).
	// Nil when [snapshots] enabled = false; senders + render path check
	// for nil and skip both the snapshot write and the button-render side
	// of the work. snapshotSweeper is paired with the store and runs the
	// safety sweep; both are nil together.
	snapshotStore   snapshots.Store
	snapshotSweeper *snapshots.Sweeper

	// muteStore is the in-memory per-user alert-suppression store (#109).
	// Always allocated — mutes are an opt-in user action regardless of
	// whether snapshots are enabled. muteSweeper reclaims expired entries
	// on a fixed cadence; both are nil only in tests that skip init.
	muteStore   *mute.Store
	muteSweeper *mute.Sweeper

	// buttonActions is the dispatch registry for button-triggered state
	// changes (#109). Always allocated and built-in handlers registered
	// at startup; nil only in tests that skip init.
	buttonActions *buttonactions.Registry
}

// ProcessStart returns the timestamp at which this ProcessorService was
// constructed. Used by !poracle-admin status for uptime display.
func (ps *ProcessorService) ProcessStart() time.Time {
	if ps == nil {
		return time.Time{}
	}
	return ps.procStart
}

// SummaryBufferCount returns the number of buffered entries in the
// (humanID, alertType) bucket. Returns 0 when the buffer is nil.
func (ps *ProcessorService) SummaryBufferCount(humanID, alertType string) int {
	if ps == nil || ps.summaryBuffer == nil {
		return 0
	}
	return len(ps.summaryBuffer.List(humanID, alertType))
}

// SummarySchedules returns the typed CRUD store backing
// summary_schedules. nil before init completes.
func (ps *ProcessorService) SummarySchedules() store.SummaryScheduleStore {
	if ps == nil {
		return nil
	}
	return ps.summarySchedules
}

// adminNotice posts to the admin channel if the bot is up. No-op when
// Discord is disabled or the admin_channel_id config is empty.
func (ps *ProcessorService) adminNotice(msg string) {
	if ps.discordBot != nil {
		ps.discordBot.PostAdminNotice(msg)
	}
}

// adminNoticeThrottled posts to the admin channel with throttling. See
// (*discordbot.Bot).PostAdminNoticeThrottled for semantics.
func (ps *ProcessorService) adminNoticeThrottled(key, msg string, ttl time.Duration) {
	if ps.discordBot != nil {
		ps.discordBot.PostAdminNoticeThrottled(key, msg, ttl)
	}
}

func NewProcessorService(cfg *config.Config, stateMgr *state.Manager, database *sqlx.DB) *ProcessorService {
	pvpCfg := &pvp.Config{
		LevelCaps:                  cfg.PVP.LevelCaps,
		PVPFilterMaxRank:           cfg.PVP.PVPFilterMaxRank,
		PVPEvolutionDirectTracking: cfg.PVP.PVPEvolutionDirectTracking,
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
	if gd.Util != nil {
		gamedata.InitWeatherTypeBoost(gd.Util)
	}

	var activePokemon *tracker.ActivePokemonTracker
	var pokemonTypes *gamedata.PokemonTypes
	if cfg.Weather.ShowAlteredPokemon {
		pokemonTypes = gamedata.PokemonTypesFromGameData(gd.Monsters)
		activePokemon = tracker.NewActivePokemonTracker(50)
		log.Info("Active pokemon tracking enabled (from game data)")
	}

	if !geo.IsLocaleSupported(cfg.Locale.TimeFormat) {
		log.Warnf("Unsupported locale.timeformat %q — Moment.js shortcuts (LTS, L, etc.) will fall back to en-gb. Supported locales: %v",
			cfg.Locale.TimeFormat, geo.SupportedLocales())
	}

	// Idle expiry has to outlast the configured forecast cadence, or
	// forecast-only cells get reclaimed between pushes.
	weatherTracker := tracker.NewWeatherTracker(
		tracker.WithForecastRefreshInterval(cfg.Weather.ForecastRefreshInterval),
	)
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
		DiademURL:    cfg.General.DiademURL,
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

	// Fallback icon URLs
	enricher.FallbackImgURL = cfg.Fallbacks.ImgURL
	enricher.FallbackImgWeather = cfg.Fallbacks.ImgURLWeather
	enricher.FallbackImgEgg = cfg.Fallbacks.ImgURLEgg
	enricher.FallbackImgGym = cfg.Fallbacks.ImgURLGym
	enricher.FallbackImgPokestop = cfg.Fallbacks.ImgURLPokestop
	enricher.FallbackPokestopURL = cfg.Fallbacks.PokestopURL

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
			InternalURL:                cfg.Geocoding.StaticInternalURL,
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
			PregenTTL:                  cfg.Tuning.TileserverPregenTTL,
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
					TTL:    v.TTL,
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
			IncludeCountry:   cfg.Locale.AddressIncludeCountry,
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

	// Nearest-street-intersection lookups (GeoNames). Shares the reverse-
	// geocoder's pogreb cache when one exists, so intersection results live in
	// the same on-disk DB (separate key namespace) and repeat sightings at a
	// fixed stop don't re-hit GeoNames.
	if len(cfg.Geocoding.IntersectionUsers) > 0 {
		var sharedCache *geocoding.Cache
		if geocoder != nil {
			sharedCache = geocoder.Cache()
		}
		enricher.Intersection = geocoding.NewIntersection(geocoding.IntersectionConfig{
			Usernames:        cfg.Geocoding.IntersectionUsers,
			Cache:            sharedCache,
			CacheDetail:      cfg.Geocoding.CacheDetail,
			TimeoutMs:        cfg.Tuning.GeocodingTimeout,
			Concurrency:      cfg.Tuning.GeocodingConcurrency,
			FailureThreshold: cfg.Tuning.GeocodingFailureThreshold,
			CooldownMs:       cfg.Tuning.GeocodingCooldownMs,
		})
		log.Infof("GeoNames intersection lookups enabled (%d user(s), cache shared=%t)", len(cfg.Geocoding.IntersectionUsers), sharedCache != nil)
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
		// The forecast client keys its own maps by the same cell ids, so it
		// has to release them when the tracker reclaims a cell.
		weatherTracker.SetOnEvict(awClient.ForgetCells)
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
		DMSummaryLimit:      cfg.AlertLimits.DMSummaryLimit,
		ChannelSummaryLimit: cfg.AlertLimits.ChannelSummaryLimit,
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
		AltLanguage:         cfg.Locale.Language,
		DefaultTemplateName: dtsDefaultTemplate,
		MinAlertTime:        cfg.General.AlertMinimumTime,
		ShlinkURL:           shlinkURL,
		ShlinkKey:           shlinkKey,
		ShlinkDomain:        shlinkDomain,
		DTSDictionary:       cfg.General.DTSDictionary,
	})
	var dtsInitErr error
	if err != nil {
		log.Errorf("DTS renderer initialization failed (alerts will not be sent!): %s", err)
		dtsRenderer = nil
		dtsInitErr = err
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

	ps := &ProcessorService{
		cfg:          cfg,
		stateMgr:     stateMgr,
		database:     database,
		ctx:          ctx,
		cancel:       cancel,
		renderCh:     renderCh,
		enricher:     enricher,
		dtsRenderer:  dtsRenderer,
		dtsInitErr:   dtsInitErr,
		procStart:    time.Now(),
		scanner:      scannerInstance,
		weather:      weatherTracker,
		weatherCares: tracker.NewWeatherCareTracker(),
		encounters:   tracker.NewEncounterTracker(),
		duplicates:   tracker.NewDuplicateCache(),
		stats:        statsTracker,
		gymState:     tracker.NewGymStateTracker(filepath.Join(cfg.BaseDir, "config", ".cache")),
		summaryBuffer: tracker.NewSummaryBuffer(
			filepath.Join(cfg.BaseDir, "config", ".cache", "summary-buffer.json"),
		),
		pokemonMatcher: &matching.PokemonMatcher{
			PVPQueryMaxRank:            cfg.PVP.PVPQueryMaxRank,
			PVPEvolutionDirectTracking: cfg.PVP.PVPEvolutionDirectTracking,
			StrictLocations:            cfg.Area.StrictLocations,
			AreaSecurityEnabled:        cfg.Area.Enabled,
			IncludeMegaEvolution:       cfg.PVP.IncludeMegaEvolution,
		},
		raidMatcher:      &matching.RaidMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: cfg.Area.Enabled},
		invasionMatcher:  &matching.InvasionMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		questMatcher:     &matching.QuestMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		lureMatcher:      &matching.LureMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		gymMatcher:       &matching.GymMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		nestMatcher:      &matching.NestMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		fortMatcher:      &matching.FortMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		maxbattleMatcher: &matching.MaxbattleMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		pvpCfg:           pvpCfg,
		activePokemon:    activePokemon,
		recentActivity:   tracker.NewRecentActivity(),
		pokemonTypes:     pokemonTypes,
		rateLimiter:      rateLimiter,
		validator: validation.New(
			cfg.Validation.URL,
			cfg.Validation.FailMode,
			cfg.Tuning.ValidationTimeoutMs,
			cfg.Tuning.ValidationMaxConcurrent,
		),
		translations: enricher.Translations,
		workerPool:   make(chan struct{}, cfg.Tuning.WorkerPoolSize),
		muteStore:    mute.NewStore(),
	}
	ps.muteSweeper = mute.NewSweeper(ps.muteStore, time.Minute)
	ps.muteSweeper.Start(ps.ctx)

	// Button action registry + built-in handlers (#109). Allocated even
	// when snapshots are disabled — operators can keep button defs in
	// DTS and the registry stays consistent across enable/disable
	// transitions.
	ps.buttonActions = buttonactions.NewRegistry()
	buttonactions.RegisterBuiltins(ps.buttonActions)

	// Opt-in snapshot store (#108). When disabled, both fields stay nil and
	// every consumer takes the nil-store path (no write, no read, no button
	// components rendered).
	if cfg.Snapshots.Enabled {
		store, err := snapshots.Open(cfg.Snapshots.Path)
		if err != nil {
			// A failed snapshot open shouldn't take down the alerter — the
			// snapshot store is for buttons/inspection, not core delivery.
			// Log and continue with a nil store (i.e. behave as if disabled).
			log.Warnf("snapshots: open at %s failed: %v — continuing with snapshots disabled", cfg.Snapshots.Path, err)
		} else {
			ps.snapshotStore = store
			ps.snapshotSweeper = snapshots.NewSweeper(
				store,
				time.Duration(cfg.Snapshots.SweepIntervalMins)*time.Minute,
				time.Duration(cfg.Snapshots.MaxAgeDays)*24*time.Hour,
			)
			ps.snapshotSweeper.Start(ps.ctx)
			// Buttons require a snapshot at click time, so we only enable
			// component emission when the snapshot store is also up.
			// Renderer may be nil during early init (tests / DTS load
			// errors) — guard accordingly.
			if ps.dtsRenderer != nil {
				ps.dtsRenderer.SetButtonsEnabled(true)
				// visible_to=admin buttons are also hidden at render time
				// on DM destinations so non-admin recipients never see a
				// button they can't use.
				ps.dtsRenderer.SetAdminRecipientCheck(func(id string) bool {
					return slices.Contains(cfg.Discord.Admins, id)
				})
			}
			log.Infof("Snapshot store enabled at %s (max_age=%dd, sweep=%dm); buttons enabled",
				cfg.Snapshots.Path, cfg.Snapshots.MaxAgeDays, cfg.Snapshots.SweepIntervalMins)
		}
	}

	return ps
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
	// Stop summary scheduler BEFORE the dispatcher. The scheduler's tick
	// calls DispatchQuestSummary → dispatcher.DispatchBypass which sends
	// on the dispatcher's job channel; stopping the dispatcher first
	// closes that channel, so a tick that fires mid-shutdown would panic
	// on send-to-closed-channel. Close() blocks until the loop goroutine
	// exits, after which it's safe to tear down the dispatcher.
	if ps.summaryScheduler != nil {
		ps.summaryScheduler.Close()
		log.Info("Summary scheduler stopped")
	}
	if ps.dispatcher != nil {
		log.Info("Stopping delivery dispatcher...")
		ps.dispatcher.Stop()
		log.Info("Delivery dispatcher stopped")
	}
	// Snapshot store shutdown follows the dispatcher — after Stop returns,
	// no more sender goroutines are running, so no more snapshot writes can
	// happen. Stop the sweeper goroutine first (releases reads), then close
	// the pogreb handle.
	if ps.snapshotSweeper != nil {
		ps.snapshotSweeper.Stop()
		log.Info("Snapshot sweeper stopped")
	}
	if ps.snapshotStore != nil {
		if err := ps.snapshotStore.Close(); err != nil {
			log.Warnf("Failed to close snapshot store: %v", err)
		} else {
			log.Info("Snapshot store closed")
		}
	}
	if ps.muteSweeper != nil {
		ps.muteSweeper.Stop()
		log.Info("Mute sweeper stopped")
	}
	if ps.enricher.StaticMap != nil {
		ps.enricher.StaticMap.Close()
	}
	ps.duplicates.Close()
	// Weather eviction runs on its own ticker and touches the same maps the
	// enrichment path reads, so it stops here with the other trackers, once
	// the webhook and render workers are already quiescent.
	ps.weather.Close()
	ps.rateLimiter.Close()
	// Persist gym state cache for restart
	if err := ps.gymState.Save(); err != nil {
		log.Warnf("Failed to save gym state cache: %v", err)
	} else {
		log.Info("Gym state cache saved")
	}
	// Persist summary buffer for restart so users don't lose buffered
	// quests across a graceful shutdown.
	if ps.summaryBuffer != nil {
		if err := ps.summaryBuffer.Save(); err != nil {
			log.Warnf("Failed to save summary buffer: %v", err)
		} else {
			log.Info("Summary buffer saved")
		}
	}
	if ps.enricher.Geocoder != nil {
		ps.enricher.Geocoder.Close()
	}
}

// Ensure ProcessorService implements webhook.Processor
var _ webhook.Processor = (*ProcessorService)(nil)

// discordBotSlashDispatcher returns the slash.Dispatcher from the running
// Discord bot, or nil when the bot is not up or slash commands are disabled.
// Used by the BotDeps slash lifecycle closures.
func discordBotSlashDispatcher(b *discordbot.Bot) *slash.Dispatcher {
	if b == nil {
		return nil
	}
	return b.SlashDispatcher()
}
