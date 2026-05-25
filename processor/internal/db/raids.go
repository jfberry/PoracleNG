package db

import (
	"database/sql"
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// RaidTracking represents a row from the raid table.
type RaidTracking struct {
	UID                   int64          `db:"uid"`
	ID                    string         `db:"id"`
	ProfileNo             int            `db:"profile_no"`
	PokemonID             int            `db:"pokemon_id"`
	Level                 int            `db:"level"`
	Team                  int            `db:"team"`
	Exclusive             bool           `db:"exclusive"`
	Form                  int            `db:"form"`
	Evolution             int            `db:"evolution"`
	Move                  int            `db:"move"`
	GymID                 sql.NullString `db:"gym_id"`
	Distance              int            `db:"distance"`
	Template              string         `db:"template"`
	Clean                 int            `db:"clean"`
	Ping                  string         `db:"ping"`
	RSVPChanges           int            `db:"rsvp_changes"`
	OverrideLocationLabel string         `db:"override_location_label"`
	OverrideAreasRaw      string         `db:"override_areas"`
	OverrideAreas         []string       `db:"-"`
}

// EggTracking represents a row from the egg table.
type EggTracking struct {
	UID                   int64          `db:"uid"`
	ID                    string         `db:"id"`
	ProfileNo             int            `db:"profile_no"`
	Level                 int            `db:"level"`
	Team                  int            `db:"team"`
	Exclusive             bool           `db:"exclusive"`
	GymID                 sql.NullString `db:"gym_id"`
	Distance              int            `db:"distance"`
	Template              string         `db:"template"`
	Clean                 int            `db:"clean"`
	Ping                  string         `db:"ping"`
	RSVPChanges           int            `db:"rsvp_changes"`
	OverrideLocationLabel string         `db:"override_location_label"`
	OverrideAreasRaw      string         `db:"override_areas"`
	OverrideAreas         []string       `db:"-"`
}

// LoadRaids loads all raid trackings from the database.
func LoadRaids(db *sqlx.DB) ([]*RaidTracking, error) {
	var raids []RaidTracking
	err := db.Select(&raids,
		`SELECT uid, id, profile_no, pokemon_id, level, team, exclusive, form, evolution,
		        move, gym_id, distance, COALESCE(template, '') AS template, clean, ping, rsvp_changes,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM raid`)
	if err != nil {
		return nil, err
	}

	result := make([]*RaidTracking, len(raids))
	for i := range raids {
		if raids[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(raids[i].OverrideAreasRaw), &raids[i].OverrideAreas)
		}
		result[i] = &raids[i]
	}
	return result, nil
}

// LoadEggs loads all egg trackings from the database.
func LoadEggs(db *sqlx.DB) ([]*EggTracking, error) {
	var eggs []EggTracking
	err := db.Select(&eggs,
		`SELECT uid, id, profile_no, level, team, exclusive, gym_id, distance,
		        COALESCE(template, '') AS template, clean, ping, rsvp_changes,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM egg`)
	if err != nil {
		return nil, err
	}

	result := make([]*EggTracking, len(eggs))
	for i := range eggs {
		if eggs[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(eggs[i].OverrideAreasRaw), &eggs[i].OverrideAreas)
		}
		result[i] = &eggs[i]
	}
	return result, nil
}
