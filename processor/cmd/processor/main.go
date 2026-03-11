package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
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

	// Weather change consumer
	if cfg.Weather.EnableChangeAlert {
		go proc.consumeWeatherChanges()
		log.Infof("Weather change alerts enabled")
	}

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
			log.Debugf("Periodic reload triggered")
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
	cfg             *config.Config
	stateMgr        *state.Manager
	database        *sqlx.DB
	sender          *webhook.Sender
	weather         *tracker.WeatherTracker
	weatherCares    *tracker.WeatherCareTracker
	encounters      *tracker.EncounterTracker
	duplicates      *tracker.DuplicateCache
	rarity          *tracker.RarityTracker
	gymState        *tracker.GymStateTracker
	pokemonMatcher  *matching.PokemonMatcher
	raidMatcher     *matching.RaidMatcher
	invasionMatcher *matching.InvasionMatcher
	questMatcher    *matching.QuestMatcher
	lureMatcher     *matching.LureMatcher
	gymMatcher      *matching.GymMatcher
	nestMatcher     *matching.NestMatcher
	fortMatcher     *matching.FortMatcher
	pvpCfg          *pvp.Config
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

	return &ProcessorService{
		cfg:          cfg,
		stateMgr:     stateMgr,
		database:     database,
		sender:       webhook.NewSender(cfg.Alerter.URL, cfg.Tuning.BatchSize, cfg.Tuning.FlushIntervalMillis),
		weather:      tracker.NewWeatherTracker(),
		weatherCares: tracker.NewWeatherCareTracker(),
		encounters:   tracker.NewEncounterTracker(),
		duplicates:   tracker.NewDuplicateCache(),
		rarity:       tracker.NewRarityTracker(24 * time.Hour),
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
		fortMatcher:     &matching.FortMatcher{StrictLocations: cfg.Area.StrictLocations, AreaSecurityEnabled: strictAreas},
		pvpCfg:          pvpCfg,
		workerPool:      make(chan struct{}, cfg.Tuning.WorkerPoolSize),
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

		l := log.WithField("ref", pokemon.EncounterID)

		// Record for rarity tracking
		ps.rarity.RecordSighting(pokemon.PokemonID)

		// Duplicate check
		verified := pokemon.Verified || pokemon.DisappearTimeVerified
		if ps.duplicates.CheckPokemon(pokemon.EncounterID, verified, pokemon.CP, pokemon.DisappearTime) {
			l.Debug("Wild encounter was sent again too soon, ignoring")
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

			// Register matched users as caring about weather in this cell
			if ps.cfg.Weather.EnableChangeAlert {
				cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
				for _, u := range matched {
					ps.weatherCares.Register(cellID, tracker.WeatherCareEntry{
						ID:         u.ID,
						Name:       u.Name,
						Type:       u.Type,
						Language:   u.Language,
						Template:   u.Template,
						Clean:      u.Clean,
						Ping:       u.Ping,
						CaresUntil: pokemon.DisappearTime,
					})
				}
			}

			// Enrich message with computed fields
			enriched := enrichPokemonMessage(raw, processed, ps.weather, pokemon.Latitude, pokemon.Longitude)

			l.Infof("Pokemon %d appeared at [%.3f,%.3f] and %d humans cared",
				pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "pokemon",
				Message:      enriched,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Pokemon %d appeared at [%.3f,%.3f] and 0 humans cared",
				pokemon.PokemonID, pokemon.Latitude, pokemon.Longitude)
		}

		// Handle pokemon change
		if change != nil {
			ps.handlePokemonChange(l, raw, change, st)
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

		l := log.WithField("ref", raid.GymID)

		st := ps.stateMgr.Get()
		ex := bool(raid.ExRaidEligible) || bool(raid.IsExRaidEligible)

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

			l.Infof("%s level %d on %s appeared at [%.3f,%.3f] and %d humans cared",
				msgType, raid.Level, gymName, raid.Latitude, raid.Longitude, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         msgType,
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Raid/egg level %d appeared at [%.3f,%.3f] and 0 humans cared",
				raid.Level, raid.Latitude, raid.Longitude)
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

	ps.weather.UpdateFromWebhook(cellID, weather.GameplayCondition, weather.Updated, weather.Latitude, weather.Longitude)
	return nil
}

// consumeWeatherChanges reads weather change events and forwards them to the alerter
// with the list of users who care about that cell.
func (ps *ProcessorService) consumeWeatherChanges() {
	for change := range ps.weather.Changes() {
		l := log.WithField("ref", change.S2CellID)

		caringUsers := ps.weatherCares.GetCaringUsers(change.S2CellID)
		if len(caringUsers) == 0 {
			l.Debugf("Weather changed to %d (from %d, source=%s) but no users care",
				change.GameplayCondition, change.OldGameplayCondition, change.Source)
			continue
		}

		l.Infof("Weather changed to %d (from %d, source=%s) and %d users care",
			change.GameplayCondition, change.OldGameplayCondition, change.Source, len(caringUsers))

		// Build matched users
		matched := make([]webhook.MatchedUser, len(caringUsers))
		for i, u := range caringUsers {
			matched[i] = webhook.MatchedUser{
				ID:       u.ID,
				Name:     u.Name,
				Type:     u.Type,
				Language: u.Language,
				Template: u.Template,
				Clean:    u.Clean,
				Ping:     u.Ping,
			}
		}

		// Build matched areas from cell center
		st := ps.stateMgr.Get()
		areas := st.Geofence.PointInAreas(change.Latitude, change.Longitude)
		matchedAreas := make([]webhook.MatchedArea, len(areas))
		for i, a := range areas {
			matchedAreas[i] = webhook.MatchedArea{
				Name:             a.Name,
				DisplayInMatches: a.DisplayInMatches,
				Group:            a.Group,
			}
		}

		// Build weather change message
		msg, _ := json.Marshal(change)

		ps.sender.Send(webhook.OutboundPayload{
			Type:         "weather_change",
			Message:      msg,
			MatchedAreas: matchedAreas,
			MatchedUsers: matched,
		})
	}
}

func (ps *ProcessorService) handlePokemonChange(l *log.Entry, raw json.RawMessage, change *tracker.EncounterChange, st *state.State) {
	// Re-match with new state and send as pokemon_changed
	oldIV := float64(change.Old.ATK+change.Old.DEF+change.Old.STA) / 0.45

	l.Infof("Pokemon changed from %d to %d", change.Old.PokemonID, change.New.PokemonID)

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

func (ps *ProcessorService) ProcessInvasion(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var inv webhook.InvasionWebhook
		if err := json.Unmarshal(raw, &inv); err != nil {
			log.Errorf("Failed to parse invasion webhook: %s", err)
			return
		}

		l := log.WithField("ref", inv.PokestopID)

		// Resolve expiration
		expiration := inv.IncidentExpiration
		if expiration == 0 {
			expiration = inv.IncidentExpireTimestamp
		}

		// Duplicate check
		if expiration > 0 && ps.duplicates.CheckInvasion(inv.PokestopID, expiration) {
			l.Debug("Invasion duplicate, ignoring")
			return
		}

		// Resolve grunt type and display type
		displayType := inv.DisplayType
		if displayType == 0 {
			displayType = inv.IncidentDisplayType
		}
		gruntType := matching.ResolveGruntType(inv.IncidentGruntType, inv.GruntType, displayType)

		data := &matching.InvasionData{
			PokestopID: inv.PokestopID,
			GruntType:  gruntType,
			Gender:     inv.Gender,
			Latitude:   inv.Latitude,
			Longitude:  inv.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.invasionMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(inv.Latitude, inv.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Invasion grunt %s at %s and %d humans cared",
				gruntType, inv.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "invasion",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Invasion grunt %s at %s and 0 humans cared", gruntType, inv.Name)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessQuest(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var quest webhook.QuestWebhook
		if err := json.Unmarshal(raw, &quest); err != nil {
			log.Errorf("Failed to parse quest webhook: %s", err)
			return
		}

		l := log.WithField("ref", quest.PokestopID)

		// Build rewards key for dedup
		rewardsKey := buildQuestRewardsKey(quest.Rewards)
		if ps.duplicates.CheckQuest(quest.PokestopID, rewardsKey) {
			l.Debug("Quest duplicate, ignoring")
			return
		}

		// Parse rewards for matching
		rewards := make([]matching.QuestRewardData, 0, len(quest.Rewards))
		for _, r := range quest.Rewards {
			rewards = append(rewards, parseQuestReward(r))
		}

		data := &matching.QuestData{
			PokestopID: quest.PokestopID,
			Latitude:   quest.Latitude,
			Longitude:  quest.Longitude,
			Rewards:    rewards,
		}

		st := ps.stateMgr.Get()
		matched := ps.questMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(quest.Latitude, quest.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Quest at %s and %d humans cared", quest.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "quest",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Quest at %s and 0 humans cared", quest.Name)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessLure(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var lure webhook.LureWebhook
		if err := json.Unmarshal(raw, &lure); err != nil {
			log.Errorf("Failed to parse lure webhook: %s", err)
			return
		}

		l := log.WithField("ref", lure.PokestopID)

		// Duplicate check
		if lure.LureExpiration > 0 && ps.duplicates.CheckLure(lure.PokestopID, lure.LureExpiration) {
			l.Debug("Lure duplicate, ignoring")
			return
		}

		data := &matching.LureData{
			PokestopID: lure.PokestopID,
			LureID:     lure.LureID,
			Latitude:   lure.Latitude,
			Longitude:  lure.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.lureMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(lure.Latitude, lure.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Lure %d at %s and %d humans cared",
				lure.LureID, lure.Name, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "lure",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Lure %d at %s and 0 humans cared", lure.LureID, lure.Name)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessGym(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var gym webhook.GymWebhook
		if err := json.Unmarshal(raw, &gym); err != nil {
			log.Errorf("Failed to parse gym webhook: %s", err)
			return
		}

		// Resolve gym ID
		gymID := gym.GymID
		if gymID == "" {
			gymID = gym.ID
		}

		l := log.WithField("ref", gymID)

		// Resolve team ID
		teamID := gym.TeamID
		if teamID == 0 {
			teamID = gym.Team
		}

		// Resolve in-battle
		inBattle := bool(gym.IsInBattle) || bool(gym.InBattle)

		// Update gym state and get old state
		oldState := ps.gymState.Update(gymID, teamID, gym.SlotsAvailable, inBattle, gym.LastOwnerID)
		if oldState == nil {
			l.Debug("Gym first seen, no change detection yet")
			return
		}

		data := &matching.GymData{
			GymID:             gymID,
			TeamID:            teamID,
			OldTeamID:         oldState.TeamID,
			SlotsAvailable:    gym.SlotsAvailable,
			OldSlotsAvailable: oldState.SlotsAvailable,
			InBattle:          inBattle,
			OldInBattle:       oldState.InBattle,
			Latitude:          gym.Latitude,
			Longitude:         gym.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.gymMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(gym.Latitude, gym.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Gym %s changed (team %d->%d) and %d humans cared",
				gym.Name, oldState.TeamID, teamID, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "gym",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Gym %s changed and 0 humans cared", gym.Name)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessNest(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var nest webhook.NestWebhook
		if err := json.Unmarshal(raw, &nest); err != nil {
			log.Errorf("Failed to parse nest webhook: %s", err)
			return
		}

		l := log.WithField("ref", nest.NestID)

		// Duplicate check
		if ps.duplicates.CheckNest(nest.NestID, nest.PokemonID, nest.ResetTime) {
			l.Debug("Nest duplicate, ignoring")
			return
		}

		data := &matching.NestData{
			NestID:     nest.NestID,
			PokemonID:  nest.PokemonID,
			Form:       nest.Form,
			PokemonAvg: nest.PokemonAvg,
			Latitude:   nest.Latitude,
			Longitude:  nest.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.nestMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(nest.Latitude, nest.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Nest pokemon %d (avg %.1f) and %d humans cared",
				nest.PokemonID, nest.PokemonAvg, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "nest",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Nest pokemon %d (avg %.1f) and 0 humans cared",
				nest.PokemonID, nest.PokemonAvg)
		}
	}()
	return nil
}

func (ps *ProcessorService) ProcessFortUpdate(raw json.RawMessage) error {
	ps.workerPool <- struct{}{}
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		defer func() { <-ps.workerPool }()

		var fort webhook.FortWebhook
		if err := json.Unmarshal(raw, &fort); err != nil {
			log.Errorf("Failed to parse fort_update webhook: %s", err)
			return
		}

		l := log.WithField("ref", fort.ID)

		data := &matching.FortData{
			ID:          fort.ID,
			FortType:    fort.FortType,
			IsEmpty:     fort.IsEmpty,
			ChangeTypes: fort.ChangeTypes,
			Latitude:    fort.Latitude,
			Longitude:   fort.Longitude,
		}

		st := ps.stateMgr.Get()
		matched := ps.fortMatcher.Match(data, st)

		if len(matched) > 0 {
			areas := st.Geofence.PointInAreas(fort.Latitude, fort.Longitude)
			matchedAreas := buildMatchedAreas(areas)

			l.Infof("Fort update %s (%s) and %d humans cared",
				fort.Name, fort.FortType, len(matched))

			ps.sender.Send(webhook.OutboundPayload{
				Type:         "fort_update",
				Message:      raw,
				MatchedAreas: matchedAreas,
				MatchedUsers: matched,
			})
		} else {
			l.Debugf("Fort update %s (%s) and 0 humans cared", fort.Name, fort.FortType)
		}
	}()
	return nil
}

// buildMatchedAreas converts geofence areas to webhook MatchedArea structs.
func buildMatchedAreas(areas []geofence.MatchedArea) []webhook.MatchedArea {
	result := make([]webhook.MatchedArea, len(areas))
	for i, a := range areas {
		result[i] = webhook.MatchedArea{
			Name:             a.Name,
			DisplayInMatches: a.DisplayInMatches,
			Group:            a.Group,
		}
	}
	return result
}

// buildQuestRewardsKey creates a dedup key from quest rewards.
func buildQuestRewardsKey(rewards []webhook.QuestReward) string {
	key := ""
	for _, r := range rewards {
		key += fmt.Sprintf("%d:", r.Type)
		if info, ok := r.Info["pokemon_id"]; ok {
			key += fmt.Sprintf("p%v", info)
		}
		if info, ok := r.Info["item_id"]; ok {
			key += fmt.Sprintf("i%v", info)
		}
		if info, ok := r.Info["amount"]; ok {
			key += fmt.Sprintf("a%v", info)
		}
		key += ";"
	}
	return key
}

// parseQuestReward converts a webhook QuestReward to a matching QuestRewardData.
func parseQuestReward(r webhook.QuestReward) matching.QuestRewardData {
	result := matching.QuestRewardData{
		Type: r.Type,
	}

	if v, ok := r.Info["pokemon_id"]; ok {
		result.PokemonID = toInt(v)
	}
	if v, ok := r.Info["item_id"]; ok {
		result.ItemID = toInt(v)
	}
	if v, ok := r.Info["amount"]; ok {
		result.Amount = toInt(v)
	}
	if v, ok := r.Info["form_id"]; ok {
		result.FormID = toInt(v)
	}
	if v, ok := r.Info["shiny"]; ok {
		if b, ok2 := v.(bool); ok2 {
			result.Shiny = b
		}
	}

	return result
}

// toInt converts a JSON number (float64) to int.
func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	}
	return 0
}

// Ensure ProcessorService implements webhook.Processor
var _ webhook.Processor = (*ProcessorService)(nil)
