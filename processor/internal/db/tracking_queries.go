package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/guregu/null/v6"
	"github.com/jmoiron/sqlx"
)

// HumanAPI represents a human record for API operations.
type HumanAPI struct {
	ID               string         `db:"id" json:"id"`
	Name             string         `db:"name" json:"name"`
	Type             string         `db:"type" json:"type"`
	Enabled          IntBool        `db:"enabled" json:"enabled"`
	Language         sql.NullString `db:"language" json:"-"`
	CurrentProfileNo int            `db:"current_profile_no" json:"current_profile_no"`
}

// LanguageOrDefault returns the human's language, or the given default if not set.
func (h *HumanAPI) LanguageOrDefault(defaultLang string) string {
	if h.Language.Valid && h.Language.String != "" {
		return h.Language.String
	}
	return defaultLang
}

// SelectOneHuman looks up a single human by ID.
func SelectOneHuman(db *sqlx.DB, id string) (*HumanAPI, error) {
	var h HumanAPI
	err := db.Get(&h,
		`SELECT id, name, type, enabled, language, current_profile_no
		 FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select human %s: %w", id, err)
	}
	return &h, nil
}

// DeleteByUID deletes a single row from a tracking table by id and uid.
func DeleteByUID(db *sqlx.DB, table, id string, uid int64) error {
	_, err := db.Exec(
		fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND uid = ?", table),
		id, uid)
	if err != nil {
		return fmt.Errorf("delete from %s uid %d: %w", table, uid, err)
	}
	return nil
}

// DeleteByUIDs deletes multiple rows from a tracking table by id and uid list.
func DeleteByUIDs(db *sqlx.DB, table, id string, uids []int64) error {
	if len(uids) == 0 {
		return nil
	}
	placeholders := make([]string, len(uids))
	args := make([]any, 0, len(uids)+1)
	args = append(args, id)
	for i, uid := range uids {
		placeholders[i] = "?"
		args = append(args, uid)
	}
	query := fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND uid IN (%s)",
		table, strings.Join(placeholders, ","))
	_, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("delete from %s uids: %w", table, err)
	}
	return nil
}

// LureTrackingAPI represents a lure tracking row including the uid for API operations.
// The diff tag drives the generic diffTracking function:
//   - diff:"-"      skip (uid, id, profile_no)
//   - diff:"match"  match key for finding related existing rows
//   - diff:"update" updatable fields (if ALL diffs are here → update in place)
//   - (no tag)      regular field (any diff → new insert)
type LureTrackingAPI struct {
	UID       int64  `db:"uid"        json:"uid"        diff:"-"`
	ID        string `db:"id"         json:"id"         diff:"-"`
	ProfileNo int    `db:"profile_no" json:"profile_no" diff:"-"`
	Ping      string `db:"ping"       json:"ping"`
	Clean     int    `db:"clean"      json:"clean"      diff:"update"`
	Distance  int    `db:"distance"   json:"distance"   diff:"update"`
	Template  string `db:"template"   json:"template"   diff:"update"`
	LureID    int    `db:"lure_id"    json:"lure_id"    diff:"match"`
}

// SelectLuresByIDProfile returns all lure trackings for a given human and profile.
func SelectLuresByIDProfile(db *sqlx.DB, id string, profileNo int) ([]LureTrackingAPI, error) {
	var lures []LureTrackingAPI
	err := db.Select(&lures,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, lure_id
		 FROM lures WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select lures for %s profile %d: %w", id, profileNo, err)
	}
	return lures, nil
}

// InsertLure inserts a single lure tracking row and returns the new uid.
func InsertLure(db *sqlx.DB, lure *LureTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO lures (id, profile_no, ping, clean, distance, template, lure_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		lure.ID, lure.ProfileNo, lure.Ping, lure.Clean, lure.Distance,
		lure.Template, lure.LureID)
	if err != nil {
		return 0, fmt.Errorf("insert lure: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// FortTrackingAPI represents a fort tracking row for API operations.
type FortTrackingAPI struct {
	UID          int64  `db:"uid"           json:"uid"           diff:"-"`
	ID           string `db:"id"            json:"id"            diff:"-"`
	ProfileNo    int    `db:"profile_no"    json:"profile_no"    diff:"-"`
	Ping         string `db:"ping"          json:"ping"`
	Distance     int    `db:"distance"      json:"distance"      diff:"update"`
	Template     string `db:"template"      json:"template"      diff:"update"`
	FortType     string `db:"fort_type"     json:"fort_type"     diff:"match"`
	IncludeEmpty IntBool `db:"include_empty" json:"include_empty"`
	ChangeTypes  string `db:"change_types"  json:"change_types"`
}

// SelectFortsByIDProfile returns all fort trackings for a given human and profile.
func SelectFortsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]FortTrackingAPI, error) {
	var forts []FortTrackingAPI
	err := db.Select(&forts,
		`SELECT uid, id, profile_no, ping, distance,
		        COALESCE(template, '') AS template,
		        COALESCE(fort_type, 'everything') AS fort_type,
		        COALESCE(include_empty, true) AS include_empty,
		        COALESCE(change_types, '[]') AS change_types
		 FROM forts WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select forts for %s profile %d: %w", id, profileNo, err)
	}
	return forts, nil
}

// InsertFort inserts a single fort tracking row and returns the new uid.
func InsertFort(db *sqlx.DB, fort *FortTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO forts (id, profile_no, ping, distance, template, fort_type, include_empty, change_types)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		fort.ID, fort.ProfileNo, fort.Ping, fort.Distance,
		fort.Template, fort.FortType, fort.IncludeEmpty, fort.ChangeTypes)
	if err != nil {
		return 0, fmt.Errorf("insert fort: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// InvasionTrackingAPI represents an invasion tracking row for API operations.
type InvasionTrackingAPI struct {
	UID       int64  `db:"uid"        json:"uid"        diff:"-"`
	ID        string `db:"id"         json:"id"         diff:"-"`
	ProfileNo int    `db:"profile_no" json:"profile_no" diff:"-"`
	Ping      string `db:"ping"       json:"ping"`
	Clean     int    `db:"clean"      json:"clean"      diff:"update"`
	Distance  int    `db:"distance"   json:"distance"   diff:"update"`
	Template  string `db:"template"   json:"template"   diff:"update"`
	Gender    int    `db:"gender"     json:"gender"`
	GruntType string `db:"grunt_type" json:"grunt_type"  diff:"match"`
}

// SelectInvasionsByIDProfile returns all invasion trackings for a given human and profile.
func SelectInvasionsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]InvasionTrackingAPI, error) {
	var invasions []InvasionTrackingAPI
	err := db.Select(&invasions,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, gender, grunt_type
		 FROM invasion WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select invasions for %s profile %d: %w", id, profileNo, err)
	}
	return invasions, nil
}

// InsertInvasion inserts a single invasion tracking row and returns the new uid.
func InsertInvasion(db *sqlx.DB, inv *InvasionTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO invasion (id, profile_no, ping, clean, distance, template, gender, grunt_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, inv.ProfileNo, inv.Ping, inv.Clean, inv.Distance,
		inv.Template, inv.Gender, inv.GruntType)
	if err != nil {
		return 0, fmt.Errorf("insert invasion: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// NestTrackingAPI represents a nest tracking row for API operations.
type NestTrackingAPI struct {
	UID         int64  `db:"uid"           json:"uid"           diff:"-"`
	ID          string `db:"id"            json:"id"            diff:"-"`
	ProfileNo   int    `db:"profile_no"    json:"profile_no"    diff:"-"`
	Ping        string `db:"ping"          json:"ping"`
	Clean       int    `db:"clean"         json:"clean"         diff:"update"`
	Distance    int    `db:"distance"      json:"distance"      diff:"update"`
	Template    string `db:"template"      json:"template"      diff:"update"`
	PokemonID   int    `db:"pokemon_id"    json:"pokemon_id"    diff:"match"`
	MinSpawnAvg int    `db:"min_spawn_avg" json:"min_spawn_avg"`
	Form        int    `db:"form"          json:"form"          diff:"match"`
}

// SelectNestsByIDProfile returns all nest trackings for a given human and profile.
func SelectNestsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]NestTrackingAPI, error) {
	var nests []NestTrackingAPI
	err := db.Select(&nests,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id,
		        min_spawn_avg, COALESCE(form, 0) AS form
		 FROM nests WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select nests for %s profile %d: %w", id, profileNo, err)
	}
	return nests, nil
}

// InsertNest inserts a single nest tracking row and returns the new uid.
func InsertNest(db *sqlx.DB, nest *NestTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO nests (id, profile_no, ping, clean, distance, template, pokemon_id, min_spawn_avg, form)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nest.ID, nest.ProfileNo, nest.Ping, nest.Clean, nest.Distance,
		nest.Template, nest.PokemonID, nest.MinSpawnAvg, nest.Form)
	if err != nil {
		return 0, fmt.Errorf("insert nest: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// QuestTrackingAPI represents a quest tracking row for API operations.
type QuestTrackingAPI struct {
	UID        int64  `db:"uid"         json:"uid"         diff:"-"`
	ID         string `db:"id"          json:"id"          diff:"-"`
	ProfileNo  int    `db:"profile_no"  json:"profile_no"  diff:"-"`
	Ping       string `db:"ping"        json:"ping"`
	Clean      int    `db:"clean"       json:"clean"       diff:"update"`
	Distance   int    `db:"distance"    json:"distance"    diff:"update"`
	Template   string `db:"template"    json:"template"    diff:"update"`
	RewardType int    `db:"reward_type" json:"reward_type"  diff:"match"`
	Reward     int    `db:"reward"      json:"reward"       diff:"match"`
	Form       int    `db:"form"        json:"form"         diff:"match"`
	Shiny      IntBool `db:"shiny"       json:"shiny"`
	Amount     int    `db:"amount"      json:"amount"`
}

// SelectQuestsByIDProfile returns all quest trackings for a given human and profile.
func SelectQuestsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]QuestTrackingAPI, error) {
	var quests []QuestTrackingAPI
	err := db.Select(&quests,
		`SELECT uid, id, profile_no, ping, clean, reward,
		        COALESCE(template, '') AS template, shiny, reward_type,
		        distance, COALESCE(form, 0) AS form,
		        COALESCE(amount, 0) AS amount
		 FROM quest WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select quests for %s profile %d: %w", id, profileNo, err)
	}
	return quests, nil
}

// InsertQuest inserts a single quest tracking row and returns the new uid.
func InsertQuest(db *sqlx.DB, quest *QuestTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO quest (id, profile_no, ping, clean, distance, template, reward_type, reward, form, shiny, amount)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		quest.ID, quest.ProfileNo, quest.Ping, quest.Clean, quest.Distance,
		quest.Template, quest.RewardType, quest.Reward, quest.Form, quest.Shiny, quest.Amount)
	if err != nil {
		return 0, fmt.Errorf("insert quest: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// MonsterTrackingAPI represents a monster tracking row for API operations.
// Monster uses no diff:"match" — it compares against ALL existing rows.
type MonsterTrackingAPI struct {
	UID             int64  `db:"uid"               json:"uid"               diff:"-"`
	ID              string `db:"id"                json:"id"                diff:"-"`
	ProfileNo       int    `db:"profile_no"        json:"profile_no"        diff:"-"`
	Ping            string `db:"ping"              json:"ping"`
	Clean           int    `db:"clean"             json:"clean"             diff:"update"`
	Distance        int    `db:"distance"          json:"distance"          diff:"update"`
	Template        string `db:"template"          json:"template"          diff:"update"`
	PokemonID       int    `db:"pokemon_id"        json:"pokemon_id"`
	Form            int    `db:"form"              json:"form"`
	MinIV           int    `db:"min_iv"            json:"min_iv"            diff:"update"`
	MaxIV           int    `db:"max_iv"            json:"max_iv"`
	MinCP           int    `db:"min_cp"            json:"min_cp"`
	MaxCP           int    `db:"max_cp"            json:"max_cp"`
	MinLevel        int    `db:"min_level"         json:"min_level"`
	MaxLevel        int    `db:"max_level"         json:"max_level"`
	ATK             int    `db:"atk"               json:"atk"`
	DEF             int    `db:"def"               json:"def"`
	STA             int    `db:"sta"               json:"sta"`
	MaxATK          int    `db:"max_atk"           json:"max_atk"`
	MaxDEF          int    `db:"max_def"           json:"max_def"`
	MaxSTA          int    `db:"max_sta"           json:"max_sta"`
	Gender          int    `db:"gender"            json:"gender"`
	MinWeight       int    `db:"min_weight"        json:"min_weight"`
	MaxWeight       int    `db:"max_weight"        json:"max_weight"`
	MinTime         int    `db:"min_time"          json:"min_time"`
	Rarity          int    `db:"rarity"            json:"rarity"`
	MaxRarity       int    `db:"max_rarity"        json:"max_rarity"`
	Size            int    `db:"size"              json:"size"`
	MaxSize         int    `db:"max_size"          json:"max_size"`
	PVPRankingLeague int   `db:"pvp_ranking_league" json:"pvp_ranking_league"`
	PVPRankingBest   int   `db:"pvp_ranking_best"   json:"pvp_ranking_best"`
	PVPRankingWorst  int   `db:"pvp_ranking_worst"  json:"pvp_ranking_worst"`
	PVPRankingMinCP  int   `db:"pvp_ranking_min_cp" json:"pvp_ranking_min_cp"`
	PVPRankingCap    int   `db:"pvp_ranking_cap"    json:"pvp_ranking_cap"`
}

// SelectMonstersByIDProfile returns all monster trackings for a given human and profile.
func SelectMonstersByIDProfile(db *sqlx.DB, id string, profileNo int) ([]MonsterTrackingAPI, error) {
	var monsters []MonsterTrackingAPI
	err := db.Select(&monsters,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id, form,
		        min_iv, max_iv, min_cp, max_cp, min_level, max_level,
		        atk, def, sta, max_atk, max_def, max_sta,
		        gender, min_weight, max_weight, min_time,
		        rarity, max_rarity, size, max_size,
		        pvp_ranking_league, pvp_ranking_best, pvp_ranking_worst,
		        pvp_ranking_min_cp, pvp_ranking_cap
		 FROM monsters WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select monsters for %s profile %d: %w", id, profileNo, err)
	}
	return monsters, nil
}

// InsertMonster inserts a single monster tracking row and returns the new uid.
func InsertMonster(db *sqlx.DB, m *MonsterTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO monsters (id, profile_no, ping, clean, distance, template,
		        pokemon_id, form, min_iv, max_iv, min_cp, max_cp, min_level, max_level,
		        atk, def, sta, max_atk, max_def, max_sta,
		        gender, min_weight, max_weight, min_time,
		        rarity, max_rarity, size, max_size,
		        pvp_ranking_league, pvp_ranking_best, pvp_ranking_worst,
		        pvp_ranking_min_cp, pvp_ranking_cap)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ProfileNo, m.Ping, m.Clean, m.Distance, m.Template,
		m.PokemonID, m.Form, m.MinIV, m.MaxIV, m.MinCP, m.MaxCP, m.MinLevel, m.MaxLevel,
		m.ATK, m.DEF, m.STA, m.MaxATK, m.MaxDEF, m.MaxSTA,
		m.Gender, m.MinWeight, m.MaxWeight, m.MinTime,
		m.Rarity, m.MaxRarity, m.Size, m.MaxSize,
		m.PVPRankingLeague, m.PVPRankingBest, m.PVPRankingWorst,
		m.PVPRankingMinCP, m.PVPRankingCap)
	if err != nil {
		return 0, fmt.Errorf("insert monster: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// UpdateMonsterByUID updates a monster tracking row by uid.
func UpdateMonsterByUID(db *sqlx.DB, m *MonsterTrackingAPI) error {
	_, err := db.Exec(
		`UPDATE monsters SET ping=?, clean=?, distance=?, template=?,
		        pokemon_id=?, form=?, min_iv=?, max_iv=?, min_cp=?, max_cp=?,
		        min_level=?, max_level=?, atk=?, def=?, sta=?, max_atk=?, max_def=?, max_sta=?,
		        gender=?, min_weight=?, max_weight=?, min_time=?,
		        rarity=?, max_rarity=?, size=?, max_size=?,
		        pvp_ranking_league=?, pvp_ranking_best=?, pvp_ranking_worst=?,
		        pvp_ranking_min_cp=?, pvp_ranking_cap=?
		 WHERE uid = ?`,
		m.Ping, m.Clean, m.Distance, m.Template,
		m.PokemonID, m.Form, m.MinIV, m.MaxIV, m.MinCP, m.MaxCP,
		m.MinLevel, m.MaxLevel, m.ATK, m.DEF, m.STA, m.MaxATK, m.MaxDEF, m.MaxSTA,
		m.Gender, m.MinWeight, m.MaxWeight, m.MinTime,
		m.Rarity, m.MaxRarity, m.Size, m.MaxSize,
		m.PVPRankingLeague, m.PVPRankingBest, m.PVPRankingWorst,
		m.PVPRankingMinCP, m.PVPRankingCap,
		m.UID)
	if err != nil {
		return fmt.Errorf("update monster uid %d: %w", m.UID, err)
	}
	return nil
}

// RaidTrackingAPI represents a raid tracking row for API operations.
type RaidTrackingAPI struct {
	UID         int64          `db:"uid"          json:"uid"          diff:"-"`
	ID          string         `db:"id"           json:"id"           diff:"-"`
	ProfileNo   int            `db:"profile_no"   json:"profile_no"   diff:"-"`
	Ping        string         `db:"ping"         json:"ping"`
	Clean       int            `db:"clean"        json:"clean"        diff:"update"`
	Distance    int            `db:"distance"     json:"distance"     diff:"update"`
	Template    string         `db:"template"     json:"template"     diff:"update"`
	Team        int            `db:"team"         json:"team"         diff:"match"`
	PokemonID   int            `db:"pokemon_id"   json:"pokemon_id"`
	Form        int            `db:"form"         json:"form"`
	Level       int            `db:"level"        json:"level"`
	Exclusive   IntBool        `db:"exclusive"    json:"exclusive"`
	Move        int            `db:"move"         json:"move"`
	Evolution   int            `db:"evolution"    json:"evolution"`
	GymID       null.String `db:"gym_id"       json:"gym_id"`
	RSVPChanges int         `db:"rsvp_changes" json:"rsvp_changes"`
}

// SelectRaidsByIDProfile returns all raid trackings for a given human and profile.
func SelectRaidsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]RaidTrackingAPI, error) {
	var raids []RaidTrackingAPI
	err := db.Select(&raids,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, pokemon_id, form,
		        level, exclusive, move, evolution, gym_id, rsvp_changes
		 FROM raid WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select raids for %s profile %d: %w", id, profileNo, err)
	}
	return raids, nil
}

// InsertRaid inserts a single raid tracking row and returns the new uid.
func InsertRaid(db *sqlx.DB, raid *RaidTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO raid (id, profile_no, ping, clean, distance, template,
		        team, pokemon_id, form, level, exclusive, move, evolution, gym_id, rsvp_changes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		raid.ID, raid.ProfileNo, raid.Ping, raid.Clean, raid.Distance, raid.Template,
		raid.Team, raid.PokemonID, raid.Form, raid.Level, raid.Exclusive,
		raid.Move, raid.Evolution, raid.GymID, raid.RSVPChanges)
	if err != nil {
		return 0, fmt.Errorf("insert raid: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// EggTrackingAPI represents an egg tracking row for API operations.
type EggTrackingAPI struct {
	UID         int64          `db:"uid"          json:"uid"          diff:"-"`
	ID          string         `db:"id"           json:"id"           diff:"-"`
	ProfileNo   int            `db:"profile_no"   json:"profile_no"   diff:"-"`
	Ping        string         `db:"ping"         json:"ping"`
	Clean       int            `db:"clean"        json:"clean"        diff:"update"`
	Distance    int            `db:"distance"     json:"distance"     diff:"update"`
	Template    string         `db:"template"     json:"template"     diff:"update"`
	Team        int            `db:"team"         json:"team"         diff:"match"`
	Level       int            `db:"level"        json:"level"`
	Exclusive   IntBool        `db:"exclusive"    json:"exclusive"`
	GymID       null.String `db:"gym_id"       json:"gym_id"`
	RSVPChanges int         `db:"rsvp_changes" json:"rsvp_changes"`
}

// SelectEggsByIDProfile returns all egg trackings for a given human and profile.
func SelectEggsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]EggTrackingAPI, error) {
	var eggs []EggTrackingAPI
	err := db.Select(&eggs,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, level, exclusive,
		        gym_id, rsvp_changes
		 FROM egg WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select eggs for %s profile %d: %w", id, profileNo, err)
	}
	return eggs, nil
}

// InsertEgg inserts a single egg tracking row and returns the new uid.
func InsertEgg(db *sqlx.DB, egg *EggTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO egg (id, profile_no, ping, clean, distance, template,
		        team, level, exclusive, gym_id, rsvp_changes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		egg.ID, egg.ProfileNo, egg.Ping, egg.Clean, egg.Distance, egg.Template,
		egg.Team, egg.Level, egg.Exclusive, egg.GymID, egg.RSVPChanges)
	if err != nil {
		return 0, fmt.Errorf("insert egg: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// GymTrackingAPI represents a gym tracking row for API operations.
type GymTrackingAPI struct {
	UID           int64   `db:"uid"            json:"uid"            diff:"-"`
	ID            string  `db:"id"             json:"id"             diff:"-"`
	ProfileNo     int     `db:"profile_no"     json:"profile_no"     diff:"-"`
	Ping          string  `db:"ping"           json:"ping"`
	Clean         int     `db:"clean"          json:"clean"          diff:"update"`
	Distance      int     `db:"distance"       json:"distance"       diff:"update"`
	Template      string  `db:"template"       json:"template"       diff:"update"`
	Team          int     `db:"team"           json:"team"           diff:"match"`
	SlotChanges   IntBool `db:"slot_changes"   json:"slot_changes"   diff:"update"`
	BattleChanges IntBool `db:"battle_changes" json:"battle_changes" diff:"update"`
	GymID         *string `db:"gym_id"         json:"gym_id"`
}

// SelectGymsByIDProfile returns all gym trackings for a given human and profile.
func SelectGymsByIDProfile(db *sqlx.DB, id string, profileNo int) ([]GymTrackingAPI, error) {
	var gyms []GymTrackingAPI
	err := db.Select(&gyms,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, slot_changes,
		        gym_id, COALESCE(battle_changes, false) AS battle_changes
		 FROM gym WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select gyms for %s profile %d: %w", id, profileNo, err)
	}
	return gyms, nil
}

// InsertGym inserts a single gym tracking row and returns the new uid.
func InsertGym(db *sqlx.DB, gym *GymTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO gym (id, profile_no, ping, clean, distance, template,
		        team, slot_changes, battle_changes, gym_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gym.ID, gym.ProfileNo, gym.Ping, gym.Clean, gym.Distance, gym.Template,
		gym.Team, gym.SlotChanges, gym.BattleChanges, gym.GymID)
	if err != nil {
		return 0, fmt.Errorf("insert gym: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// MaxbattleTrackingAPI represents a maxbattle tracking row for API operations.
// Maxbattle in JS always inserts (no diff logic), so no diff:"match" tags.
type MaxbattleTrackingAPI struct {
	UID       int64   `db:"uid"        json:"uid"        diff:"-"`
	ID        string  `db:"id"         json:"id"         diff:"-"`
	ProfileNo int     `db:"profile_no" json:"profile_no" diff:"-"`
	Ping      string  `db:"ping"       json:"ping"`
	Clean     int     `db:"clean"      json:"clean"`
	Distance  int     `db:"distance"   json:"distance"`
	Template  string  `db:"template"   json:"template"`
	PokemonID int     `db:"pokemon_id" json:"pokemon_id"`
	Form      int     `db:"form"       json:"form"`
	Level     int     `db:"level"      json:"level"`
	Move      int     `db:"move"       json:"move"`
	Gmax      int     `db:"gmax"       json:"gmax"`
	Evolution int     `db:"evolution"  json:"evolution"`
	StationID *string `db:"station_id" json:"station_id"`
}

// SelectMaxbattlesByIDProfile returns all maxbattle trackings for a given human and profile.
func SelectMaxbattlesByIDProfile(db *sqlx.DB, id string, profileNo int) ([]MaxbattleTrackingAPI, error) {
	var maxbattles []MaxbattleTrackingAPI
	err := db.Select(&maxbattles,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id, form,
		        level, move, gmax, evolution, station_id
		 FROM maxbattle WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return nil, fmt.Errorf("select maxbattles for %s profile %d: %w", id, profileNo, err)
	}
	return maxbattles, nil
}

// InsertMaxbattle inserts a single maxbattle tracking row and returns the new uid.
func InsertMaxbattle(db *sqlx.DB, mb *MaxbattleTrackingAPI) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO maxbattle (id, profile_no, ping, clean, distance, template,
		        pokemon_id, form, level, move, gmax, evolution, station_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mb.ID, mb.ProfileNo, mb.Ping, mb.Clean, mb.Distance, mb.Template,
		mb.PokemonID, mb.Form, mb.Level, mb.Move, mb.Gmax, mb.Evolution, mb.StationID)
	if err != nil {
		return 0, fmt.Errorf("insert maxbattle: %w", err)
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return uid, nil
}

// ---------------------------------------------------------------------------
// Select*ByID — query by user ID only (all profiles, no profile_no filter)
// ---------------------------------------------------------------------------

// SelectMonstersByID returns all monster trackings for a given human (all profiles).
func SelectMonstersByID(db *sqlx.DB, id string) ([]MonsterTrackingAPI, error) {
	var monsters []MonsterTrackingAPI
	err := db.Select(&monsters,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id, form,
		        min_iv, max_iv, min_cp, max_cp, min_level, max_level,
		        atk, def, sta, max_atk, max_def, max_sta,
		        gender, min_weight, max_weight, min_time,
		        rarity, max_rarity, size, max_size,
		        pvp_ranking_league, pvp_ranking_best, pvp_ranking_worst,
		        pvp_ranking_min_cp, pvp_ranking_cap
		 FROM monsters WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select monsters for %s: %w", id, err)
	}
	return monsters, nil
}

// SelectRaidsByID returns all raid trackings for a given human (all profiles).
func SelectRaidsByID(db *sqlx.DB, id string) ([]RaidTrackingAPI, error) {
	var raids []RaidTrackingAPI
	err := db.Select(&raids,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, pokemon_id, form,
		        level, exclusive, move, evolution, gym_id, rsvp_changes
		 FROM raid WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select raids for %s: %w", id, err)
	}
	return raids, nil
}

// SelectEggsByID returns all egg trackings for a given human (all profiles).
func SelectEggsByID(db *sqlx.DB, id string) ([]EggTrackingAPI, error) {
	var eggs []EggTrackingAPI
	err := db.Select(&eggs,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, level, exclusive,
		        gym_id, rsvp_changes
		 FROM egg WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select eggs for %s: %w", id, err)
	}
	return eggs, nil
}

// SelectQuestsByID returns all quest trackings for a given human (all profiles).
func SelectQuestsByID(db *sqlx.DB, id string) ([]QuestTrackingAPI, error) {
	var quests []QuestTrackingAPI
	err := db.Select(&quests,
		`SELECT uid, id, profile_no, ping, clean, reward,
		        COALESCE(template, '') AS template, shiny, reward_type,
		        distance, COALESCE(form, 0) AS form,
		        COALESCE(amount, 0) AS amount
		 FROM quest WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select quests for %s: %w", id, err)
	}
	return quests, nil
}

// SelectInvasionsByID returns all invasion trackings for a given human (all profiles).
func SelectInvasionsByID(db *sqlx.DB, id string) ([]InvasionTrackingAPI, error) {
	var invasions []InvasionTrackingAPI
	err := db.Select(&invasions,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, gender, grunt_type
		 FROM invasion WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select invasions for %s: %w", id, err)
	}
	return invasions, nil
}

// SelectLuresByID returns all lure trackings for a given human (all profiles).
func SelectLuresByID(db *sqlx.DB, id string) ([]LureTrackingAPI, error) {
	var lures []LureTrackingAPI
	err := db.Select(&lures,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, lure_id
		 FROM lures WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select lures for %s: %w", id, err)
	}
	return lures, nil
}

// SelectNestsByID returns all nest trackings for a given human (all profiles).
func SelectNestsByID(db *sqlx.DB, id string) ([]NestTrackingAPI, error) {
	var nests []NestTrackingAPI
	err := db.Select(&nests,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id,
		        min_spawn_avg, COALESCE(form, 0) AS form
		 FROM nests WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select nests for %s: %w", id, err)
	}
	return nests, nil
}

// SelectGymsByID returns all gym trackings for a given human (all profiles).
func SelectGymsByID(db *sqlx.DB, id string) ([]GymTrackingAPI, error) {
	var gyms []GymTrackingAPI
	err := db.Select(&gyms,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, team, slot_changes,
		        gym_id, COALESCE(battle_changes, false) AS battle_changes
		 FROM gym WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select gyms for %s: %w", id, err)
	}
	return gyms, nil
}

// SelectMaxbattlesByID returns all maxbattle trackings for a given human (all profiles).
func SelectMaxbattlesByID(db *sqlx.DB, id string) ([]MaxbattleTrackingAPI, error) {
	var maxbattles []MaxbattleTrackingAPI
	err := db.Select(&maxbattles,
		`SELECT uid, id, profile_no, ping, clean, distance,
		        COALESCE(template, '') AS template, pokemon_id, form,
		        level, move, gmax, evolution, station_id
		 FROM maxbattle WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select maxbattles for %s: %w", id, err)
	}
	return maxbattles, nil
}

// SelectFortsByID returns all fort trackings for a given human (all profiles).
func SelectFortsByID(db *sqlx.DB, id string) ([]FortTrackingAPI, error) {
	var forts []FortTrackingAPI
	err := db.Select(&forts,
		`SELECT uid, id, profile_no, ping, distance,
		        COALESCE(template, '') AS template,
		        COALESCE(fort_type, 'everything') AS fort_type,
		        COALESCE(include_empty, true) AS include_empty,
		        COALESCE(change_types, '[]') AS change_types
		 FROM forts WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select forts for %s: %w", id, err)
	}
	return forts, nil
}
