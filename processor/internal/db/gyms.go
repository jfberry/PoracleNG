package db

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// GymTracking represents a row from the gym table.
type GymTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Distance              int      `db:"distance"`
	Template              string   `db:"template"`
	Team                  int      `db:"team"`
	SlotChanges           bool     `db:"slot_changes"`
	GymID                 *string  `db:"gym_id"`
	BattleChanges         bool     `db:"battle_changes"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadGyms loads all gym trackings from the database.
func LoadGyms(db *sqlx.DB) ([]*GymTracking, error) {
	var gyms []GymTracking
	err := db.Select(&gyms,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, slot_changes,
		        gym_id, COALESCE(battle_changes, false) AS battle_changes,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM gym`)
	if err != nil {
		return nil, err
	}

	result := make([]*GymTracking, len(gyms))
	for i := range gyms {
		if gyms[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(gyms[i].OverrideAreasRaw), &gyms[i].OverrideAreas)
		}
		result[i] = &gyms[i]
	}
	return result, nil
}
