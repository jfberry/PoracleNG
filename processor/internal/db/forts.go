package db

import "github.com/jmoiron/sqlx"

// FortTracking represents a row from the forts table.
type FortTracking struct {
	ID           string `db:"id"`
	ProfileNo    int    `db:"profile_no"`
	Ping         string `db:"ping"`
	Distance     int    `db:"distance"`
	Template     string `db:"template"`
	FortType     string `db:"fort_type"`
	IncludeEmpty bool   `db:"include_empty"`
	ChangeTypes  string `db:"change_types"`
}

// LoadForts loads all fort trackings from the database.
func LoadForts(db *sqlx.DB) ([]*FortTracking, error) {
	var forts []FortTracking
	err := db.Select(&forts,
		`SELECT id, profile_no, ping, distance,
		        COALESCE(template, '1') AS template,
		        COALESCE(fort_type, 'everything') AS fort_type,
		        COALESCE(include_empty, true) AS include_empty,
		        COALESCE(change_types, '[]') AS change_types
		 FROM forts`)
	if err != nil {
		return nil, err
	}

	result := make([]*FortTracking, len(forts))
	for i := range forts {
		result[i] = &forts[i]
	}
	return result, nil
}
