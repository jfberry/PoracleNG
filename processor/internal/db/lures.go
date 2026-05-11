package db

import "github.com/jmoiron/sqlx"

// LureTracking represents a row from the lures table.
type LureTracking struct {
	ID        string `db:"id"`
	ProfileNo int    `db:"profile_no"`
	Ping      string `db:"ping"`
	Clean     int    `db:"clean"`
	Distance  int    `db:"distance"`
	Template  string `db:"template"`
	LureID    int    `db:"lure_id"`
}

// LoadLures loads all lure trackings from the database.
func LoadLures(db *sqlx.DB) ([]*LureTracking, error) {
	var lures []LureTracking
	err := db.Select(&lures,
		`SELECT id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, lure_id
		 FROM lures`)
	if err != nil {
		return nil, err
	}

	result := make([]*LureTracking, len(lures))
	for i := range lures {
		result[i] = &lures[i]
	}
	return result, nil
}
