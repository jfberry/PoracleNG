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
		Humans:   data.Humans,
		Monsters: data.Monsters,
		Raids:    data.Raids,
		Eggs:     data.Eggs,
		Profiles: data.Profiles,
		Geofence: spatial,
		Fences:   fences,
	}

	manager.Set(s)

	log.Infof("State loaded: %d humans, %d raids, %d eggs, %d fences",
		len(data.Humans), len(data.Raids), len(data.Eggs), len(fences))

	return nil
}
