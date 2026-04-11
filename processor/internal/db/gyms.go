package db

import "github.com/jmoiron/sqlx"

// GymTracking represents a row from the gym table.
type GymTracking struct {
	ID            string  `db:"id"`
	ProfileNo     int     `db:"profile_no"`
	Ping          string  `db:"ping"`
	Clean         int     `db:"clean"`
	Distance      int     `db:"distance"`
	Template      string  `db:"template"`
	Team          int     `db:"team"`
	SlotChanges   bool    `db:"slot_changes"`
	GymID         *string `db:"gym_id"`
	BattleChanges bool    `db:"battle_changes"`
}

// LoadGyms loads all gym trackings from the database.
func LoadGyms(db *sqlx.DB) ([]*GymTracking, error) {
	var gyms []GymTracking
	err := db.Select(&gyms,
		`SELECT id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, slot_changes,
		        gym_id, COALESCE(battle_changes, false) AS battle_changes
		 FROM gym`)
	if err != nil {
		return nil, err
	}

	result := make([]*GymTracking, len(gyms))
	for i := range gyms {
		result[i] = &gyms[i]
	}
	return result, nil
}
