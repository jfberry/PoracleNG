package pvp

import (
	"slices"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// LeagueRank represents the best rank info for a league.
type LeagueRank struct {
	Rank int   `json:"rank"`
	CP   int   `json:"cp"`
	Caps []int `json:"caps,omitempty"`
	Form int   `json:"form,omitempty"`
}

// PVPResult holds processed PVP data for a pokemon.
type PVPResult struct {
	BestRank      map[int][]LeagueRank         // league -> best rank entries
	EvolutionData map[int]map[int][]LeagueRank // pokemon_id -> league -> rank entries
}

// Config holds PVP configuration.
type Config struct {
	LevelCaps                  []int
	PVPFilterMaxRank           int
	PVPEvolutionDirectTracking bool
	IncludeMegaEvolution       bool
	PVPFilterGreatMinCP        int
	PVPFilterUltraMinCP        int
	PVPFilterLittleMinCP       int
}

// Calculate processes PVP data from Golbat webhook into PVPResult.
// Port of monster.js:310-390 rankCalculator function.
func Calculate(pokemon *webhook.PokemonWebhook, cfg *Config) *PVPResult {
	result := &PVPResult{
		BestRank:      make(map[int][]LeagueRank),
		EvolutionData: make(map[int]map[int][]LeagueRank),
	}

	capsConsidered := cfg.LevelCaps
	if len(capsConsidered) == 0 {
		capsConsidered = []int{50}
	}

	// Collect league data from webhook
	leagueMap := make(map[int][]webhook.PVPRankEntry)

	// From new pvp field (Chuck / new RDM)
	if pokemon.PVP != nil {
		for leagueName, entries := range pokemon.PVP {
			var leagueCP int
			switch leagueName {
			case "little":
				leagueCP = 500
			case "great":
				leagueCP = 1500
			case "ultra":
				leagueCP = 2500
			default:
				continue
			}
			filtered := filterMega(entries, cfg.IncludeMegaEvolution)
			leagueMap[leagueCP] = append(leagueMap[leagueCP], filtered...)
		}
	}

	// From legacy fields
	if pokemon.PVPRankingsGreatLeague != nil {
		filtered := filterMega(pokemon.PVPRankingsGreatLeague, cfg.IncludeMegaEvolution)
		leagueMap[1500] = append(leagueMap[1500], filtered...)
	}
	if pokemon.PVPRankingsUltraLeague != nil {
		filtered := filterMega(pokemon.PVPRankingsUltraLeague, cfg.IncludeMegaEvolution)
		leagueMap[2500] = append(leagueMap[2500], filtered...)
	}
	if pokemon.PVPRankingsLittleLeague != nil {
		filtered := filterMega(pokemon.PVPRankingsLittleLeague, cfg.IncludeMegaEvolution)
		leagueMap[500] = append(leagueMap[500], filtered...)
	}

	minCPMap := map[int]int{
		500:  cfg.PVPFilterLittleMinCP,
		1500: cfg.PVPFilterGreatMinCP,
		2500: cfg.PVPFilterUltraMinCP,
	}

	for league, leagueData := range leagueMap {
		minCP := minCPMap[league]
		result.BestRank[league] = calculateLeague(league, leagueData, capsConsidered, pokemon.PokemonID, cfg, minCP, result.EvolutionData)
	}

	return result
}

func calculateLeague(league int, leagueData []webhook.PVPRankEntry, capsConsidered []int, pokemonID int, cfg *Config, minCP int, evoData map[int]map[int][]LeagueRank) []LeagueRank {
	type capBest struct {
		rank int
		cp   int
	}
	best := make(map[int]*capBest)
	for _, cap := range capsConsidered {
		best[cap] = &capBest{rank: 4096, cp: 0}
	}

	for _, stats := range leagueData {
		var caps []int
		if stats.Cap == 0 && !stats.Capped {
			// Not ohbem
			caps = append(caps, 50)
		} else if stats.Capped {
			for _, c := range capsConsidered {
				if c >= stats.Cap {
					caps = append(caps, c)
				}
			}
		} else {
			caps = append(caps, stats.Cap)
		}

		for _, cap := range caps {
			b, ok := best[cap]
			if !ok {
				continue
			}
			if stats.Rank > 0 && stats.Rank < b.rank {
				b.rank = stats.Rank
				b.cp = stats.CP
			} else if stats.Rank > 0 && stats.CP > 0 && stats.Rank == b.rank && stats.CP > b.cp {
				b.cp = stats.CP
			}
		}

		// Evolution tracking
		if stats.Evolution == 0 && cfg.PVPEvolutionDirectTracking && stats.Rank > 0 && stats.CP > 0 &&
			stats.Pokemon != pokemonID && stats.Rank <= cfg.PVPFilterMaxRank && stats.CP >= minCP {
			var evoCaps []int
			// Cap assignment mirrors JS: capped → all caps >= cap, explicit cap → [cap],
			// neither (not ohbem) → nil (matches any cap in matcher, same as JS null)
			if stats.Capped {
				for _, c := range capsConsidered {
					if c >= stats.Cap {
						evoCaps = append(evoCaps, c)
					}
				}
			} else if stats.Cap > 0 {
				for _, c := range capsConsidered {
					if c == stats.Cap {
						evoCaps = append(evoCaps, c)
					}
				}
			}

			evoRank := LeagueRank{
				Rank: stats.Rank,
				CP:   stats.CP,
				Caps: evoCaps,
				Form: stats.Form,
			}

			if _, ok := evoData[stats.Pokemon]; !ok {
				evoData[stats.Pokemon] = make(map[int][]LeagueRank)
			}
			evoData[stats.Pokemon][league] = append(evoData[stats.Pokemon][league], evoRank)
		}
	}

	// Consolidate best ranks (skip sentinel 4096 entries — no matching PVP data for that cap)
	var bestRanks []LeagueRank
	for cap, details := range best {
		if details.rank >= 4096 {
			continue
		}
		found := false
		for i := range bestRanks {
			if bestRanks[i].CP == details.cp && bestRanks[i].Rank == details.rank {
				bestRanks[i].Caps = append(bestRanks[i].Caps, cap)
				found = true
				break
			}
		}
		if !found {
			bestRanks = append(bestRanks, LeagueRank{
				Rank: details.rank,
				CP:   details.cp,
				Caps: []int{cap},
			})
		}
	}

	return bestRanks
}

func filterMega(entries []webhook.PVPRankEntry, includeMega bool) []webhook.PVPRankEntry {
	if includeMega {
		return entries
	}
	var filtered []webhook.PVPRankEntry
	for _, e := range entries {
		if e.Evolution == 0 {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// CapsContain checks if the caps list contains a specific cap value.
func CapsContain(caps []int, target int) bool {
	return slices.Contains(caps, target)
}
