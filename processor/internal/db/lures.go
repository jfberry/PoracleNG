package db

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// LureTracking represents a row from the lures table.
type LureTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Distance              int      `db:"distance"`
	Template              string   `db:"template"`
	LureID                int      `db:"lure_id"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadLures loads all lure trackings from the database.
func LoadLures(db *sqlx.DB) ([]*LureTracking, error) {
	var lures []LureTracking
	err := db.Select(&lures,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, lure_id,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM lures`)
	if err != nil {
		return nil, err
	}

	result := make([]*LureTracking, len(lures))
	for i := range lures {
		if lures[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(lures[i].OverrideAreasRaw), &lures[i].OverrideAreas)
		}
		result[i] = &lures[i]
	}
	return result, nil
}
