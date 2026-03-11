package db

import "github.com/jmoiron/sqlx"

// NestTracking represents a row from the nests table.
type NestTracking struct {
	ID          string `db:"id"`
	ProfileNo   int    `db:"profile_no"`
	Ping        string `db:"ping"`
	Clean       bool   `db:"clean"`
	Distance    int    `db:"distance"`
	Template    string `db:"template"`
	PokemonID   int    `db:"pokemon_id"`
	MinSpawnAvg int    `db:"min_spawn_avg"`
	Form        int    `db:"form"`
}

// LoadNests loads all nest trackings from the database.
func LoadNests(db *sqlx.DB) ([]*NestTracking, error) {
	var nests []NestTracking
	err := db.Select(&nests,
		`SELECT id, profile_no, ping, clean, distance,
		        COALESCE(template, '1') AS template, pokemon_id,
		        min_spawn_avg, COALESCE(form, 0) AS form
		 FROM nests`)
	if err != nil {
		return nil, err
	}

	result := make([]*NestTracking, len(nests))
	for i := range nests {
		result[i] = &nests[i]
	}
	return result, nil
}
