package db

import "github.com/jmoiron/sqlx"

// MaxbattleTracking represents a row from the maxbattle table.
type MaxbattleTracking struct {
	ID        string  `db:"id"`
	ProfileNo int     `db:"profile_no"`
	PokemonID int     `db:"pokemon_id"`
	Gmax      int     `db:"gmax"`
	Level     int     `db:"level"`
	Form      int     `db:"form"`
	Move      int     `db:"move"`
	Evolution int     `db:"evolution"`
	Distance  int     `db:"distance"`
	StationID *string `db:"station_id"`
	Ping      string  `db:"ping"`
	Clean     int     `db:"clean"`
	Template  string  `db:"template"`
}

// LoadMaxbattles loads all maxbattle trackings from the database.
func LoadMaxbattles(db *sqlx.DB) ([]*MaxbattleTracking, error) {
	var maxbattles []MaxbattleTracking
	err := db.Select(&maxbattles,
		`SELECT id, profile_no, pokemon_id, gmax, level, form, move, evolution,
		        distance, station_id, ping, clean, COALESCE(template, '') AS template
		 FROM maxbattle`)
	if err != nil {
		return nil, err
	}

	result := make([]*MaxbattleTracking, len(maxbattles))
	for i := range maxbattles {
		result[i] = &maxbattles[i]
	}
	return result, nil
}
