package enrichment

import (
	"maps"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// pvpFilter holds a user's PVP tracking filter parameters.
type pvpFilter struct {
	League int
	Worst  int
	Cap    int
}

// consolidatedUser merges a MatchedUser with their PVP filter entries.
type consolidatedUser struct {
	webhook.MatchedUser
	Filters []pvpFilter
}

// PokemonPerUser computes per-user enrichment for a pokemon webhook.
// It includes the user's language reference, filtered PVP display data,
// and user-specific fields (distance, bearing).
// The alerter merges per_user_enrichment[userId] into the per-user view.
func (e *Enricher) PokemonPerUser(
	langEnrichments map[string]map[string]any,
	matchedUsers []webhook.MatchedUser,
) map[string]map[string]any {
	if e.PVPDisplay == nil {
		return nil
	}

	// Consolidate users by ID (merge PVP filters, same as alerter's consolidatedAlerts)
	consolidated := consolidateUsers(matchedUsers)

	result := make(map[string]map[string]any, len(consolidated))
	for _, cu := range consolidated {
		m := make(map[string]any, 16)

		lang := cu.Language
		if lang == "" {
			lang = e.DefaultLocale
		}
		if lang == "" {
			lang = "en"
		}
		m["_lang"] = lang

		// PVP user ranking (0 if 4096, same as alerter)
		if cu.PVPRankingWorst == 4096 {
			m["pvpUserRanking"] = 0
		} else {
			m["pvpUserRanking"] = cu.PVPRankingWorst
		}
		m["userHasPvpTracks"] = len(cu.Filters) > 0

		// Distance and bearing
		m["distance"] = cu.Distance
		m["bearing"] = cu.Bearing
		m["bearingEmojiKey"] = cu.CardinalDirection

		// PVP display config values (for template access)
		m["pvpDisplayMaxRank"] = e.PVPDisplay.MaxRank
		m["pvpDisplayGreatMinCP"] = e.PVPDisplay.GreatMinCP
		m["pvpDisplayUltraMinCP"] = e.PVPDisplay.UltraMinCP
		m["pvpDisplayLittleMinCP"] = e.PVPDisplay.LittleMinCP

		// PVP display per user — filter pre-enriched data from language enrichment
		langEnrich := langEnrichments[lang]
		if langEnrich != nil {
			pvpGreat := e.createPvpDisplay(1500, langEnrich["pvpEnriched_great_league"], e.PVPDisplay.GreatMinCP, cu.Filters)
			pvpUltra := e.createPvpDisplay(2500, langEnrich["pvpEnriched_ultra_league"], e.PVPDisplay.UltraMinCP, cu.Filters)
			pvpLittle := e.createPvpDisplay(500, langEnrich["pvpEnriched_little_league"], e.PVPDisplay.LittleMinCP, cu.Filters)

			m["pvpGreat"] = pvpGreat
			m["pvpGreatBest"] = calculateBestInfo(pvpGreat)
			m["pvpUltra"] = pvpUltra
			m["pvpUltraBest"] = calculateBestInfo(pvpUltra)
			m["pvpLittle"] = pvpLittle
			m["pvpLittleBest"] = calculateBestInfo(pvpLittle)
			m["pvpAvailable"] = pvpGreat != nil || pvpUltra != nil || pvpLittle != nil
		}

		result[cu.ID] = m
	}

	return result
}

// consolidateUsers groups matched users by ID and merges their PVP filters.
//
// A filter is only recorded when the matched rule is actually a PVP rule, i.e.
// has a non-zero pvp_ranking_league AND a meaningful pvp_ranking_worst
// (between 1 and 4095). The legacy JS check was just `worst < 4096`, which
// relied on non-PVP rules storing 4096 in the DB. In PoracleNG the tracking
// INSERT passes the Go struct's zero value for unset fields, so non-PVP rules
// persist with worst=0 and would otherwise be mistaken for real PVP filters,
// making userHasPvpTracks universally true and polluting per-user PVP display.
func consolidateUsers(matchedUsers []webhook.MatchedUser) []consolidatedUser {
	seen := make(map[string]int, len(matchedUsers))
	var consolidated []consolidatedUser

	for _, u := range matchedUsers {
		idx, exists := seen[u.ID]
		if !exists {
			idx = len(consolidated)
			seen[u.ID] = idx
			consolidated = append(consolidated, consolidatedUser{MatchedUser: u})
		}
		if u.PVPRankingLeague > 0 && u.PVPRankingWorst > 0 && u.PVPRankingWorst < 4096 {
			consolidated[idx].Filters = append(consolidated[idx].Filters, pvpFilter{
				League: u.PVPRankingLeague,
				Worst:  u.PVPRankingWorst,
				Cap:    u.PVPRankingCap,
			})
		}
	}

	return consolidated
}

// createPvpDisplay filters pre-enriched PVP rank entries for a specific user,
// applying the display max rank, min CP, and user's PVP tracking filters.
// Returns nil if no entries pass the filters.
func (e *Enricher) createPvpDisplay(leagueCap int, rawEntries any, minCP int, filters []pvpFilter) []map[string]any {
	entries, ok := rawEntries.([]map[string]any)
	if !ok || len(entries) == 0 {
		return nil
	}

	maxRank := e.PVPDisplay.MaxRank
	var displayList []map[string]any

	for _, rank := range entries {
		rankVal := toInt(rank["rank"])
		cpVal := toInt(rank["cp"])

		if rankVal > maxRank || cpVal < minCP {
			continue
		}

		displayRank := make(map[string]any, len(rank)+3)
		// Copy all pre-enriched fields
		maps.Copy(displayRank, rank)

		// Ensure numeric fields are properly typed
		displayRank["rank"] = rankVal
		displayRank["cp"] = cpVal
		displayRank["pokemonId"] = toInt(rank["pokemon"])

		capVal := toInt(rank["cap"])
		capped, _ := rank["capped"].(bool)

		// User filter matching
		matchesUserTrack := false
		passesFilter := false

		if len(filters) > 0 {
			for _, f := range filters {
				if (f.League == leagueCap || f.League == 0) &&
					(f.Cap == 0 || f.Cap == capVal || capped) &&
					(f.Worst >= rankVal) {
					passesFilter = true
					matchesUserTrack = true
				}
			}
		} else {
			passesFilter = true
		}

		displayRank["matchesUserTrack"] = matchesUserTrack
		displayRank["passesFilter"] = passesFilter

		if !e.PVPDisplay.FilterByTrack || passesFilter {
			displayList = append(displayList, displayRank)
		}
	}

	if len(displayList) == 0 {
		return nil
	}
	return displayList
}

// calculateBestInfo finds the best (lowest) rank among display entries and
// returns a summary with rank, list, and name.
func calculateBestInfo(ranks []map[string]any) map[string]any {
	if len(ranks) == 0 {
		return nil
	}

	bestRank := 4096
	var bestList []map[string]any

	for _, r := range ranks {
		rank := toInt(r["rank"])
		if rank == bestRank {
			bestList = append(bestList, r)
		} else if rank < bestRank {
			bestRank = rank
			bestList = []map[string]any{r}
		}
	}

	// Build unique name list
	nameSet := make(map[string]bool)
	var names []string
	for _, r := range bestList {
		name, _ := r["fullName"].(string)
		if name != "" && !nameSet[name] {
			nameSet[name] = true
			names = append(names, name)
		}
	}

	return map[string]any{
		"rank": bestRank,
		"list": bestList,
		"name": strings.Join(names, ", "),
	}
}

// toInt converts an any value to int, handling common numeric types.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
