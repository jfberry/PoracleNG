package nlp

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// contextSynonyms maps natural language phrases to Poracle filter tokens.
// The outer key is the command intent ("track", "raid", "quest", etc.),
// and "*" matches any intent. Empty string value means "consume but produce no filter".
type contextSynonyms map[string]map[string]string

var synonyms = contextSynonyms{
	"track": {
		"shiny":    "", // consumed but not filterable for track
		"shinies":  "",
		"hundo":    "iv100",
		"hundos":   "iv100",
		"hundred":  "iv100",
		"perfect":  "iv100",
		"100%":     "iv100",
		"nundo":    "iv0 maxiv0",
		"nundos":   "iv0 maxiv0",
		"zero":     "iv0 maxiv0",
		"0%":       "iv0 maxiv0",
		"good ivs": "iv80",
		"good iv":  "iv80",
		"shadow":   "form:shadow",
		"tiny":     "size:xxs",
		"xxs":      "size:xxs",
		"small":    "size:xs",
		"xs":       "size:xs",
		"big":      "size:xl",
		"large":    "size:xl",
		"xl":       "size:xl",
		"huge":     "size:xxl",
		"xxl":      "size:xxl",
		"male":     "male",
		"female":   "female",
		"gmax":     "gmax",
	},
	"raid": {
		"shadow":         "level15",
		"legendary":      "level5",
		"mega":           "level6",
		"mega legendary": "level7",
		"primal":         "level10",
		"ultra beast":    "level8",
		"ex":             "ex",
		"exclusive":      "ex",
	},
	"quest": {
		"shiny":       "shiny",
		"shinies":     "shiny",
		"stardust":    "stardust",
		"energy":      "energy",
		"mega energy": "energy",
		"candy":       "candy",
	},
	"maxbattle": {
		"gmax": "gmax",
	},
	"fort": {
		"new": "new",
	},
	"lure": {
		// Lure types — the six known modules. Users who type the Poracle
		// name ("glacial") hit the passthrough regex; this context adds
		// natural-language aliases ("ice lure", "rock lure", etc.) and
		// disambiguates unambiguous singletons ("glacial" on its own in
		// a lure-intent message) into type:<name> syntax the tracking
		// command expects.
		"normal":    "type:normal",
		"regular":   "type:normal",
		"glacial":   "type:glacial",
		"ice":       "type:glacial",
		"mossy":     "type:mossy",
		"grass":     "type:mossy",
		"plant":     "type:mossy",
		"magnetic": "type:magnetic",
		"magnet":    "type:magnetic",
		"rock":      "type:magnetic",
		"rainy":     "type:rainy",
		"rain":      "type:rainy",
		"sparkly":   "type:sparkly",
		"sparkle":   "type:sparkly",
		"shiny":     "type:sparkly",
	},
	"*": {
		"nearby":      "d1000",
		"near me":     "d1000",
		"close":       "d1000",
		"everything":  "everything",
		"all":         "everything",
		// Quality-of-life flags — synonyms for clean/edit/ping that survive
		// through assemble as raw Poracle filter tokens. Single-word
		// synonyms only: anything multi-word containing "me" / "with" /
		// "to" / etc. is stripped by Normalize. Anything multi-word
		// containing a remove keyword ("delete", "remove") trips the
		// remove-intent detector — so "auto delete" is out, "autodelete"
		// stays because the intent detector now matches whole tokens only.
		"autodelete":  "clean",
		"auto-delete": "clean",
		"autoclean":   "clean",
		"auto-clean":  "clean",
		"cleanup":     "clean",
		"editable":    "edit",
		"notify":      "ping",
		"pinged":      "ping",
	},
}

// teamNames maps team aliases to canonical Poracle team names.
var teamNames = map[string]string{
	"mystic":   "mystic",
	"blue":     "mystic",
	"valor":    "valor",
	"red":      "valor",
	"instinct": "instinct",
	"yellow":   "instinct",
	"harmony":  "harmony",
	"gray":     "harmony",
}

// poracleFilterRe matches tokens already in Poracle filter syntax.
var poracleFilterRe = regexp.MustCompile(
	`^(?:` +
		`iv\d+(?:-\d+)?` +
		`|cp\d+(?:-\d+)?` +
		`|level\d+(?:-\d+)?` +
		`|d\d+` +
		`|gen\d+` +
		`|atk\d+|def\d+|sta\d+` +
		`|maxiv\d+|maxcp\d+|maxlevel\d+|maxatk\d+|maxdef\d+|maxsta\d+` +
		`|great\d+|ultra\d+|little\d+` +
		`|weight\d+|maxweight\d+` +
		`|rarity:\w+|maxrarity:\w+` +
		`|template:\d+` +
		`|clean|edit|ping` +
		`|individually` +
		`|type:\w+` +
		`|size:\w+` +
		`|form:\w+` +
		`|move:\S+` +
		`|male|female` +
		`|gmax` +
		`|new` +
		`|ex` +
		`|shiny` +
		`|stardust|energy|candy` +
		`)$`,
)

// distanceRe matches "1km", "2.5km", "500m", "1.5 km".
var distanceRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(km|m)$`)

// pvpLeagues maps league names/abbreviations to Poracle league prefix.
var pvpLeagues = map[string]string{
	"great league":  "great",
	"gl":            "great",
	"ultra league":  "ultra",
	"ul":            "ultra",
	"little league": "little",
	"ll":            "little",
}

// bareLeagueWords are the single-token forms that should trigger a league
// match when a user drops the word "league" in a multi-league list
// ("great and ultra league top 10" — "and" is a noise word, so after
// normalize we see ["great", "ultra", "league"]; we want both leagues).
// Matched in matchPVP only; outside PVP context these are ordinary tokens.
var bareLeagueWords = map[string]string{
	"great":  "great",
	"ultra":  "ultra",
	"little": "little",
}

// statWords are words that combine with a following number into a filter.
// "level 5" → "level5", "iv 95" → "iv95", etc.
var statWords = map[string]bool{
	"level": true, "iv": true, "cp": true,
	"atk": true, "def": true, "sta": true,
}

// betweenPattern matches "between X Y stat" in token sequences
// (note: "and" is a noise word and gets stripped by normalize).
var betweenPattern = regexp.MustCompile(`between\s+(\d+)\s+(\d+)\s+(iv|cp|level|atk|def|sta)`)

// FilterResult holds the outcome of applying filter matching to a token sequence.
type FilterResult struct {
	Filters    []string
	Everything bool
	Consumed   map[int]bool // indices of consumed tokens
}

// matchFilters processes tokens and extracts filter expressions.
// It handles context-aware synonyms, distance parsing, PVP, "between X and Y",
// team names, stat+number combining, and Poracle pass-through syntax.
func matchFilters(tokens []string, intent string) *FilterResult {
	result := &FilterResult{
		Consumed: make(map[int]bool),
	}

	joined := strings.Join(tokens, " ")

	// --- "between X Y stat" pattern (before other matching) ---
	if m := betweenPattern.FindStringSubmatch(joined); m != nil {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		stat := m[3]
		if lo > hi {
			lo, hi = hi, lo
		}
		result.Filters = append(result.Filters, fmt.Sprintf("%s%d-%d", stat, lo, hi))
		for i, t := range tokens {
			if t == "between" || t == m[1] || t == m[2] || t == m[3] {
				result.Consumed[i] = true
			}
		}
		return result
	}

	// --- Multi-word synonym matching (longest first) ---
	for _, ctx := range []string{intent, "*"} {
		synMap, ok := synonyms[ctx]
		if !ok {
			continue
		}
		// Collect and sort multi-word phrases longest first
		var multiPhrases []string
		for phrase := range synMap {
			if strings.Contains(phrase, " ") {
				multiPhrases = append(multiPhrases, phrase)
			}
		}
		sortByLengthDesc(multiPhrases)

		for _, phrase := range multiPhrases {
			// Skip if all tokens of this phrase are already consumed
			if allTokensConsumed(tokens, phrase, result) {
				continue
			}
			if found := strings.Contains(joined, phrase); found {
				markMultiWordConsumed(tokens, phrase, result)
				replacement := synMap[phrase]
				if replacement == "everything" {
					result.Everything = true
				} else if replacement != "" {
					result.Filters = append(result.Filters, strings.Fields(replacement)...)
				}
			}
		}
	}

	// --- PVP matching (multi-token) ---
	matchPVP(tokens, intent, result)

	// --- Stat word + number combining: "level 5" → "level5" ---
	for i := 0; i < len(tokens)-1; i++ {
		if result.Consumed[i] || result.Consumed[i+1] {
			continue
		}
		if statWords[tokens[i]] {
			if _, err := strconv.Atoi(tokens[i+1]); err == nil {
				combined := tokens[i] + tokens[i+1]
				if poracleFilterRe.MatchString(combined) {
					result.Filters = append(result.Filters, combined)
					result.Consumed[i] = true
					result.Consumed[i+1] = true
				}
			}
		}
	}

	// --- Distance parsing (multi-token: "1.5 km") ---
	for i := 0; i < len(tokens)-1; i++ {
		if result.Consumed[i] || result.Consumed[i+1] {
			continue
		}
		combined := tokens[i] + tokens[i+1]
		if d := parseDistance(combined); d != "" {
			result.Filters = append(result.Filters, d)
			result.Consumed[i] = true
			result.Consumed[i+1] = true
		}
	}

	// --- Single-token matching ---
	for i, t := range tokens {
		if result.Consumed[i] {
			continue
		}

		// Distance with unit attached: "1km", "500m"
		if d := parseDistance(t); d != "" {
			result.Filters = append(result.Filters, d)
			result.Consumed[i] = true
			continue
		}

		// Context-specific single-word synonyms FIRST (before passthrough).
		// This ensures e.g. "shiny" in track context is consumed as empty
		// rather than passed through as a filter.
		if hasSynonym(t, intent) {
			syn := lookupSynonym(t, intent)
			if syn == "everything" {
				result.Everything = true
			} else if syn != "" {
				result.Filters = append(result.Filters, strings.Fields(syn)...)
			}
			result.Consumed[i] = true
			continue
		}

		// Wildcard single-word synonyms
		if hasSynonym(t, "*") {
			syn := lookupSynonym(t, "*")
			if syn == "everything" {
				result.Everything = true
			} else if syn != "" {
				result.Filters = append(result.Filters, strings.Fields(syn)...)
			}
			result.Consumed[i] = true
			continue
		}

		// Poracle pass-through syntax
		if poracleFilterRe.MatchString(t) {
			if t == "everything" {
				result.Everything = true
			} else {
				result.Filters = append(result.Filters, t)
			}
			result.Consumed[i] = true
			continue
		}

		// Team names (all contexts)
		if team, ok := teamNames[t]; ok {
			result.Filters = append(result.Filters, team)
			result.Consumed[i] = true
			continue
		}
	}

	return result
}

// hasSynonym checks if a token exists as a key in the synonym map for the given context.
func hasSynonym(token, ctx string) bool {
	synMap, ok := synonyms[ctx]
	if !ok {
		return false
	}
	_, exists := synMap[token]
	return exists
}

// lookupSynonym returns the replacement for a synonym, or "".
func lookupSynonym(token, ctx string) string {
	synMap, ok := synonyms[ctx]
	if !ok {
		return ""
	}
	return synMap[token]
}

// parseDistance converts distance expressions to Poracle "d{meters}" format.
func parseDistance(s string) string {
	m := distanceRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return ""
	}
	unit := m[2]
	if unit == "km" {
		val *= 1000
	}
	meters := int(math.Round(val))
	return fmt.Sprintf("d%d", meters)
}

// matchPVP handles PVP-related token sequences.
//
// Collects every league mention in the input rather than bailing after the
// first match, so "pikachu great and ultra top 10" produces both great10
// and ultra10 filters — matching the bot's own multi-league support
// (!track pikachu great5 ultra10). Rank applies to all detected leagues.
func matchPVP(tokens []string, intent string, result *FilterResult) {
	if intent != "track" {
		return
	}

	// Collect all league mentions. Multi-word phrases first (longest match
	// wins implicitly thanks to the token consumption below preventing the
	// single-word pass from re-matching "ultra" inside "ultra league").
	leagues := make([]string, 0, 3)
	seen := map[string]bool{}
	addLeague := func(lg string) {
		if !seen[lg] {
			seen[lg] = true
			leagues = append(leagues, lg)
		}
	}

	// Multi-word phrases, e.g. "great league", "ultra league" — but we
	// can't rely on these alone because users often drop the word
	// "league" from all-but-the-last entry ("great and ultra league").
	// Match every occurrence by marking consumed tokens as we go and
	// re-joining before each scan.
	for phrase, lg := range pvpLeagues {
		if !strings.Contains(phrase, " ") {
			continue
		}
		for {
			if allTokensConsumed(tokens, phrase, result) {
				break
			}
			if !strings.Contains(currentJoined(tokens, result), phrase) {
				break
			}
			addLeague(lg)
			markMultiWordConsumed(tokens, phrase, result)
		}
	}
	// Single-word league abbreviations ("gl", "ul", "ll") and bare league
	// words ("great", "ultra", "little") — the latter only count as a
	// league mention in PVP context, so the map is kept separate.
	for i, t := range tokens {
		if result.Consumed[i] {
			continue
		}
		if lg, ok := pvpLeagues[t]; ok {
			addLeague(lg)
			result.Consumed[i] = true
			continue
		}
		if lg, ok := bareLeagueWords[t]; ok {
			addLeague(lg)
			result.Consumed[i] = true
		}
	}

	// Detect rank/top N (single value, applied to every league)
	rank := 0
	for i, t := range tokens {
		if result.Consumed[i] {
			continue
		}
		if (t == "rank" || t == "top") && i+1 < len(tokens) {
			if n, err := strconv.Atoi(tokens[i+1]); err == nil && n > 0 {
				rank = n
				result.Consumed[i] = true
				result.Consumed[i+1] = true
				break
			}
		}
	}

	// Detect bare "pvp" or "good pvp"
	hasPVPWord := false
	for i, t := range tokens {
		if result.Consumed[i] {
			continue
		}
		if t == "pvp" {
			hasPVPWord = true
			result.Consumed[i] = true
			// Also consume "good" before pvp
			if i > 0 && tokens[i-1] == "good" && !result.Consumed[i-1] {
				result.Consumed[i-1] = true
			}
			break
		}
	}

	// Build PVP filters — one per league mentioned, or default to great if
	// the user said "pvp"/"top 10" without naming a league.
	if len(leagues) == 0 && (rank > 0 || hasPVPWord) {
		addLeague("great")
	}
	if rank == 0 {
		rank = 5
	}
	// Emit in canonical league order (great → ultra → little) so the
	// Poracle command reads predictably regardless of the order the user
	// mentioned them.
	for _, canonical := range []string{"great", "ultra", "little"} {
		if seen[canonical] {
			result.Filters = append(result.Filters, fmt.Sprintf("%s%d", canonical, rank))
		}
	}
}

// sortByLengthDesc sorts strings by length descending.
func sortByLengthDesc(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && len(ss[j]) > len(ss[j-1]); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// currentJoined joins the tokens that remain unconsumed into a single
// string for substring matching. Used by matchPVP to re-check for
// multi-word league phrases after each match so multiple occurrences get
// picked up.
func currentJoined(tokens []string, result *FilterResult) string {
	var parts []string
	for i, t := range tokens {
		if result.Consumed[i] {
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// allTokensConsumed checks if all tokens that would match a phrase are already consumed.
func allTokensConsumed(tokens []string, phrase string, result *FilterResult) bool {
	words := strings.FieldsSeq(phrase)
	for w := range words {
		foundUnconsumed := false
		for i, t := range tokens {
			if t == w && !result.Consumed[i] {
				foundUnconsumed = true
				break
			}
		}
		if !foundUnconsumed {
			return true
		}
	}
	return false
}

// markMultiWordConsumed marks token indices that form a multi-word phrase.
func markMultiWordConsumed(tokens []string, phrase string, result *FilterResult) {
	words := strings.Fields(phrase)
	joined := strings.Join(tokens, " ")
	idx := strings.Index(joined, phrase)
	if idx < 0 {
		return
	}

	pos := 0
	for i, t := range tokens {
		end := pos + len(t)
		if pos >= idx && end <= idx+len(phrase) {
			if slices.Contains(words, t) {
				result.Consumed[i] = true
			}
		}
		pos = end + 1
	}
}
