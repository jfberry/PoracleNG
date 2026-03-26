package state

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Load reloads tracking data from the database while preserving the existing
// geofence data. Use LoadWithGeofences for a full reload including geofences.
func Load(manager *Manager, database *sqlx.DB) error {
	data, err := db.LoadAll(database)
	if err != nil {
		return fmt.Errorf("load database: %w", err)
	}

	// Reuse existing geofence data from current state
	prev := manager.Get()
	var spatial *geofence.SpatialIndex
	var fences []geofence.Fence
	if prev != nil {
		spatial = prev.Geofence
		fences = prev.Fences
	}

	s := &State{
		Humans:    data.Humans,
		Monsters:  data.Monsters,
		Raids:     data.Raids,
		Eggs:      data.Eggs,
		Profiles:  data.Profiles,
		Invasions: data.Invasions,
		Quests:    data.Quests,
		Lures:     data.Lures,
		Gyms:      data.Gyms,
		Nests:     data.Nests,
		Forts:      data.Forts,
		Maxbattles: data.Maxbattles,
		Geofence:   spatial,
		Fences:    fences,
	}

	manager.Set(s)
	recordStateMetrics(s)

	log.Infof("State loaded: %d humans, %d pokemon, %d raids, %d eggs, %d invasions, %d quests, %d lures, %d gyms, %d nests, %d forts, %d maxbattles, %d fences",
		len(data.Humans), data.Monsters.Total, len(data.Raids), len(data.Eggs),
		len(data.Invasions), len(data.Quests), len(data.Lures),
		len(data.Gyms), len(data.Nests), len(data.Forts), len(data.Maxbattles), len(fences))

	return nil
}

// LoadWithGeofences reloads everything: tracking data from the database and
// geofence files from disk/Koji. Called on startup and explicit geofence reload.
func LoadWithGeofences(manager *Manager, database *sqlx.DB, geofenceCfg config.GeofenceConfig) error {
	// Fetch Koji geofences (downloads HTTP URLs to cache, falls back to cached on failure)
	if err := geofence.FetchKojiGeofences(geofenceCfg.Paths, geofenceCfg.Koji.BearerToken, geofenceCfg.Koji.CacheDir); err != nil {
		log.Warnf("Koji geofence fetch had errors: %s", err)
	}

	data, err := db.LoadAll(database)
	if err != nil {
		return fmt.Errorf("load database: %w", err)
	}

	spatial, fences, err := geofence.LoadAllGeofences(geofenceCfg.Paths, geofenceCfg.Koji.CacheDir)
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
		Humans:    data.Humans,
		Monsters:  data.Monsters,
		Raids:     data.Raids,
		Eggs:      data.Eggs,
		Profiles:  data.Profiles,
		Invasions: data.Invasions,
		Quests:    data.Quests,
		Lures:     data.Lures,
		Gyms:      data.Gyms,
		Nests:     data.Nests,
		Forts:      data.Forts,
		Maxbattles: data.Maxbattles,
		Geofence:   spatial,
		Fences:    fences,
	}

	manager.Set(s)
	recordStateMetrics(s)

	log.Infof("State loaded: %d humans, %d pokemon, %d raids, %d eggs, %d invasions, %d quests, %d lures, %d gyms, %d nests, %d forts, %d maxbattles, %d fences",
		len(data.Humans), data.Monsters.Total, len(data.Raids), len(data.Eggs),
		len(data.Invasions), len(data.Quests), len(data.Lures),
		len(data.Gyms), len(data.Nests), len(data.Forts), len(data.Maxbattles), len(fences))

	return nil
}

func recordStateMetrics(s *State) {
	metrics.StateHumans.Set(float64(len(s.Humans)))
	if s.Monsters != nil {
		metrics.StateTrackingRules.WithLabelValues("pokemon").Set(float64(s.Monsters.Total))
	}
	metrics.StateTrackingRules.WithLabelValues("raid").Set(float64(len(s.Raids)))
	metrics.StateTrackingRules.WithLabelValues("egg").Set(float64(len(s.Eggs)))
	metrics.StateTrackingRules.WithLabelValues("invasion").Set(float64(len(s.Invasions)))
	metrics.StateTrackingRules.WithLabelValues("quest").Set(float64(len(s.Quests)))
	metrics.StateTrackingRules.WithLabelValues("lure").Set(float64(len(s.Lures)))
	metrics.StateTrackingRules.WithLabelValues("gym").Set(float64(len(s.Gyms)))
	metrics.StateTrackingRules.WithLabelValues("nest").Set(float64(len(s.Nests)))
	metrics.StateTrackingRules.WithLabelValues("fort").Set(float64(len(s.Forts)))
	metrics.StateTrackingRules.WithLabelValues("maxbattle").Set(float64(len(s.Maxbattles)))
	metrics.StateGeofences.Set(float64(len(s.Fences)))
}
