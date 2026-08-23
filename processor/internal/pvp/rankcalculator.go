package pvp

import (
	"slices"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// LeagueRank represents the best rank info for a league, for one evolution slot.
type LeagueRank struct {
	Rank      int   `json:"rank"`
	CP        int   `json:"cp"`
	Caps      []int `json:"caps,omitempty"`
	Form      int   `json:"form,omitempty"`
	Evolution int   `json:"evolution,omitempty"` // 0=base, 1=Mega, 2=Mega X, 3=Mega Y
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
			leagueMap[leagueCP] = append(leagueMap[leagueCP], entries...)
		}
	}

	// From legacy fields
	if pokemon.PVPRankingsGreatLeague != nil {
		leagueMap[1500] = append(leagueMap[1500], pokemon.PVPRankingsGreatLeague...)
	}
	if pokemon.PVPRankingsUltraLeague != nil {
		leagueMap[2500] = append(leagueMap[2500], pokemon.PVPRankingsUltraLeague...)
	}
	if pokemon.PVPRankingsLittleLeague != nil {
		leagueMap[500] = append(leagueMap[500], pokemon.PVPRankingsLittleLeague...)
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
	best := make(map[int]map[int]*capBest) // evolution -> cap -> best
	ensure := func(evo int) map[int]*capBest {
		if best[evo] == nil {
			m := make(map[int]*capBest, len(capsConsidered))
			for _, c := range capsConsidered {
				m[c] = &capBest{rank: 4096, cp: 0}
			}
			best[evo] = m
		}
		return best[evo]
	}

	for _, stats := range leagueData {
		var caps []int
		if stats.Cap == 0 && !stats.Capped {
			// Golbat non-ohbem format (cap=0, not capped) — treat as cap 50
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

		capMap := ensure(stats.Evolution)
		for _, cap := range caps {
			b, ok := capMap[cap]
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

		// Cross-species evolution direct tracking. Each qualifying entry for a
		// DIFFERENT pokemon (stats.Pokemon != pokemonID) is recorded under that
		// evolved species so a rule on it can fire on this pre-evolution. Mega /
		// temporary-evolution entries are INCLUDED, tagged with their Evolution,
		// so a `mega` rule on the evolved species (e.g. track Mega Charizard, get
		// Charmander alerts) matches via the matcher's evolution discriminator;
		// base rules still only see base (evolution 0) entries unless the server
		// include_mega_evolution default is on.
		if cfg.PVPEvolutionDirectTracking && stats.Rank > 0 && stats.CP > 0 &&
			stats.Pokemon != pokemonID && stats.Rank <= cfg.PVPFilterMaxRank && stats.CP >= minCP {
			var evoCaps []int
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
			evoRank := LeagueRank{Rank: stats.Rank, CP: stats.CP, Caps: evoCaps, Form: stats.Form, Evolution: stats.Evolution}
			if _, ok := evoData[stats.Pokemon]; !ok {
				evoData[stats.Pokemon] = make(map[int][]LeagueRank)
			}
			evoData[stats.Pokemon][league] = append(evoData[stats.Pokemon][league], evoRank)
		}
	}

	var bestRanks []LeagueRank
	for evo, capMap := range best {
		var evoRanks []LeagueRank
		for cap, details := range capMap {
			if details.rank >= 4096 {
				continue
			}
			merged := false
			for i := range evoRanks {
				if evoRanks[i].CP == details.cp && evoRanks[i].Rank == details.rank {
					evoRanks[i].Caps = append(evoRanks[i].Caps, cap)
					merged = true
					break
				}
			}
			if !merged {
				evoRanks = append(evoRanks, LeagueRank{Rank: details.rank, CP: details.cp, Caps: []int{cap}, Evolution: evo})
			}
		}
		bestRanks = append(bestRanks, evoRanks...)
	}
	return bestRanks
}

// CapsContain checks if the caps list contains a specific cap value.
func CapsContain(caps []int, target int) bool {
	return slices.Contains(caps, target)
}
