package db

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// FortTracking represents a row from the forts table.
type FortTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Distance              int      `db:"distance"`
	Template              string   `db:"template"`
	FortType              string   `db:"fort_type"`
	IncludeEmpty          bool     `db:"include_empty"`
	ChangeTypes           string   `db:"change_types"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadForts loads all fort trackings from the database.
func LoadForts(db *sqlx.DB) ([]*FortTracking, error) {
	var forts []FortTracking
	err := db.Select(&forts,
		`SELECT uid, id, profile_no, ping, distance,
		        COALESCE(template, '') AS template,
		        COALESCE(fort_type, 'everything') AS fort_type,
		        COALESCE(include_empty, true) AS include_empty,
		        COALESCE(change_types, '[]') AS change_types,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM forts`)
	if err != nil {
		return nil, err
	}

	result := make([]*FortTracking, len(forts))
	for i := range forts {
		if forts[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(forts[i].OverrideAreasRaw), &forts[i].OverrideAreas)
		}
		result[i] = &forts[i]
	}
	return result, nil
}
