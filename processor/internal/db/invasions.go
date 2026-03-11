package db

import "github.com/jmoiron/sqlx"

// InvasionTracking represents a row from the invasion table.
type InvasionTracking struct {
	ID        string `db:"id"`
	ProfileNo int    `db:"profile_no"`
	Ping      string `db:"ping"`
	Clean     bool   `db:"clean"`
	Distance  int    `db:"distance"`
	Template  string `db:"template"`
	Gender    int    `db:"gender"`
	GruntType string `db:"grunt_type"`
}

// LoadInvasions loads all invasion trackings from the database.
func LoadInvasions(db *sqlx.DB) ([]*InvasionTracking, error) {
	var invasions []InvasionTracking
	err := db.Select(&invasions,
		`SELECT id, profile_no, ping, clean, distance,
		        COALESCE(template, '1') AS template, gender, grunt_type
		 FROM invasion`)
	if err != nil {
		return nil, err
	}

	result := make([]*InvasionTracking, len(invasions))
	for i := range invasions {
		result[i] = &invasions[i]
	}
	return result, nil
}
