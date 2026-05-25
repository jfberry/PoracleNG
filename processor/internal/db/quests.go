package db

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// QuestTracking represents a row from the quest table.
type QuestTracking struct {
	UID                   int64    `db:"uid"`
	ID                    string   `db:"id"`
	ProfileNo             int      `db:"profile_no"`
	Ping                  string   `db:"ping"`
	Clean                 int      `db:"clean"`
	Reward                int      `db:"reward"`
	Template              string   `db:"template"`
	Shiny                 bool     `db:"shiny"`
	RewardType            int      `db:"reward_type"`
	Distance              int      `db:"distance"`
	Form                  int      `db:"form"`
	Amount                int      `db:"amount"`
	OverrideLocationLabel string   `db:"override_location_label"`
	OverrideAreasRaw      string   `db:"override_areas"`
	OverrideAreas         []string `db:"-"`
}

// LoadQuests loads all quest trackings from the database.
func LoadQuests(db *sqlx.DB) ([]*QuestTracking, error) {
	var quests []QuestTracking
	err := db.Select(&quests,
		`SELECT uid, id, profile_no, ping, clean, reward,
		        COALESCE(template, '') AS template, shiny, reward_type,
		        distance, COALESCE(form, 0) AS form,
		        COALESCE(amount, 0) AS amount,
		        COALESCE(override_location_label, '') AS override_location_label,
		        COALESCE(override_areas, '') AS override_areas
		 FROM quest`)
	if err != nil {
		return nil, err
	}

	result := make([]*QuestTracking, len(quests))
	for i := range quests {
		if quests[i].OverrideAreasRaw != "" {
			_ = json.Unmarshal([]byte(quests[i].OverrideAreasRaw), &quests[i].OverrideAreas)
		}
		result[i] = &quests[i]
	}
	return result, nil
}
