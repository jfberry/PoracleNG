package db

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// RaidTracking represents a row from the raid table.
type RaidTracking struct {
	ID          string         `db:"id"`
	ProfileNo   int            `db:"profile_no"`
	PokemonID   int            `db:"pokemon_id"`
	Level       int            `db:"level"`
	Team        int            `db:"team"`
	Exclusive   bool           `db:"exclusive"`
	Form        int            `db:"form"`
	Evolution   int            `db:"evolution"`
	Move        int            `db:"move"`
	GymID       sql.NullString `db:"gym_id"`
	Distance    int            `db:"distance"`
	Template    string         `db:"template"`
	Clean       int            `db:"clean"`
	Ping        string         `db:"ping"`
	RSVPChanges int            `db:"rsvp_changes"`
}

// EggTracking represents a row from the egg table.
type EggTracking struct {
	ID          string         `db:"id"`
	ProfileNo   int            `db:"profile_no"`
	Level       int            `db:"level"`
	Team        int            `db:"team"`
	Exclusive   bool           `db:"exclusive"`
	GymID       sql.NullString `db:"gym_id"`
	Distance    int            `db:"distance"`
	Template    string         `db:"template"`
	Clean       int            `db:"clean"`
	Ping        string         `db:"ping"`
	RSVPChanges int            `db:"rsvp_changes"`
}

// LoadRaids loads all raid trackings from the database.
func LoadRaids(db *sqlx.DB) ([]*RaidTracking, error) {
	var raids []RaidTracking
	err := db.Select(&raids,
		`SELECT id, profile_no, pokemon_id, level, team, exclusive, form, evolution,
		        move, gym_id, distance, COALESCE(template, '') AS template, clean, ping, rsvp_changes
		 FROM raid`)
	if err != nil {
		return nil, err
	}

	result := make([]*RaidTracking, len(raids))
	for i := range raids {
		result[i] = &raids[i]
	}
	return result, nil
}

// LoadEggs loads all egg trackings from the database.
func LoadEggs(db *sqlx.DB) ([]*EggTracking, error) {
	var eggs []EggTracking
	err := db.Select(&eggs,
		`SELECT id, profile_no, level, team, exclusive, gym_id, distance,
		        COALESCE(template, '') AS template, clean, ping, rsvp_changes
		 FROM egg`)
	if err != nil {
		return nil, err
	}

	result := make([]*EggTracking, len(eggs))
	for i := range eggs {
		result[i] = &eggs[i]
	}
	return result, nil
}
