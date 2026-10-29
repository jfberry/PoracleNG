package db

import (
	"github.com/jmoiron/sqlx"
)

// TappableTracking represents a row from the tappable table.
//
// RewardType uses the quest reward type ids (2 item, 3 stardust, 4 candy,
// 8 pokecoin, 9 XL candy, 12 mega energy). Reward is the item / pokemon id
// (0 = any) or, for stardust and pokecoin, the minimum amount. Amount is the
// minimum count for item / candy / XL candy / mega energy rewards.
type TappableTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	RewardType            int      `db:"reward_type"`
	Reward                int      `db:"reward"`
	Amount                int      `db:"amount"`
	Distance              int      `db:"distance"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Template              string   `db:"template"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadTappables loads all tappable trackings from the database.
func LoadTappables(db *sqlx.DB) ([]*TappableTracking, error) {
	var tappables []TappableTracking
	err := db.Select(&tappables,
		`SELECT uid, id, profile_no, reward_type, reward, amount,
		        distance, ping, clean, COALESCE(template, '') AS template,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM tappable`)
	if err != nil {
		return nil, err
	}

	result := make([]*TappableTracking, len(tappables))
	for i := range tappables {
		tappables[i].OverrideAreas = loadOverrideKeys(tappables[i].OverrideAreasRaw)
		result[i] = &tappables[i]
	}
	return result, nil
}
