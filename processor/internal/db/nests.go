package db

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// NestTracking represents a row from the nests table.
type NestTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Distance              int      `db:"distance"`
	Template              string   `db:"template"`
	PokemonID             int      `db:"pokemon_id"`
	MinSpawnAvg           int      `db:"min_spawn_avg"`
	Form                  int      `db:"form"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadNests loads all nest trackings from the database.
func LoadNests(db *sqlx.DB) ([]*NestTracking, error) {
	var nests []NestTracking
	err := db.Select(&nests,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id,
		        min_spawn_avg, COALESCE(form, 0) AS form,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM nests`)
	if err != nil {
		return nil, err
	}

	result := make([]*NestTracking, len(nests))
	for i := range nests {
		if nests[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(nests[i].OverrideAreasRaw), &nests[i].OverrideAreas)
		}
		result[i] = &nests[i]
	}
	return result, nil
}
