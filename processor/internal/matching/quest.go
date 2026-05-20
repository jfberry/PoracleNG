package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
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

// Match returns matched users for a quest split into two buckets — those
// for rules with no summary bit (immediate dispatch) and those for rules
// with the summary bit set (buffered for grouped delivery). The third
// return is the geofence areas that contain the pokestop and is
// independent of the immediate/buffered split: the area listing reflects
// whether ANY rule matched.
//
// A user with both an immediate and a summary rule is returned in BOTH
// slices because the matcher pre-partitions trackings by summary bit and
// runs human validation per partition (each call dedups within itself).
func (m *QuestMatcher) Match(data *QuestData, st *state.State) (immediate []webhook.MatchedUser, buffered []webhook.MatchedUser, areas []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeQuest).Observe(time.Since(start).Seconds())
	}()


	if st == nil {
		return nil, nil, nil
	}

	geoAreas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)
	var immediateTrackings []trackingUserData
	var bufferedTrackings []trackingUserData

	for _, q := range st.Quests {
		if !questRewardMatches(q, data.Rewards) {
			continue
		}

		td := trackingUserData{
			HumanID:   q.ID,
			ProfileNo: q.ProfileNo,
			Distance:  q.Distance,
			Template:  q.Template,
			Clean:     q.Clean,
			Ping:      q.Ping,
			UID:       q.UID,
		}
		if db.IsSummary(q.Clean) {
			bufferedTrackings = append(bufferedTrackings, td)
		} else {
			immediateTrackings = append(immediateTrackings, td)
		}
	}

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeQuest).Observe(float64(len(immediateTrackings) + len(bufferedTrackings)))

	strict := m.AreaSecurityEnabled && m.StrictLocations
	immediate = ValidateHumansGeneric(
		immediateTrackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		strict,
		st.Humans,
		"quest",
	)
	buffered = ValidateHumansGeneric(
		bufferedTrackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		strict,
		st.Humans,
		"quest",
	)
	return immediate, buffered, ConvertAreas(geoAreas)
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
