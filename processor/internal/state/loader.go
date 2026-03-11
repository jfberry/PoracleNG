package state

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// Load loads all data from the database and geofence files, builds a new State,
// and atomically swaps it in.
func Load(manager *Manager, database *sqlx.DB, geofencePaths []string) error {
	data, err := db.LoadAll(database)
	if err != nil {
		return fmt.Errorf("load database: %w", err)
	}

	spatial, fences, err := geofence.LoadAllGeofences(geofencePaths)
	if err != nil {
		return fmt.Errorf("load geofences: %w", err)
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
		Forts:     data.Forts,
		Geofence:  spatial,
		Fences:    fences,
	}

	manager.Set(s)

	log.Infof("State loaded: %d humans, %d raids, %d eggs, %d invasions, %d quests, %d lures, %d gyms, %d nests, %d forts, %d fences",
		len(data.Humans), len(data.Raids), len(data.Eggs),
		len(data.Invasions), len(data.Quests), len(data.Lures),
		len(data.Gyms), len(data.Nests), len(data.Forts), len(fences))

	return nil
}
