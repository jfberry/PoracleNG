package state

import (
	"fmt"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// summaryScheduleAlertTypes are the alert types we currently support
// summaries for. Extend this list when new buffered-delivery types come
// online; the loader iterates it once per reload.
var summaryScheduleAlertTypes = []string{"quest"}

// countSummaryScheduleEntries returns the total (humanID, alertType)
// pair count across the nested map, not just the outer humanID count.
// Used by the state log line so operators see entries, not users.
func countSummaryScheduleEntries(schedules map[string]map[string][]db.ActiveHourEntry) int {
	n := 0
	for _, byType := range schedules {
		n += len(byType)
	}
	return n
}

// loadSummarySchedules reads schedules for every supported alert type from
// the given store and returns the nested id -> alertType -> entries map. A
// nil store is tolerated (returns an empty map) so test paths and call
// sites that haven't been wired yet keep compiling.
//
// Errors from the underlying store are logged and the affected alert type
// is skipped — a transient DB hiccup on this auxiliary table must not
// abort the entire state reload (which would also kill the alert-side
// tracking refresh). Other secondary loaders in this package (humans,
// profiles) follow the same log-and-continue convention.
func loadSummarySchedules(scheduleStore store.SummaryScheduleStore) map[string]map[string][]db.ActiveHourEntry {
	out := map[string]map[string][]db.ActiveHourEntry{}
	if scheduleStore == nil {
		return out
	}
	for _, alertType := range summaryScheduleAlertTypes {
		schedules, err := scheduleStore.ListByType(alertType)
		if err != nil {
			log.Warnf("state: list summary_schedules type=%s: %s — skipping (the scheduler will see an empty map for this type until the next successful reload)", alertType, err)
			continue
		}
		for _, s := range schedules {
			if out[s.ID] == nil {
				out[s.ID] = map[string][]db.ActiveHourEntry{}
			}
			// Preserve the entry even when ParsedActiveHours is nil so the
			// scheduler can no-op on empty schedules without re-querying.
			out[s.ID][s.AlertType] = s.ParsedActiveHours
		}
	}
	return out
}

// Load reloads tracking data from the database while preserving the existing
// geofence data. Use LoadWithGeofences for a full reload including geofences.
func Load(manager *Manager, database *sqlx.DB, scheduleStore store.SummaryScheduleStore) error {
	dbStart := time.Now()
	data, err := db.LoadAll(database)
	metrics.StateDBQueryDuration.Observe(time.Since(dbStart).Seconds())
	if err != nil {
		return fmt.Errorf("load database: %w", err)
	}

	locs, err := db.LoadUserLocations(database)
	if err != nil {
		return fmt.Errorf("load user locations: %w", err)
	}
	for id, h := range data.Humans {
		if m, ok := locs[id]; ok {
			h.Locations = m
		}
	}

	schedules := loadSummarySchedules(scheduleStore)

	// Reuse existing geofence data from current state
	prev := manager.Get()
	var spatial *geofence.SpatialIndex
	var fences []geofence.Fence
	if prev != nil {
		spatial = prev.Geofence
		fences = prev.Fences
	}

	s := &State{
		Humans:           data.Humans,
		Monsters:         data.Monsters,
		Raids:            data.Raids,
		Eggs:             data.Eggs,
		Profiles:         data.Profiles,
		Invasions:        data.Invasions,
		Quests:           data.Quests,
		Lures:            data.Lures,
		Gyms:             data.Gyms,
		Nests:            data.Nests,
		Forts:            data.Forts,
		Maxbattles:       data.Maxbattles,
		Geofence:         spatial,
		Fences:           fences,
		SummarySchedules: schedules,
	}

	manager.Set(s)
	recordStateMetrics(s)
	metrics.StateLastReloadSuccess.SetToCurrentTime()

	log.Infof("State loaded: %d humans, %d pokemon, %d raids, %d eggs, %d invasions, %d quests, %d lures, %d gyms, %d nests, %d forts, %d maxbattles, %d fences, %d summary schedules",
		len(data.Humans), data.Monsters.Total, len(data.Raids), len(data.Eggs),
		len(data.Invasions), len(data.Quests), len(data.Lures),
		len(data.Gyms), len(data.Nests), len(data.Forts), len(data.Maxbattles), len(fences), countSummaryScheduleEntries(schedules))
	log.Infof("State buckets: %s", summarizeMonsterBuckets(data.Monsters))

	return nil
}

// LoadWithGeofences reloads everything: tracking data from the database and
// geofence files from disk/Koji. Called on startup and explicit geofence reload.
func LoadWithGeofences(manager *Manager, database *sqlx.DB, scheduleStore store.SummaryScheduleStore, geofenceCfg config.GeofenceConfig) error {
	// Fetch Koji geofences (downloads HTTP URLs to cache, falls back to cached on failure)
	if err := geofence.FetchKojiGeofences(geofenceCfg.Paths, geofenceCfg.Koji.BearerToken, geofenceCfg.Koji.CacheDir); err != nil {
		log.Warnf("Koji geofence fetch had errors: %s", err)
	}

	dbStart := time.Now()
	data, err := db.LoadAll(database)
	metrics.StateDBQueryDuration.Observe(time.Since(dbStart).Seconds())
	if err != nil {
		return fmt.Errorf("load database: %w", err)
	}

	locs, err := db.LoadUserLocations(database)
	if err != nil {
		return fmt.Errorf("load user locations: %w", err)
	}
	for id, h := range data.Humans {
		if m, ok := locs[id]; ok {
			h.Locations = m
		}
	}

	schedules := loadSummarySchedules(scheduleStore)

	spatial, fences, err := geofence.LoadAllGeofences(geofenceCfg.Paths, geofenceCfg.Koji.CacheDir, geofenceCfg.DefaultName)
	if err != nil {
		return fmt.Errorf("load geofences: %w", err)
	}

	for _, f := range fences {
		if len(f.Multipath) > 0 {
			parts := make([]int, len(f.Multipath))
			for i, p := range f.Multipath {
				parts[i] = len(p)
			}
			log.Infof("Geofence: %s (multi-polygon, %d parts: %v points)", f.Name, len(f.Multipath), parts)
		} else {
			log.Infof("Geofence: %s (polygon, %d points)", f.Name, len(f.Path))
		}
	}

	s := &State{
		Humans:           data.Humans,
		Monsters:         data.Monsters,
		Raids:            data.Raids,
		Eggs:             data.Eggs,
		Profiles:         data.Profiles,
		Invasions:        data.Invasions,
		Quests:           data.Quests,
		Lures:            data.Lures,
		Gyms:             data.Gyms,
		Nests:            data.Nests,
		Forts:            data.Forts,
		Maxbattles:       data.Maxbattles,
		Geofence:         spatial,
		Fences:           fences,
		SummarySchedules: schedules,
	}

	manager.Set(s)
	recordStateMetrics(s)
	metrics.StateLastReloadSuccess.SetToCurrentTime()

	log.Infof("State loaded: %d humans, %d pokemon, %d raids, %d eggs, %d invasions, %d quests, %d lures, %d gyms, %d nests, %d forts, %d maxbattles, %d fences, %d summary schedules",
		len(data.Humans), data.Monsters.Total, len(data.Raids), len(data.Eggs),
		len(data.Invasions), len(data.Quests), len(data.Lures),
		len(data.Gyms), len(data.Nests), len(data.Forts), len(data.Maxbattles), len(fences), countSummaryScheduleEntries(schedules))
	log.Infof("State buckets: %s", summarizeMonsterBuckets(data.Monsters))

	return nil
}

func recordStateMetrics(s *State) {
	metrics.StateHumans.Set(float64(len(s.Humans)))
	if s.Monsters != nil {
		metrics.StateTrackingRules.WithLabelValues(metrics.TypePokemon).Set(float64(s.Monsters.Total))
		metrics.StateMonsterEverythingBucket.Set(float64(len(s.Monsters.ByPokemonID[0])))
		for league, slice := range s.Monsters.PVPSpecific {
			metrics.StateMonsterPVPSpecific.WithLabelValues(strconv.Itoa(league)).Set(float64(len(slice)))
		}
		for league, slice := range s.Monsters.PVPEverything {
			metrics.StateMonsterPVPEverything.WithLabelValues(strconv.Itoa(league)).Set(float64(len(slice)))
		}
	}
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeRaid).Set(float64(len(s.Raids)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeEgg).Set(float64(len(s.Eggs)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeInvasion).Set(float64(len(s.Invasions)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeQuest).Set(float64(len(s.Quests)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeLure).Set(float64(len(s.Lures)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeGym).Set(float64(len(s.Gyms)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeNest).Set(float64(len(s.Nests)))
	metrics.StateTrackingRules.WithLabelValues("fort").Set(float64(len(s.Forts)))
	metrics.StateTrackingRules.WithLabelValues(metrics.TypeMaxbattle).Set(float64(len(s.Maxbattles)))
	metrics.StateGeofences.Set(float64(len(s.Fences)))
}
