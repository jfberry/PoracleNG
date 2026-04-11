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

// FlexString handles JSON values that may arrive as a string or a number.
// Older scanners sent some IDs (e.g. spawnpoint_id) as integers; newer ones
// send hex strings. This accepts either and stores the result as a string.
type FlexString string

func (fs *FlexString) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" {
		*fs = ""
		return nil
	}
	// Try as quoted string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*fs = FlexString(str)
		return nil
	}
	// Must be a bare number — use the raw representation
	*fs = FlexString(s)
	return nil
}

// InboundWebhook represents a single webhook entry from Golbat.
type InboundWebhook struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// PokemonWebhook mirrors Golbat's pokemon webhook message.
type PokemonWebhook struct {
	EncounterID           string     `json:"encounter_id"`
	PokemonID             int        `json:"pokemon_id"`
	Form                  int        `json:"form"`
	Latitude              float64    `json:"latitude"`
	Longitude             float64    `json:"longitude"`
	DisappearTime         int64      `json:"disappear_time"`
	DisappearTimeVerified bool       `json:"disappear_time_verified"`
	Verified              bool       `json:"verified"`
	IndividualAttack      *int       `json:"individual_attack"`
	IndividualDefense     *int       `json:"individual_defense"`
	IndividualStamina     *int       `json:"individual_stamina"`
	CP                    int        `json:"cp"`
	PokemonLevel          int        `json:"pokemon_level"`
	Move1                 int        `json:"move_1"`
	Move2                 int        `json:"move_2"`
	Gender                int        `json:"gender"`
	Weight                float64    `json:"weight"`
	Height                float64    `json:"height"`
	Size                  int        `json:"size"`
	Weather               int        `json:"weather"`
	BoostedWeather        int        `json:"boosted_weather"`
	Costume               int        `json:"costume"`
	DisplayPokemonID      int        `json:"display_pokemon_id"`
	DisplayForm           int        `json:"display_form"`
	SeenType              string     `json:"seen_type"`
	PokestopID            string     `json:"pokestop_id"`
	SpawnpointID          FlexString `json:"spawnpoint_id"`
	PokestopName          string     `json:"pokestop_name"`
	BaseCatch             float64    `json:"base_catch"`
	GreatCatch            float64    `json:"great_catch"`
	UltraCatch            float64    `json:"ultra_catch"`
	Shiny                 *bool      `json:"shiny"` // null = unknown, true/false = reported by worker

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
	S2CellID          json.Number   `json:"s2_cell_id"`
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
	Clean             int                  `json:"clean"`
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

// InvasionWebhook mirrors Golbat's invasion/pokestop webhook message.
type InvasionWebhook struct {
	PokestopID              string  `json:"pokestop_id"`
	Name                    string  `json:"pokestop_name"`
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
	Lineup                  []InvasionLineupEntry `json:"lineup"`
}

// InvasionLineupEntry is a confirmed catch from a grunt battle.
type InvasionLineupEntry struct {
	PokemonID int `json:"pokemon_id"`
	Form      int `json:"form"`
}

// QuestWebhook mirrors Golbat's quest webhook message.
type QuestWebhook struct {
	PokestopID string        `json:"pokestop_id"`
	Name       string        `json:"pokestop_name"`
	Latitude   float64       `json:"latitude"`
	Longitude  float64       `json:"longitude"`
	Title      string        `json:"title"`
	Target     int           `json:"target"`
	QuestType  int           `json:"type"`
	Template   string        `json:"template"`
	Rewards    []QuestReward `json:"rewards"`
}

// QuestReward represents a single quest reward.
type QuestReward struct {
	Type int            `json:"type"`
	Info map[string]any `json:"info"`
}

// LureWebhook mirrors a pokestop webhook with lure data.
type LureWebhook struct {
	PokestopID     string  `json:"pokestop_id"`
	Name           string  `json:"name"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	LureExpiration int64   `json:"lure_expiration"`
	LureID         int     `json:"lure_id"`
	// Invasion fields — a pokestop can have both a lure and an invasion
	IncidentGruntType  int `json:"incident_grunt_type"`
	GruntType          int `json:"grunt_type"`
	DisplayType        int `json:"display_type"`
	IncidentDisplayType int `json:"incident_display_type"`
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

// NestWebhook mirrors a nest webhook message from the nest processor.
type NestWebhook struct {
	NestID       int64   `json:"nest_id"`
	Name         string  `json:"name"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	PokemonID    int     `json:"pokemon_id"`
	Form         int     `json:"form"`
	PokemonCount uint64  `json:"pokemon_count"`
	PokemonAvg   float64 `json:"pokemon_avg"`
	PokemonRatio float64 `json:"pokemon_ratio"`
	PolyPath     string  `json:"poly_path"` // JSON-encoded polygon path
	ResetTime    int64   `json:"reset_time"`
}

// MaxbattleWebhook mirrors Golbat's max_battle webhook message.
type MaxbattleWebhook struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Latitude               float64 `json:"latitude"`
	Longitude              float64 `json:"longitude"`
	StartTime              int64   `json:"start_time"`
	EndTime                int64   `json:"end_time"`
	IsBattleAvailable      bool    `json:"is_battle_available"`
	BattleLevel            int     `json:"battle_level"`
	BattleStart            int64   `json:"battle_start"`
	BattleEnd              int64   `json:"battle_end"`
	BattlePokemonID        int     `json:"battle_pokemon_id"`
	BattlePokemonForm      int     `json:"battle_pokemon_form"`
	BattlePokemonCostume   int     `json:"battle_pokemon_costume"`
	BattlePokemonGender    int     `json:"battle_pokemon_gender"`
	BattlePokemonAlignment int     `json:"battle_pokemon_alignment"`
	BattlePokemonBreadMode int     `json:"battle_pokemon_bread_mode"`
	BattlePokemonMove1     int     `json:"battle_pokemon_move_1"`
	BattlePokemonMove2     int     `json:"battle_pokemon_move_2"`
	TotalStationedPokemon  int     `json:"total_stationed_pokemon"`
	TotalStationedGmax     int     `json:"total_stationed_gmax"`
	Updated                int64   `json:"updated"`
}

// FortWebhook mirrors Golbat's fort_update webhook message.
type FortWebhook struct {
	ChangeType string        `json:"change_type"` // "new", "edit", "removal"
	EditTypes  []string      `json:"edit_types"`  // e.g. ["name", "location", "image_url"]
	New        *FortSnapshot `json:"new"`
	Old        *FortSnapshot `json:"old"`
}

// FortSnapshot represents the new or old state of a fort in a fort_update webhook.
type FortSnapshot struct {
	ID          string       `json:"id"`
	FortType    string       `json:"type"` // "pokestop" or "gym"
	Name        string       `json:"name"`
	Description string       `json:"description"`
	ImageURL    string       `json:"image_url"`
	Location    FortLocation `json:"location"`
}

// FortLocation holds lat/lon for a fort.
type FortLocation struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// FortID returns the fort ID from whichever snapshot is present.
func (f *FortWebhook) FortID() string {
	if f.New != nil {
		return f.New.ID
	}
	if f.Old != nil {
		return f.Old.ID
	}
	return ""
}

// FortType returns the fort type from whichever snapshot is present.
func (f *FortWebhook) FortType() string {
	if f.New != nil {
		return f.New.FortType
	}
	if f.Old != nil {
		return f.Old.FortType
	}
	return ""
}

// Latitude returns the lat from whichever snapshot is present.
func (f *FortWebhook) Latitude() float64 {
	if f.New != nil {
		return f.New.Location.Lat
	}
	if f.Old != nil {
		return f.Old.Location.Lat
	}
	return 0
}

// Longitude returns the lon from whichever snapshot is present.
func (f *FortWebhook) Longitude() float64 {
	if f.New != nil {
		return f.New.Location.Lon
	}
	if f.Old != nil {
		return f.Old.Location.Lon
	}
	return 0
}

// IsEmpty returns true if neither the new nor old snapshot has a name or description.
func (f *FortWebhook) IsEmpty() bool {
	if f.New != nil && (f.New.Name != "" || f.New.Description != "") {
		return false
	}
	if f.Old != nil && (f.Old.Name != "" || f.Old.Description != "") {
		return false
	}
	return true
}

// AllChangeTypes returns a combined list of change types (edit_types + change_type).
func (f *FortWebhook) AllChangeTypes() []string {
	var types []string
	// If this was an edit from an empty fort, treat as "new"
	changeType := f.ChangeType
	if changeType == "edit" && (f.Old == nil || (f.Old.Name == "" && f.Old.Description == "")) {
		changeType = "new"
	}
	types = append(types, f.EditTypes...)
	types = append(types, changeType)
	return types
}

// FortName returns the name from whichever snapshot is present.
func (f *FortWebhook) FortName() string {
	if f.New != nil {
		return f.New.Name
	}
	if f.Old != nil {
		return f.Old.Name
	}
	return ""
}

// DeliveryJob represents a rendered alert ready for delivery to Discord/Telegram.
type DeliveryJob struct {
	Lat          string          `json:"lat"`
	Lon          string          `json:"lon"`
	Message      json.RawMessage `json:"message"` // pre-rendered JSON from DTS template
	Target       string          `json:"target"`
	Type         string          `json:"type"` // "discord:user", "telegram:group", etc.
	Name         string          `json:"name"`
	TTH          map[string]any  `json:"tth"`
	Clean        int             `json:"clean"`
	EditKey      string          `json:"editKey,omitempty"`
	Emoji        []string        `json:"emoji"`
	LogReference string          `json:"logReference"`
	Language     string          `json:"language"`
}
