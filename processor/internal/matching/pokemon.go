package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Default max values for tracking rule filters. These match the defaults
// set by the !track command when no max filter is specified.
const (
	defaultMaxCP     = 9000
	defaultMaxLevel  = 55
	defaultMaxStat   = 15 // ATK, DEF, STA
	defaultMaxWeight = 9000000
)

// ProcessedPokemon holds computed fields for matching.
type ProcessedPokemon struct {
	PokemonID   int
	Form        int
	IV          float64 // -1 if not encountered
	CP          int
	Level       int
	ATK         int
	DEF         int
	STA         int
	Gender      int
	Weight      float64
	Size        int
	RarityGroup int
	TTHSeconds  float64
	Latitude    float64
	Longitude   float64
	Encountered bool
	PVPBestRank map[int][]pvp.LeagueRank
	PVPEvoData  map[int]map[int][]pvp.LeagueRank
}

// ProcessPokemonWebhook converts a raw webhook into ProcessedPokemon with computed fields.
func ProcessPokemonWebhook(pokemon *webhook.PokemonWebhook, rarityGroup int, pvpCfg *pvp.Config) *ProcessedPokemon {
	encountered := pokemon.IndividualAttack != nil && pokemon.IndividualDefense != nil && pokemon.IndividualStamina != nil

	var iv float64
	var atk, def, sta, cp, level int
	var weight float64

	if encountered {
		atk = *pokemon.IndividualAttack
		def = *pokemon.IndividualDefense
		sta = *pokemon.IndividualStamina
		iv = float64(atk+def+sta) / 0.45 // sum of IVs (0-45) to percentage (0-100)
		cp = pokemon.CP
		level = pokemon.PokemonLevel
		weight = pokemon.Weight
	} else {
		iv = -1
	}

	form := pokemon.Form
	size := pokemon.Size
	gender := pokemon.Gender

	tthSeconds := float64(pokemon.DisappearTime) - float64(time.Now().Unix())

	pvpResult := pvp.Calculate(pokemon, pvpCfg)

	return &ProcessedPokemon{
		PokemonID:   pokemon.PokemonID,
		Form:        form,
		IV:          iv,
		CP:          cp,
		Level:       level,
		ATK:         atk,
		DEF:         def,
		STA:         sta,
		Gender:      gender,
		Weight:      weight,
		Size:        size,
		RarityGroup: rarityGroup,
		TTHSeconds:  tthSeconds,
		Latitude:    pokemon.Latitude,
		Longitude:   pokemon.Longitude,
		Encountered: encountered,
		PVPBestRank: pvpResult.BestRank,
		PVPEvoData:  pvpResult.EvolutionData,
	}
}

// PokemonMatcher performs in-memory pokemon matching.
// Direct port of monsterAlarmMatch.js.
type PokemonMatcher struct {
	PVPQueryMaxRank            int
	PVPEvolutionDirectTracking bool
	StrictLocations            bool
	AreaSecurityEnabled        bool
}

// Match returns all matched users for a pokemon.
func (m *PokemonMatcher) Match(pokemon *ProcessedPokemon, st *state.State) []webhook.MatchedUser {
	if st == nil || st.Monsters == nil {
		return nil
	}

	var matched []*db.MonsterTracking

	// Basic Pokemon - everything (pokemon_id=0 catch-all)
	matched = append(matched, m.matchMonsters(pokemon, st.Monsters.ByPokemonID[0], pokemon.PokemonID, pokemon.Form, true, 0, pvp.LeagueRank{})...)

	// Basic Pokemon - by pokemon_id
	matched = append(matched, m.matchMonsters(pokemon, st.Monsters.ByPokemonID[pokemon.PokemonID], pokemon.PokemonID, pokemon.Form, true, 0, pvp.LeagueRank{})...)

	// PVP Pokemon
	for league, leagueDataArr := range pokemon.PVPBestRank {
		for _, leagueData := range leagueDataArr {
			if leagueData.Rank <= m.PVPQueryMaxRank {
				matched = append(matched, m.matchMonsters(pokemon, st.Monsters.PVPEverything[league], pokemon.PokemonID, pokemon.Form, true, league, leagueData)...)
				matched = append(matched, m.matchMonsters(pokemon, st.Monsters.PVPSpecific[league], pokemon.PokemonID, pokemon.Form, true, league, leagueData)...)
			}
		}
	}

	// PVP Evolution mon
	if m.PVPEvolutionDirectTracking && len(pokemon.PVPEvoData) > 0 {
		for pokemonID, pvpMon := range pokemon.PVPEvoData {
			for league, leagueDataArr := range pvpMon {
				candidates := st.Monsters.PVPSpecific[league]
				for _, leagueData := range leagueDataArr {
					if leagueData.Rank <= m.PVPQueryMaxRank {
						evoMatched := m.matchMonsters(pokemon, candidates, pokemonID, 0, false, league, leagueData)
						matched = append(matched, evoMatched...)
					}
				}
			}
		}
	}

	// Validate humans
	matchedAreaNames := st.Geofence.MatchedAreaNames(pokemon.Latitude, pokemon.Longitude)
	return ValidateHumans(
		matched,
		pokemon.Latitude, pokemon.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
	)
}

// matchMonsters filters monster trackings against pokemon data.
// Port of monsterAlarmMatch.js:143-188.
func (m *PokemonMatcher) matchMonsters(
	data *ProcessedPokemon,
	monsters []*db.MonsterTracking,
	targetPokemonID int,
	targetForm int,
	includeEverything bool,
	league int,
	leagueData pvp.LeagueRank,
) []*db.MonsterTracking {
	if len(monsters) == 0 {
		return nil
	}

	var results []*db.MonsterTracking
	for _, monster := range monsters {
		// Pokemon ID check
		if !(monster.PokemonID == targetPokemonID || (includeEverything && monster.PokemonID == 0)) {
			continue
		}
		// Form check (0 = any)
		if monster.Form != 0 && monster.Form != data.Form {
			continue
		}
		// PVP league filters
		if league != 0 {
			if leagueData.Rank > monster.PVPRankingWorst {
				continue
			}
			if leagueData.Rank < monster.PVPRankingBest {
				continue
			}
			if leagueData.CP < monster.PVPRankingMinCP {
				continue
			}
			if monster.PVPRankingCap != 0 && len(leagueData.Caps) > 0 && !pvp.CapsContain(leagueData.Caps, monster.PVPRankingCap) {
				continue
			}
		}

		// IV range (IV is -1 for unencountered, min_iv defaults to -1)
		if data.IV < float64(monster.MinIV) {
			continue
		}
		if data.IV > float64(monster.MaxIV) {
			continue
		}
		// Min time
		if data.TTHSeconds < float64(monster.MinTime) {
			continue
		}

		// Gender (0 = any) — available for both encountered and unencountered
		if monster.Gender != 0 && monster.Gender != data.Gender {
			continue
		}

		// Encounter-only stat filters: CP, level, individual IVs, weight.
		// For unencountered pokemon these values are unknown (Golbat omits them).
		// If the tracking rule constrains any of these beyond their defaults,
		// skip the match — the user cares about a stat we don't have data for.
		if !data.Encountered {
			if monster.MinCP > 0 || monster.MaxCP < defaultMaxCP ||
				monster.MinLevel > 0 || monster.MaxLevel < defaultMaxLevel ||
				monster.ATK > 0 || monster.MaxATK < defaultMaxStat ||
				monster.DEF > 0 || monster.MaxDEF < defaultMaxStat ||
				monster.STA > 0 || monster.MaxSTA < defaultMaxStat ||
				monster.MinWeight > 0 || monster.MaxWeight < defaultMaxWeight {
				continue
			}
		} else {
			// CP range
			if data.CP < monster.MinCP {
				continue
			}
			if data.CP > monster.MaxCP {
				continue
			}
			// Level range
			if data.Level < monster.MinLevel {
				continue
			}
			if data.Level > monster.MaxLevel {
				continue
			}
			// IV stat ranges
			if data.ATK < monster.ATK {
				continue
			}
			if data.DEF < monster.DEF {
				continue
			}
			if data.STA < monster.STA {
				continue
			}
			if data.ATK > monster.MaxATK {
				continue
			}
			if data.DEF > monster.MaxDEF {
				continue
			}
			if data.STA > monster.MaxSTA {
				continue
			}
			// Weight (stored as weight * 1000)
			weight := int(data.Weight * 1000)
			if weight < monster.MinWeight {
				continue
			}
			if weight > monster.MaxWeight {
				continue
			}
		}
		// Rarity
		if data.RarityGroup < monster.Rarity {
			continue
		}
		if data.RarityGroup > monster.MaxRarity {
			continue
		}
		// Size
		if data.Size < monster.Size {
			continue
		}
		if data.Size > monster.MaxSize {
			continue
		}

		results = append(results, monster)
	}
	return results
}
