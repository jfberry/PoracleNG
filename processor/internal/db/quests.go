package db

import "github.com/jmoiron/sqlx"

// QuestTracking represents a row from the quest table.
type QuestTracking struct {
	ID         string `db:"id"`
	ProfileNo  int    `db:"profile_no"`
	Ping       string `db:"ping"`
	Clean      int    `db:"clean"`
	Reward     int    `db:"reward"`
	Template   string `db:"template"`
	Shiny      bool   `db:"shiny"`
	RewardType int    `db:"reward_type"`
	Distance   int    `db:"distance"`
	Form       int    `db:"form"`
	Amount     int    `db:"amount"`
}

// LoadQuests loads all quest trackings from the database.
func LoadQuests(db *sqlx.DB) ([]*QuestTracking, error) {
	var quests []QuestTracking
	err := db.Select(&quests,
		`SELECT id, profile_no, ping, clean, reward,
		        COALESCE(template, '') AS template, shiny, reward_type,
		        distance, COALESCE(form, 0) AS form,
		        COALESCE(amount, 0) AS amount
		 FROM quest`)
	if err != nil {
		return nil, err
	}

	result := make([]*QuestTracking, len(quests))
	for i := range quests {
		result[i] = &quests[i]
	}
	return result, nil
}
