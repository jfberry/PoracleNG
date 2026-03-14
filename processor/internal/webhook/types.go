package webhook

import (
	"encoding/json"
	"strconv"
)

// FlexBool handles JSON booleans that may arrive as true/false or 0/1.
type FlexBool bool

func (fb *FlexBool) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "true" || s == "1" {
		*fb = true
		return nil
	}
	if s == "false" || s == "0" || s == "null" {
		*fb = false
		return nil
	}
	// Try parsing as a number
	n, err := strconv.ParseFloat(s, 64)
	if err == nil {
		*fb = FlexBool(n != 0)
		return nil
	}
	// Try unquoted string
	*fb = false
	return nil
}

// InboundWebhook represents a single webhook entry from Golbat.
type InboundWebhook struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// PokemonWebhook mirrors Golbat's pokemon webhook message.
type PokemonWebhook struct {
	EncounterID           string  `json:"encounter_id"`
	PokemonID             int     `json:"pokemon_id"`
	Form                  int     `json:"form"`
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	DisappearTime         int64   `json:"disappear_time"`
	DisappearTimeVerified bool    `json:"disappear_time_verified"`
	Verified              bool    `json:"verified"`
	IndividualAttack      *int    `json:"individual_attack"`
	IndividualDefense     *int    `json:"individual_defense"`
	IndividualStamina     *int    `json:"individual_stamina"`
	CP                    int     `json:"cp"`
	PokemonLevel          int     `json:"pokemon_level"`
	Move1                 int     `json:"move_1"`
	Move2                 int     `json:"move_2"`
	Gender                int     `json:"gender"`
	Weight                float64 `json:"weight"`
	Height                float64 `json:"height"`
	Size                  int     `json:"size"`
	Weather               int     `json:"weather"`
	BoostedWeather        int     `json:"boosted_weather"`
	Costume               int     `json:"costume"`
	DisplayPokemonID      int     `json:"display_pokemon_id"`
	DisplayForm           int     `json:"display_form"`
	SeenType              string  `json:"seen_type"`
	PokestopID            string  `json:"pokestop_id"`
	SpawnpointID          string  `json:"spawnpoint_id"`
	PokestopName          string  `json:"pokestop_name"`
	BaseCatch             float64 `json:"base_catch"`
	GreatCatch            float64 `json:"great_catch"`
	UltraCatch            float64 `json:"ultra_catch"`

	// PVP data from Golbat (sole source of PVP rankings)
	PVP                     map[string][]PVPRankEntry `json:"pvp"`
	PVPRankingsGreatLeague  []PVPRankEntry            `json:"pvp_rankings_great_league"`
	PVPRankingsUltraLeague  []PVPRankEntry            `json:"pvp_rankings_ultra_league"`
	PVPRankingsLittleLeague []PVPRankEntry            `json:"pvp_rankings_little_league"`
}

// PVPRankEntry represents a single PVP ranking entry from Golbat.
type PVPRankEntry struct {
	Pokemon    int     `json:"pokemon"`
	Form       int     `json:"form"`
	Cap        int     `json:"cap"`
	Capped     bool    `json:"capped"`
	Rank       int     `json:"rank"`
	CP         int     `json:"cp"`
	Level      float64 `json:"level"`
	Percentage float64 `json:"percentage"`
	Evolution  int     `json:"evolution"`
}

// RaidWebhook mirrors Golbat's raid webhook message.
type RaidWebhook struct {
	GymID            string   `json:"gym_id"`
	GymName          string   `json:"gym_name"`
	GymURL           string   `json:"gym_url"`
	Name             string   `json:"name"`
	URL              string   `json:"url"`
	Latitude         float64  `json:"latitude"`
	Longitude        float64  `json:"longitude"`
	PokemonID        int      `json:"pokemon_id"`
	Form             int      `json:"form"`
	Gender           int      `json:"gender"`
	Costume          int      `json:"costume"`
	Evolution        int      `json:"evolution"`
	Alignment        int      `json:"alignment"`
	Level            int      `json:"level"`
	TeamID           int      `json:"team_id"`
	Start            int64    `json:"start"`
	End              int64    `json:"end"`
	Move1            int      `json:"move_1"`
	Move2            int      `json:"move_2"`
	ExRaidEligible   FlexBool `json:"ex_raid_eligible"`
	IsExRaidEligible FlexBool `json:"is_ex_raid_eligible"`
	RSVPs            []RSVP   `json:"rsvps"`
}

// RSVP represents a raid RSVP timeslot.
type RSVP struct {
	Timeslot   int64 `json:"timeslot"`
	GoingCount int   `json:"going_count"`
	MaybeCount int   `json:"maybe_count"`
}

// WeatherWebhook mirrors Golbat's weather webhook message.
type WeatherWebhook struct {
	S2CellID          string        `json:"s2_cell_id"`
	Latitude          float64       `json:"latitude"`
	Longitude         float64       `json:"longitude"`
	Polygon           [4][2]float64 `json:"polygon"`
	GameplayCondition int           `json:"gameplay_condition"`
	Updated           int64         `json:"updated"`
}

// MatchedArea represents a geofence area that a point falls within.
type MatchedArea struct {
	Name             string `json:"name"`
	DisplayInMatches bool   `json:"displayInMatches"`
	Group            string `json:"group"`
}

// ActivePokemonEntry represents a pokemon affected by a weather change for a user.
type ActivePokemonEntry struct {
	PokemonID     int     `json:"pokemon_id"`
	Form          int     `json:"form"`
	IV            float64 `json:"iv"`
	CP            int     `json:"cp"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	DisappearTime int64   `json:"disappear_time"`
}

// MatchedUser represents a user who matched an alert.
type MatchedUser struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Type              string               `json:"type"`
	Language          string               `json:"language"`
	Latitude          float64              `json:"latitude"`
	Longitude         float64              `json:"longitude"`
	Template          string               `json:"template"`
	Distance          int                  `json:"distance"`
	Clean             bool                 `json:"clean"`
	Ping              string               `json:"ping"`
	Bearing           int                  `json:"bearing"`
	CardinalDirection string               `json:"cardinalDirection"`
	PokemonID         int                  `json:"pokemon_id"`
	PVPRankingCap     int                  `json:"pvp_ranking_cap"`
	PVPRankingLeague  int                  `json:"pvp_ranking_league"`
	PVPRankingWorst   int                  `json:"pvp_ranking_worst"`
	RSVPChanges       int                  `json:"rsvp_changes"`
	ActivePokemons    []ActivePokemonEntry `json:"active_pokemons,omitempty"`
}

// OutboundPayload is sent from processor to alerter.
type OutboundPayload struct {
	Type         string                 `json:"type"`
	Message      json.RawMessage        `json:"message"`
	Enrichment   map[string]interface{} `json:"enrichment,omitempty"`
	MatchedAreas []MatchedArea          `json:"matched_areas"`
	MatchedUsers []MatchedUser          `json:"matched_users"`
	OldState     *EncounterOld          `json:"old_state,omitempty"`
}

// EncounterOld holds old state for pokemon_changed events.
type EncounterOld struct {
	PokemonID int     `json:"pokemon_id"`
	Form      int     `json:"form"`
	Weather   int     `json:"weather"`
	CP        int     `json:"cp"`
	IV        float64 `json:"iv"`
}

// InvasionWebhook mirrors Golbat's invasion/pokestop webhook message.
type InvasionWebhook struct {
	PokestopID              string  `json:"pokestop_id"`
	Name                    string  `json:"name"`
	Latitude                float64 `json:"latitude"`
	Longitude               float64 `json:"longitude"`
	IncidentExpiration      int64   `json:"incident_expiration"`
	IncidentExpireTimestamp int64   `json:"incident_expire_timestamp"`
	IncidentGruntType       int     `json:"incident_grunt_type"`
	GruntType               int     `json:"grunt_type"`
	Gender                  int     `json:"gender"`
	DisplayType             int     `json:"display_type"`
	IncidentDisplayType     int     `json:"incident_display_type"`
	Confirmed               bool    `json:"confirmed"`
}

// QuestWebhook mirrors Golbat's quest webhook message.
type QuestWebhook struct {
	PokestopID string        `json:"pokestop_id"`
	Name       string        `json:"pokestop_name"`
	Latitude   float64       `json:"latitude"`
	Longitude  float64       `json:"longitude"`
	Rewards    []QuestReward `json:"rewards"`
}

// QuestReward represents a single quest reward.
type QuestReward struct {
	Type int                    `json:"type"`
	Info map[string]interface{} `json:"info"`
}

// LureWebhook mirrors a pokestop webhook with lure data.
type LureWebhook struct {
	PokestopID     string  `json:"pokestop_id"`
	Name           string  `json:"name"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	LureExpiration int64   `json:"lure_expiration"`
	LureID         int     `json:"lure_id"`
}

// GymWebhook mirrors Golbat's gym/gym_details webhook message.
type GymWebhook struct {
	GymID          string   `json:"gym_id"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	TeamID         int      `json:"team_id"`
	Team           int      `json:"team"`
	SlotsAvailable int      `json:"slots_available"`
	IsInBattle     FlexBool `json:"is_in_battle"`
	InBattle       FlexBool `json:"in_battle"`
	LastOwnerID    int      `json:"last_owner_id"`
}

// NestWebhook mirrors a nest webhook message.
type NestWebhook struct {
	NestID     int64   `json:"nest_id"`
	PokemonID  int     `json:"pokemon_id"`
	Form       int     `json:"form"`
	PokemonAvg float64 `json:"pokemon_avg"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	ResetTime  int64   `json:"reset_time"`
}

// FortWebhook mirrors a fort_update webhook message.
type FortWebhook struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	FortType    string   `json:"type"`
	IsEmpty     bool     `json:"is_empty"`
	ChangeTypes []string `json:"change_types"`
	ResetTime   int64    `json:"reset_time"`
}
