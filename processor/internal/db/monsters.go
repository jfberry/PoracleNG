package db

import "github.com/jmoiron/sqlx"

// MonsterTracking represents a row from the monsters table.
type MonsterTracking struct {
	ID               string `db:"id"`
	ProfileNo        int    `db:"profile_no"`
	PokemonID        int    `db:"pokemon_id"`
	Form             int    `db:"form"`
	Distance         int    `db:"distance"`
	MinIV            int    `db:"min_iv"`
	MaxIV            int    `db:"max_iv"`
	MinCP            int    `db:"min_cp"`
	MaxCP            int    `db:"max_cp"`
	MinLevel         int    `db:"min_level"`
	MaxLevel         int    `db:"max_level"`
	ATK              int    `db:"atk"`
	DEF              int    `db:"def"`
	STA              int    `db:"sta"`
	MaxATK           int    `db:"max_atk"`
	MaxDEF           int    `db:"max_def"`
	MaxSTA           int    `db:"max_sta"`
	Gender           int    `db:"gender"`
	MinWeight        int    `db:"min_weight"`
	MaxWeight        int    `db:"max_weight"`
	MinTime          int    `db:"min_time"`
	Rarity           int    `db:"rarity"`
	MaxRarity        int    `db:"max_rarity"`
	Size             int    `db:"size"`
	MaxSize          int    `db:"max_size"`
	Template         string `db:"template"`
	Clean            bool   `db:"clean"`
	Ping             string `db:"ping"`
	PVPRankingLeague int    `db:"pvp_ranking_league"`
	PVPRankingBest   int    `db:"pvp_ranking_best"`
	PVPRankingWorst  int    `db:"pvp_ranking_worst"`
	PVPRankingMinCP  int    `db:"pvp_ranking_min_cp"`
	PVPRankingCap    int    `db:"pvp_ranking_cap"`
}

// MonsterIndex holds indexed monster trackings for fast lookup.
type MonsterIndex struct {
	ByPokemonID   map[int][]*MonsterTracking // keyed by pokemon_id (0 = catch-all)
	PVPSpecific   map[int][]*MonsterTracking // keyed by pvp_ranking_league, pokemon_id != 0
	PVPEverything map[int][]*MonsterTracking // keyed by pvp_ranking_league, pokemon_id == 0
	Total         int                        // total number of monster tracking rules
}

// LoadMonsters loads all monster trackings and builds indexed structures.
func LoadMonsters(db *sqlx.DB) (*MonsterIndex, error) {
	var monsters []MonsterTracking
	err := db.Select(&monsters,
		`SELECT id, profile_no, pokemon_id, form, distance,
		        min_iv, max_iv, min_cp, max_cp, min_level, max_level,
		        atk, def, sta, max_atk, max_def, max_sta,
		        gender, min_weight, max_weight, min_time,
		        rarity, max_rarity, size, max_size,
		        COALESCE(template, '') AS template, clean, ping,
		        pvp_ranking_league, pvp_ranking_best, pvp_ranking_worst,
		        pvp_ranking_min_cp, pvp_ranking_cap
		 FROM monsters`)
	if err != nil {
		return nil, err
	}

	idx := &MonsterIndex{
		ByPokemonID:   make(map[int][]*MonsterTracking),
		PVPSpecific:   make(map[int][]*MonsterTracking),
		PVPEverything: make(map[int][]*MonsterTracking),
	}
	for _, league := range []int{500, 1500, 2500} {
		idx.PVPSpecific[league] = nil
		idx.PVPEverything[league] = nil
	}

	for i := range monsters {
		m := &monsters[i]
		if m.PVPRankingLeague != 0 {
			if m.PokemonID != 0 {
				idx.PVPSpecific[m.PVPRankingLeague] = append(idx.PVPSpecific[m.PVPRankingLeague], m)
			} else {
				idx.PVPEverything[m.PVPRankingLeague] = append(idx.PVPEverything[m.PVPRankingLeague], m)
			}
		} else {
			idx.ByPokemonID[m.PokemonID] = append(idx.ByPokemonID[m.PokemonID], m)
		}
	}
	idx.Total = len(monsters)
	return idx, nil
}
