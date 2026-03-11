package matching

import (
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// QuestRewardData holds a single parsed quest reward for matching.
type QuestRewardData struct {
	Type      int // 2=item, 3=stardust, 4=candy, 7=pokemon, 12=mega energy
	PokemonID int
	ItemID    int
	Amount    int
	FormID    int
	Shiny     bool
}

// QuestData holds processed quest data for matching.
type QuestData struct {
	PokestopID string
	Latitude   float64
	Longitude  float64
	Rewards    []QuestRewardData
}

// QuestMatcher performs in-memory quest matching.
type QuestMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for a quest.
func (m *QuestMatcher) Match(data *QuestData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, q := range st.Quests {
		if !questRewardMatches(q, data.Rewards) {
			continue
		}

		trackings = append(trackings, trackingUserData{
			HumanID:   q.ID,
			ProfileNo: q.ProfileNo,
			Distance:  q.Distance,
			Template:  q.Template,
			Clean:     q.Clean,
			Ping:      q.Ping,
		})
	}

	return ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"quest",
	)
}

// questRewardMatches checks if any quest reward matches the tracking entry.
func questRewardMatches(q *db.QuestTracking, rewards []QuestRewardData) bool {
	for _, r := range rewards {
		if singleRewardMatches(q, &r) {
			return true
		}
	}
	return false
}

func singleRewardMatches(q *db.QuestTracking, r *QuestRewardData) bool {
	if q.RewardType != r.Type {
		return false
	}

	switch r.Type {
	case 7: // pokemon
		if q.Reward != r.PokemonID {
			return false
		}
		if q.Form != 0 && q.Form != r.FormID {
			return false
		}
		if q.Shiny && !r.Shiny {
			return false
		}
		return true

	case 2: // item
		if q.Reward != r.ItemID {
			return false
		}
		if q.Amount > 0 && r.Amount < q.Amount {
			return false
		}
		return true

	case 3: // stardust
		if q.Reward > r.Amount {
			return false
		}
		return true

	case 12: // mega energy
		if q.Reward != 0 && q.Reward != r.PokemonID {
			return false
		}
		if q.Amount > 0 && r.Amount < q.Amount {
			return false
		}
		return true

	case 4: // candy
		if q.Reward != 0 && q.Reward != r.PokemonID {
			return false
		}
		if q.Amount > 0 && r.Amount < q.Amount {
			return false
		}
		return true
	}

	return false
}
