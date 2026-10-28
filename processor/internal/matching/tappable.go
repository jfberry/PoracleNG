package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Tappable reward types. The ids are quest's (QuestRewardProto.Type) so
// rowtext, translations and icons are shared with quest.
const (
	TappableRewardItem       = 2
	TappableRewardStardust   = 3
	TappableRewardCandy      = 4
	TappableRewardPokecoin   = 8
	TappableRewardXLCandy    = 9
	TappableRewardMegaEnergy = 12
)

// TappableRewardData holds a single parsed tappable reward for matching.
// Type is one of the TappableReward* ids; an entry with any other Type (a
// reward Poracle can't track) never matches a rule.
type TappableRewardData struct {
	Type      int
	PokemonID int // candy / XL candy / mega energy
	ItemID    int // item
	Amount    int // the reward count (stardust / pokecoin amount included)
	// Key is Golbat's LootItemProto oneof name ("item", "pokemon_candy",
	// "experience", ...). Kept so enrichment can describe rewards Poracle
	// can't track (Type 0).
	Key string
}

// TappableData holds processed tappable data for matching. Rewards are parsed
// once per event by the handler.
type TappableData struct {
	EncounterID string
	PokestopID  string // empty when the tappable is not on a pokestop
	Latitude    float64
	Longitude   float64
	Rewards     []TappableRewardData
}

// TappableMatcher performs in-memory tappable matching.
type TappableMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for a tappable along with the geofence
// areas that contain it. A rule matches when any of the tappable's rewards
// matches it (quest semantics); users are deduplicated by ValidateHumansGeneric.
func (m *TappableMatcher) Match(data *TappableData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeTappable).Observe(time.Since(start).Seconds())
	}()

	if st == nil || len(data.Rewards) == 0 {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, t := range st.Tappables {
		if !tappableRewardMatches(t, data.Rewards) {
			continue
		}
		trackings = append(trackings, trackingUserData{
			RuleTable:             "tappable",
			HumanID:               t.ID,
			ProfileNo:             t.ProfileNo,
			Distance:              t.Distance,
			Template:              t.Template,
			Clean:                 t.Clean,
			Ping:                  t.Ping,
			UID:                   t.UID,
			OverrideLocationLabel: t.OverrideLocationLabel,
			OverrideAreas:         t.OverrideAreas,
		})
	}

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeTappable).Observe(float64(len(trackings)))

	users := ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"tappable",
	)
	return users, ConvertAreas(areas)
}

// tappableRewardMatches reports whether any reward matches the rule.
func tappableRewardMatches(t *db.TappableTracking, rewards []TappableRewardData) bool {
	for i := range rewards {
		if tappableSingleRewardMatches(t, &rewards[i]) {
			return true
		}
	}
	return false
}

func tappableSingleRewardMatches(t *db.TappableTracking, r *TappableRewardData) bool {
	if t.RewardType != r.Type {
		return false
	}
	switch r.Type {
	case TappableRewardItem:
		// reward 0 = any item
		if t.Reward != 0 && t.Reward != r.ItemID {
			return false
		}
		return t.Amount <= 0 || r.Amount >= t.Amount

	case TappableRewardStardust, TappableRewardPokecoin:
		// The minimum amount lives in reward (quest quirk).
		return r.Amount >= t.Reward

	case TappableRewardCandy, TappableRewardXLCandy, TappableRewardMegaEnergy:
		// reward 0 = any pokemon
		if t.Reward != 0 && t.Reward != r.PokemonID {
			return false
		}
		return t.Amount <= 0 || r.Amount >= t.Amount
	}
	return false
}
