package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/logging"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
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

	database, err := db.OpenDB(cfg.Database.DSN)
	if err != nil {
		log.Fatalf("Failed to open database: %s", err)
	}
	defer database.Close()

	stateMgr := state.NewManager()

	// Initial load
	if err := state.Load(stateMgr, database, cfg.Geofence.Paths); err != nil {
		log.Fatalf("Failed to load initial state: %s", err)
	}

	// Create processor
	proc := NewProcessorService(cfg, stateMgr, database)

	// HTTP server
	mux := http.NewServeMux()

	// Webhook receiver
	webhookHandler := webhook.NewHandler(proc)
	mux.Handle("/", webhookHandler)

	// API endpoints
	apiHandler := api.NewHandler(func() error {
		return state.Load(stateMgr, database, cfg.Geofence.Paths)
	})
	apiHandler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: mux,
	}

	// Periodic reload
	go func() {
		interval := time.Duration(cfg.Tuning.ReloadIntervalSecs) * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := state.Load(stateMgr, database, cfg.Geofence.Paths); err != nil {
				log.Errorf("Periodic reload failed: %s", err)
			}
		}
	}()

	// Start server
	go func() {
		log.Infof("Processor starting on %s", cfg.Server.ListenAddr)
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
	cfg            *config.Config
	stateMgr       *state.Manager
	database       *sqlx.DB
	sender         *webhook.Sender
	weather        *tracker.WeatherTracker
	encounters     *tracker.EncounterTracker
	duplicates     *tracker.DuplicateCache
	rarity         *tracker.RarityTracker
	pokemonMatcher *matching.PokemonMatcher
	raidMatcher    *matching.RaidMatcher
	pvpCfg         *pvp.Config
	workerPool     chan struct{}
	wg             sync.WaitGroup
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

	return &ProcessorService{
		cfg:        cfg,
		stateMgr:   stateMgr,
		database:   database,
		sender:     webhook.NewSender(cfg.Alerter.URL, cfg.Tuning.BatchSize, cfg.Tuning.FlushIntervalMillis),
		weather:    tracker.NewWeatherTracker(),
		encounters: tracker.NewEncounterTracker(),
		duplicates: tracker.NewDuplicateCache(),
		rarity:     tracker.NewRarityTracker(24 * time.Hour),
		pokemonMatcher: &matching.PokemonMatcher{
			PVPQueryMaxRank:            cfg.PVP.PVPQueryMaxRank,
			PVPEvolutionDirectTracking: cfg.PVP.PVPEvolutionDirectTracking,
			StrictLocations:            cfg.Area.StrictLocations,
			AreaSecurityEnabled:        cfg.Area.Enabled,
		},
		raidMatcher: &matching.RaidMatcher{
			StrictLocations:     cfg.Area.StrictLocations,
			AreaSecurityEnabled: cfg.Area.Enabled,
		},
		pvpCfg:     pvpCfg,
		workerPool: make(chan struct{}, cfg.Tuning.WorkerPoolSize),
	}
}

func (ps *ProcessorService) Close() {
	ps.wg.Wait()
	ps.sender.Close()
	ps.duplicates.Close()
}

func (ps *ProcessorService) ProcessPokemon(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var pokemon webhook.PokemonWebhook
		if err := json.Unmarshal(raw, &pokemon); err != nil {
			log.Errorf("Failed to parse pokemon webhook: %s", err)
			return
		}

		// Record for rarity tracking
		ps.rarity.RecordSighting(pokemon.PokemonID)

		// Duplicate check
		verified := pokemon.Verified || pokemon.DisappearTimeVerified
		if ps.duplicates.CheckPokemon(pokemon.EncounterID, verified, pokemon.CP, pokemon.DisappearTime) {
			log.Debugf("%s: Wild encounter was sent again too soon, ignoring", pokemon.EncounterID)
			return
		}

		// Weather inference
		if pokemon.Weather > 0 && ps.cfg.Weather.EnableInference {
			cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
			ps.weather.CheckWeatherOnMonster(cellID, pokemon.Latitude, pokemon.Longitude, pokemon.Weather)
		}

		// Encounter tracking (change detection)
		atk, def, sta := 0, 0, 0
		if pokemon.IndividualAttack != nil {
			atk = *pokemon.IndividualAttack
		}
		if pokemon.IndividualDefense != nil {
			def = *pokemon.IndividualDefense
		}
		if pokemon.IndividualStamina != nil {
			sta = *pokemon.IndividualStamina
		}
		weather := pokemon.Weather
		if pokemon.BoostedWeather > 0 {
			weather = pokemon.BoostedWeather
		}
		encounterState := tracker.EncounterState{
			PokemonID:     pokemon.PokemonID,
			Form:          pokemon.Form,
			Weather:       weather,
			CP:            pokemon.CP,
			ATK:           atk,
			DEF:           def,
			STA:           sta,
			DisappearTime: pokemon.DisappearTime,
		}
		_, change := ps.encounters.Track(pokemon.EncounterID, encounterState)

		// Get rarity group
		rarityGroup := ps.rarity.GetRarityGroup(pokemon.PokemonID)

		// Process pokemon into matching format
		processed := matching.ProcessPokemonWebhook(&pokemon, rarityGroup, ps.pvpCfg)

		// Match
		st := ps.stateMgr.Get()
		matched := ps.pokemonMatcher.Match(processed, st)

		if len(matched) > 0 {
			// Get matched areas for the alerter
			areas := st.Geofence.PointInAreas(pokemon.Latitude, pokemon.Longitude)
			matchedAreas := make([]webhook.MatchedArea, len(areas))
			for i, a := range areas {
				matchedAreas[i] = webhook.MatchedArea{
					Name:             a.Name,
					DisplayInMatches: a.DisplayInMatches,
					Group:            a.Group,
				}
			}

			// Enrich message with computed fields
			enriched := enrichPokemonMessage(raw, processed, ps.weather, pokemon.Latitude, pokemon.Longitude)

			log.Infof("%s: Pokemon %d appeared at [%.3f,%.3f] and %d humans cared",
				pokemon.EncounterID, pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "pokemon",
				Message:      enriched,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			log.Debugf("%s: Pokemon %d appeared at [%.3f,%.3f] and 0 humans cared",
				pokemon.EncounterID, pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude)
		}

		// Handle pokemon change
		if change != nil {
			ps.handlePokemonChange(raw, change, st)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessRaid(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var raid webhook.RaidWebhook
		if err := json.Unmarshal(raw, &raid); err != nil {
			log.Errorf("Failed to parse raid webhook: %s", err)
			return
		}

		st := ps.stateMgr.Get()
		ex := raid.ExRaidEligible || raid.IsExRaidEligible

		var matched []webhook.MatchedUser

		if raid.PokemonID > 0 {
			// Raid with boss
			raidData := &matching.RaidData{
				GymID:     raid.GymID,
				PokemonID: raid.PokemonID,
				Form:      raid.Form,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Evolution: raid.Evolution,
				Move1:     raid.Move1,
				Move2:     raid.Move2,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched = ps.raidMatcher.MatchRaid(raidData, st)
		} else {
			// Egg
			eggData := &matching.EggData{
				GymID:     raid.GymID,
				Level:     raid.Level,
				TeamID:    raid.TeamID,
				Ex:        ex,
				Latitude:  raid.Latitude,
				Longitude: raid.Longitude,
			}
			matched = ps.raidMatcher.MatchEgg(eggData, st)
		}

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(raid.Latitude, raid.Longitude)
			matchedAreas := make([]webhook.MatchedArea, len(areas))
			for i, a := range areas {
				matchedAreas[i] = webhook.MatchedArea{
					Name:             a.Name,
					DisplayInMatches: a.DisplayInMatches,
					Group:            a.Group,
				}
			}

			msgType := "raid"
			if raid.PokemonID == 0 {
				msgType = "egg"
			}

			gymName := raid.GymName
			if gymName == "" {
				gymName = raid.Name
			}

			log.Infof("%s: %s level %d on %s appeared at [%.3f,%.3f] and %d humans cared",
				raid.GymID, msgType, raid.Level, gymName, raid.Latitude, raid.Longitude, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         msgType,
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			log.Debugf("%s: Raid/egg level %d appeared at [%.3f,%.3f] and 0 humans cared",
				raid.GymID, raid.Level, raid.Latitude, raid.Longitude)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessWeather(raw json.RawMessage) error {
	var weather webhook.WeatherWebhook
	if err := json.Unmarshal(raw, &weather); err != nil {
		log.Errorf("Failed to parse weather webhook: %s", err)
		return err
	}

	cellID := weather.S2CellID
	if cellID == "" {
		cellID = tracker.GetWeatherCellID(weather.Latitude, weather.Longitude)
	}

	ps.weather.UpdateFromWebhook(cellID, weather.GameplayCondition, weather.Updated)
	return nil
}

func (ps *ProcessorService) handlePokemonChange(raw json.RawMessage, change *tracker.EncounterChange, st *state.State) {
	// Re-match with new state and send as pokemon_changed
	oldIV := float64(change.Old.ATK+change.Old.DEF+change.Old.STA) / 0.45

	log.Infof("%s: Pokemon changed from %d to %d", change.EncounterID, change.Old.PokemonID, change.New.PokemonID)

	ps.sender.Send(webhook.OutboundPayload{
		Type:    "pokemon_changed",
		Message: raw,
		OldState: &webhook.EncounterOld{
			PokemonID: change.Old.PokemonID,
			Form:      change.Old.Form,
			Weather:   change.Old.Weather,
			CP:        change.Old.CP,
			IV:        oldIV,
		},
	})
}

func enrichPokemonMessage(raw json.RawMessage, processed *matching.ProcessedPokemon, weather *tracker.WeatherTracker, lat, lon float64) json.RawMessage {
	// Parse the original message and add computed fields
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw
	}

	msg["iv"] = processed.IV
	msg["atk"] = processed.ATK
	msg["def"] = processed.DEF
	msg["sta"] = processed.STA
	msg["cp"] = processed.CP
	msg["level"] = processed.Level
	msg["tthSeconds"] = processed.TTHSeconds
	msg["encountered"] = processed.Encountered
	msg["rarityGroup"] = processed.RarityGroup
	msg["pvpBestRank"] = processed.PVPBestRank
	msg["pvpEvolutionData"] = processed.PVPEvoData

	// Add cell weather
	cellID := tracker.GetWeatherCellID(lat, lon)
	cellWeather := weather.GetCurrentWeatherInCell(cellID)
	msg["gameWeatherId"] = cellWeather

	enriched, err := json.Marshal(msg)
	if err != nil {
		return raw
	}
	return enriched
}

// Ensure ProcessorService implements webhook.Processor
var _ webhook.Processor = (*ProcessorService)(nil)
