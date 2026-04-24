package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ArgMatcher matches command argument tokens against declared parameter types
// using the user's language + English fallback.
type ArgMatcher struct {
	bundle   *i18n.Bundle
	gameData *gamedata.GameData
	resolver *PokemonResolver

	// Pre-built lookup tables (built once at construction)
	teamMap      map[string]map[string]int    // lang → name → team ID
	genderMap    map[string]map[string]int    // lang → name → gender ID
	lureMap      map[string]map[string]int    // lang → name → lure ID
	typeMap      map[string]map[string]int    // lang → name → type ID
	raidLevelMap map[string]map[string][]int  // lang → name → level IDs
	prefixMap    map[string][]string          // (lang + "\x00" + key) → prefix strings
	keywordMap   map[string]map[string]string // lang → lowercase_translated → original key

	// Multi-word vocabularies — lets the parser collapse "razz berry"
	// into a single token before per-param matching runs, so users don't
	// need to type underscores or quotes for known multi-word names.
	// See collapseMultiWord.
	bareMultiWord      map[string]bool            // items + pokemon names (multi-word entries only)
	prefixedMultiWord  map[string]map[string]bool // "move" → multi-word move names, "form" → form names
}

// NewArgMatcher builds the pre-computed lookup tables for argument matching.
func NewArgMatcher(bundle *i18n.Bundle, gd *gamedata.GameData, resolver *PokemonResolver, languages []string) *ArgMatcher {
	am := &ArgMatcher{
		bundle:       bundle,
		gameData:     gd,
		resolver:     resolver,
		teamMap:      make(map[string]map[string]int),
		genderMap:    make(map[string]map[string]int),
		lureMap:      make(map[string]map[string]int),
		typeMap:      make(map[string]map[string]int),
		raidLevelMap: make(map[string]map[string][]int),
		prefixMap:    make(map[string][]string),
		keywordMap:   make(map[string]map[string]string),
	}

	for _, lang := range languages {
		tr := bundle.For(lang)
		if tr == nil {
			continue
		}

		// Team names: translated team names + color aliases
		teams := make(map[string]int)
		teams[strings.ToLower(tr.T("arg.valor"))] = 2
		teams[strings.ToLower(tr.T("arg.mystic"))] = 1
		teams[strings.ToLower(tr.T("arg.instinct"))] = 3
		teams[strings.ToLower(tr.T("arg.harmony"))] = 0
		teams[strings.ToLower(tr.T("arg.red"))] = 2
		teams[strings.ToLower(tr.T("arg.blue"))] = 1
		teams[strings.ToLower(tr.T("arg.yellow"))] = 3
		teams[strings.ToLower(tr.T("arg.gray"))] = 0
		am.teamMap[lang] = teams

		// Gender keywords
		genders := make(map[string]int)
		genders[strings.ToLower(tr.T("arg.male"))] = 1
		genders[strings.ToLower(tr.T("arg.female"))] = 2
		genders[strings.ToLower(tr.T("arg.genderless"))] = 3
		am.genderMap[lang] = genders

		// Lure types — accept pogo-translations keys lure_501..lure_506
		// (resources/gamelocale/) in addition to the legacy arg.* keys.
		// The lure_N values are added last so they take precedence over
		// arg.normal if they ever collide in the user's language.
		lures := make(map[string]int)
		lures[strings.ToLower(tr.T("arg.normal"))] = 0
		lures[strings.ToLower(tr.T("arg.glacial"))] = 502
		lures[strings.ToLower(tr.T("arg.mossy"))] = 503
		lures[strings.ToLower(tr.T("arg.magnetic"))] = 504
		lures[strings.ToLower(tr.T("arg.rainy"))] = 505
		lures[strings.ToLower(tr.T("arg.sparkly"))] = 506
		for lureID := 501; lureID <= 506; lureID++ {
			key := fmt.Sprintf("lure_%d", lureID)
			if name := strings.ToLower(tr.T(key)); name != "" && name != key {
				lures[name] = lureID
			}
		}
		am.lureMap[lang] = lures

		// Type names from poke_type_{id} translation keys
		types := make(map[string]int)
		for id := 1; id <= 18; id++ {
			key := gamedata.TypeTranslationKey(id)
			name := tr.T(key)
			if name != key {
				types[strings.ToLower(name)] = id
			}
		}
		am.typeMap[lang] = types

		// Raid level names from raid.level.* translation keys + util.json
		levels := make(map[string][]int)
		// From translation keys
		levelNames := map[string][]int{
			strings.ToLower(tr.T("raid.level.legendary")):        {5},
			strings.ToLower(tr.T("raid.level.mega")):              {6},
			strings.ToLower(tr.T("raid.level.mega_legendary")):    {7},
			strings.ToLower(tr.T("raid.level.ultra_beast")):       {8},
			strings.ToLower(tr.T("raid.level.elite")):             {9},
			strings.ToLower(tr.T("raid.level.primal")):            {10},
			strings.ToLower(tr.T("raid.level.shadow_legendary")):  {15},
		}
		// "shadow" without qualifier matches all shadow levels
		shadowLevels := []int{}
		if gd != nil && gd.Util != nil {
			for lvl := range gd.Util.RaidLevels {
				if lvl >= 11 && lvl <= 15 {
					shadowLevels = append(shadowLevels, lvl)
				}
			}
		}
		if len(shadowLevels) == 0 {
			shadowLevels = []int{11, 12, 13, 14, 15}
		}
		levelNames[strings.ToLower(tr.T("raid.level.shadow"))] = shadowLevels

		for name, lvls := range levelNames {
			if name != "" && name != "raid.level." {
				levels[name] = lvls
			}
		}
		am.raidLevelMap[lang] = levels

		// Pre-compute keyword translations for this language.
		kw := make(map[string]string)
		for _, key := range knownKeywordKeys {
			val := strings.ToLower(tr.T(key))
			if val != key && val != "" {
				kw[val] = key
			}
		}
		am.keywordMap[lang] = kw
	}

	// Pre-compute prefix translations for all (lang, key) pairs.
	for _, lang := range languages {
		for _, key := range knownPrefixKeys {
			cacheKey := lang + "\x00" + key
			am.prefixMap[cacheKey] = am.resolvePrefix(key, lang)
		}
	}

	am.buildMultiWordVocabularies(languages)

	return am
}

// buildMultiWordVocabularies collects every multi-word phrase the argument
// parser might want to match as a single token. Built once at startup;
// collapseMultiWord scans are O(tokens × window) against this set.
func (am *ArgMatcher) buildMultiWordVocabularies(languages []string) {
	am.bareMultiWord = make(map[string]bool)
	am.prefixedMultiWord = map[string]map[string]bool{
		"move": {},
		"form": {},
	}

	if am.gameData != nil {
		// Form IDs are scattered across Monsters; collect once.
		formIDs := make(map[int]bool, len(am.gameData.Monsters))
		for key := range am.gameData.Monsters {
			if key.Form > 0 {
				formIDs[key.Form] = true
			}
		}

		add := func(dest map[string]bool, tr *i18n.Translator, key string) {
			name := strings.ToLower(tr.T(key))
			if name != "" && strings.Contains(name, " ") {
				dest[name] = true
			}
		}
		for _, lang := range languages {
			tr := am.bundle.For(lang)
			if tr == nil {
				continue
			}
			for id := range am.gameData.Items {
				add(am.bareMultiWord, tr, gamedata.ItemTranslationKey(id))
			}
			for id := range am.gameData.Moves {
				add(am.prefixedMultiWord["move"], tr, gamedata.MoveTranslationKey(id))
			}
			for id := range formIDs {
				add(am.prefixedMultiWord["form"], tr, gamedata.FormTranslationKey(id))
			}
		}
	}
	// Pokemon names with spaces OR with the de-punctuated variants the
	// resolver registers (so "mr mime" collapses alongside "mr. mime").
	if am.resolver != nil {
		for _, langMap := range am.resolver.nameToIDs {
			for name := range langMap {
				if strings.Contains(name, " ") {
					am.bareMultiWord[name] = true
				}
			}
		}
	}
}

// collapseMultiWord greedily joins token sequences that form a known
// multi-word phrase into a single token. Greedy-longest at each
// position. Tokens starting with "move:" / "form:" consult the
// prefix-scoped vocabulary; everything else consults the bare set.
// Window capped at 4 — covers every known phrase.
func (am *ArgMatcher) collapseMultiWord(tokens []string) []string {
	if len(tokens) < 2 {
		return tokens
	}
	const maxWindow = 4

	out := make([]string, 0, len(tokens))
	i := 0
	for i < len(tokens) {
		matched := false
		// Detect a prefix like "move:" / "form:" on the first token —
		// the remainder is the start of a potential multi-word value
		// that we want to consume into the same prefixed token.
		prefix, remainder := splitParamPrefix(tokens[i], am.prefixedMultiWord)
		for window := maxWindow; window >= 2; window-- {
			if i+window > len(tokens) {
				continue
			}
			if prefix != "" {
				joined := remainder
				for j := 1; j < window; j++ {
					joined += " " + tokens[i+j]
				}
				if am.prefixedMultiWord[prefix][joined] {
					out = append(out, prefix+":"+joined)
					i += window
					matched = true
					break
				}
			} else {
				joined := tokens[i]
				for j := 1; j < window; j++ {
					joined += " " + tokens[i+j]
				}
				if am.bareMultiWord[joined] {
					out = append(out, joined)
					i += window
					matched = true
					break
				}
			}
		}
		if !matched {
			out = append(out, tokens[i])
			i++
		}
	}
	return out
}

// splitParamPrefix returns (prefix, remainder) if tok looks like
// "<knownPrefix>:<rest>" for any prefix present in prefixedMultiWord,
// otherwise ("", ""). "move:hyper" → ("move", "hyper"). We only match
// the colon form so users who typed "move:ice beam" get multi-word
// lookahead; single-word "moveice" (no colon) still parses as before.
func splitParamPrefix(tok string, prefixedMultiWord map[string]map[string]bool) (string, string) {
	colon := strings.IndexByte(tok, ':')
	if colon <= 0 {
		return "", ""
	}
	prefix := tok[:colon]
	if _, ok := prefixedMultiWord[prefix]; !ok {
		return "", ""
	}
	return prefix, tok[colon+1:]
}

// knownPrefixKeys lists all arg.prefix.* keys used by any command.
var knownPrefixKeys = []string{
	"arg.prefix.iv", "arg.prefix.miniv", "arg.prefix.maxiv",
	"arg.prefix.cp", "arg.prefix.mincp", "arg.prefix.maxcp",
	"arg.prefix.level", "arg.prefix.maxlevel",
	"arg.prefix.atk", "arg.prefix.maxatk",
	"arg.prefix.def", "arg.prefix.maxdef",
	"arg.prefix.sta", "arg.prefix.maxsta",
	"arg.prefix.weight", "arg.prefix.maxweight",
	"arg.prefix.rarity", "arg.prefix.maxrarity",
	"arg.prefix.size", "arg.prefix.maxsize",
	"arg.prefix.d", "arg.prefix.t", "arg.prefix.gen", "arg.prefix.cap",
	"arg.prefix.form", "arg.prefix.template", "arg.prefix.move", "arg.prefix.language",
	"arg.prefix.stardust", "arg.prefix.energy", "arg.prefix.candy",
	"arg.prefix.minspawn",
	"arg.prefix.great", "arg.prefix.greathigh", "arg.prefix.greatcp",
	"arg.prefix.ultra", "arg.prefix.ultrahigh", "arg.prefix.ultracp",
	"arg.prefix.little", "arg.prefix.littlehigh", "arg.prefix.littlecp",
}

// knownKeywordKeys lists all arg.* keyword keys used by any command.
var knownKeywordKeys = []string{
	"arg.remove", "arg.everything", "arg.individually",
	"arg.clean", "arg.edit", "arg.shiny", "arg.ex",
	"arg.rsvp", "arg.no_rsvp", "arg.rsvp_only",
	"arg.gmax",
	"arg.pokestop", "arg.gym", "arg.location", "arg.new", "arg.removal", "arg.photo", "arg.include_empty",
	"arg.stardust", "arg.energy", "arg.candy",
	"arg.slot_changes", "arg.battle_changes",
}

// Match walks through tokens and tries each declared ParamDef in priority order.
// Uses lang (user's language) + "en" (English fallback).
// Consumed tokens are removed from consideration. Unmatched tokens go to Unrecognized.
func (am *ArgMatcher) Match(tokens []string, params []ParamDef, lang string) *ParsedArgs {
	// Collapse known multi-word phrases first so "!quest razz berry"
	// or "!raid move:hyper beam" reach the per-param matchers as single
	// tokens the same way "razz_berry" / "move:hyper_beam" would.
	tokens = am.collapseMultiWord(tokens)

	result := NewParsedArgs()
	consumed := make([]bool, len(tokens))

	// Sort params by priority: prefix patterns first, then keywords, then names last
	// We process in this order to avoid ambiguity
	for _, priority := range matchPriorities {
		for _, param := range params {
			if param.Type != priority {
				continue
			}
			for i, tok := range tokens {
				if consumed[i] {
					continue
				}
				if am.tryMatch(tok, param, lang, result) {
					consumed[i] = true
					break // each param definition matches at most one token (except Pokemon/Type which collect)
				}
			}
		}
	}

	// For collecting types: match ALL unmatched tokens, not just one
	for _, param := range params {
		if param.Type == ParamRemoveUID {
			for i, tok := range tokens {
				if consumed[i] {
					continue
				}
				if tryRemoveUID(tok, result) {
					consumed[i] = true
				}
			}
		}
		if param.Type == ParamPokemonName {
			for i, tok := range tokens {
				if consumed[i] {
					continue
				}
				if am.tryMatchPokemon(tok, lang, result) {
					consumed[i] = true
				}
			}
		}
		if param.Type == ParamTypeName {
			for i, tok := range tokens {
				if consumed[i] {
					continue
				}
				if am.tryMatchType(tok, lang, result) {
					consumed[i] = true
				}
			}
		}
	}

	// Collect unrecognized
	for i, tok := range tokens {
		if !consumed[i] {
			result.Unrecognized = append(result.Unrecognized, tok)
		}
	}

	return result
}

// matchPriorities defines the order in which param types are tried.
// Structurally unambiguous types (prefix patterns) first, then exact-match
// keywords, then game data lookups, then pokemon names last.
var matchPriorities = []ParamType{
	ParamPrefixRange,
	ParamPrefixSingle,
	ParamPrefixString,
	ParamLatLon,
	ParamPVPLeague,
	ParamRaidLevelName,
	ParamKeyword,
	ParamTeam,
	ParamGender,
	ParamLureType,
	// ParamTypeName and ParamPokemonName handled separately (collect all matches)
}

func (am *ArgMatcher) tryMatch(tok string, param ParamDef, lang string, result *ParsedArgs) bool {
	switch param.Type {
	case ParamPrefixRange:
		return am.tryPrefixRange(tok, param.Key, lang, result)
	case ParamPrefixSingle:
		return am.tryPrefixSingle(tok, param.Key, lang, result)
	case ParamPrefixString:
		return am.tryPrefixString(tok, param.Key, lang, result)
	case ParamKeyword:
		return am.tryKeyword(tok, param.Key, lang, result)
	case ParamTeam:
		return am.tryTeam(tok, lang, result)
	case ParamGender:
		return am.tryGender(tok, lang, result)
	case ParamLureType:
		return am.tryLureType(tok, lang, result)
	case ParamRaidLevelName:
		return am.tryRaidLevelName(tok, lang, result)
	case ParamPVPLeague:
		return am.tryPVPLeague(tok, param.Key, lang, result)
	case ParamLatLon:
		return am.tryLatLon(tok, result)
	}
	return false
}

// stripPrefix tries to strip a prefix from a token, supporting both
// direct concatenation (iv100) and colon separator (iv:100).
// Returns the value after the prefix and true, or ("", false) if no match.
func stripPrefix(tok, prefix string) (string, bool) {
	// Try with colon first (iv:100)
	if strings.HasPrefix(tok, prefix+":") {
		val := tok[len(prefix)+1:]
		if val != "" {
			return val, true
		}
	}
	// Try direct concatenation (iv100)
	if strings.HasPrefix(tok, prefix) {
		val := tok[len(prefix):]
		if val != "" {
			return val, true
		}
	}
	return "", false
}

// cachedPrefix returns pre-computed prefixes if available, falling back to resolvePrefix.
func (am *ArgMatcher) cachedPrefix(key, lang string) []string {
	if prefixes, ok := am.prefixMap[lang+"\x00"+key]; ok {
		return prefixes
	}
	return am.resolvePrefix(key, lang)
}

// tryPrefixRange matches patterns like "iv100", "iv:100", "iv50-100", "iv:50-100".
// The prefix is translated (e.g. German "wp" for "cp").
func (am *ArgMatcher) tryPrefixRange(tok, key, lang string, result *ParsedArgs) bool {
	prefixes := am.cachedPrefix(key, lang)
	for _, p := range prefixes {
		val, ok := stripPrefix(tok, p)
		if !ok {
			continue
		}
		parts := strings.SplitN(val, "-", 2)
		min, err := strconv.Atoi(parts[0])
		if err != nil {
			// Try resolving as a name (e.g. "xxl" → 5 for size, "rare" → 3 for rarity)
			if resolved, ok := am.resolveRangeName(key, parts[0], lang); ok {
				min = resolved
			} else {
				continue
			}
		}
		shortKey := strings.TrimPrefix(key, "arg.prefix.")
		if len(parts) == 2 {
			max, err := strconv.Atoi(parts[1])
			if err != nil {
				if resolved, ok := am.resolveRangeName(key, parts[1], lang); ok {
					max = resolved
				} else {
					continue
				}
			}
			result.Ranges[shortKey] = Range{Min: min, Max: max, HasMax: true}
		} else {
			// Single value: only min is set, max uses the field's default
			result.Ranges[shortKey] = Range{Min: min, HasMax: false}
		}
		return true
	}
	return false
}

// resolveRangeName resolves a name like "xxl" or "rare" to its numeric ID
// for size and rarity prefix range parameters. Checks the user's language
// translations first, then English, then the raw game data names.
func (am *ArgMatcher) resolveRangeName(key, name, lang string) (int, bool) {
	var keyPrefix string
	var maxID int
	switch key {
	case "arg.prefix.size", "arg.prefix.maxsize":
		keyPrefix = "size_"
		maxID = 5
	case "arg.prefix.rarity", "arg.prefix.maxrarity":
		keyPrefix = "rarity_"
		maxID = 6
	default:
		return 0, false
	}

	lower := strings.ToLower(name)

	// Try user language, then English. size_N / rarity_N are shipped in the
	// embedded i18n files (processor/internal/i18n/locale/) for every
	// supported language, so no util.json fallback is needed.
	if am.bundle != nil {
		for _, tryLang := range []string{lang, "en"} {
			if tryLang == "" {
				continue
			}
			tr := am.bundle.For(tryLang)
			if tr == nil {
				continue
			}
			for id := 1; id <= maxID; id++ {
				translated := tr.T(keyPrefix + strconv.Itoa(id))
				if translated != "" && strings.ToLower(translated) == lower {
					return id, true
				}
			}
		}
	}
	return 0, false
}

// tryPrefixSingle matches patterns like "d500", "d:500", "t60", "gen3".
func (am *ArgMatcher) tryPrefixSingle(tok, key, lang string, result *ParsedArgs) bool {
	prefixes := am.cachedPrefix(key, lang)
	for _, p := range prefixes {
		val, ok := stripPrefix(tok, p)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			// Try float for distance (d500.5)
			if f, ferr := strconv.ParseFloat(val, 64); ferr == nil {
				n = int(f)
			} else if resolved, ok := am.resolveRangeName(key, val, lang); ok {
				// Try name resolution (e.g. maxsize:xxl, maxrarity:rare)
				n = resolved
			} else {
				continue
			}
		}
		shortKey := strings.TrimPrefix(key, "arg.prefix.")
		result.Singles[shortKey] = n
		return true
	}
	return false
}

// tryPrefixString matches patterns like "form:alola" or "formalola", "template:2", "move:hydro pump".
func (am *ArgMatcher) tryPrefixString(tok, key, lang string, result *ParsedArgs) bool {
	prefix := am.cachedPrefix(key, lang)
	for _, p := range prefix {
		if val, ok := stripPrefix(tok, p); ok {
			shortKey := strings.TrimPrefix(key, "arg.prefix.")
			result.Strings[shortKey] = val
			return true
		}
	}
	return false
}

// tryKeyword matches exact keywords like "remove", "everything", "clean".
func (am *ArgMatcher) tryKeyword(tok, key, lang string, result *ParsedArgs) bool {
	// Check pre-computed keyword map for user's language.
	if kw, ok := am.keywordMap[lang]; ok {
		if matchedKey, found := kw[tok]; found && matchedKey == key {
			result.Keywords[key] = true
			return true
		}
	}
	// English fallback.
	if lang != "en" {
		if kw, ok := am.keywordMap["en"]; ok {
			if matchedKey, found := kw[tok]; found && matchedKey == key {
				result.Keywords[key] = true
				return true
			}
		}
	}
	return false
}

// tryTeam matches team names (translated + color aliases).
func (am *ArgMatcher) tryTeam(tok, lang string, result *ParsedArgs) bool {
	if id, ok := am.lookupInLangMaps(tok, lang, am.teamMap); ok {
		result.Team = id
		return true
	}
	return false
}

// tryGender matches gender keywords.
func (am *ArgMatcher) tryGender(tok, lang string, result *ParsedArgs) bool {
	if id, ok := am.lookupInLangMaps(tok, lang, am.genderMap); ok {
		result.Gender = id
		return true
	}
	return false
}

// tryLureType matches lure type names.
func (am *ArgMatcher) tryLureType(tok, lang string, result *ParsedArgs) bool {
	if id, ok := am.lookupInLangMaps(tok, lang, am.lureMap); ok {
		result.LureType = id
		return true
	}
	return false
}

// tryMatchType matches a type name and adds its ID to result.Types.
func (am *ArgMatcher) tryMatchType(tok, lang string, result *ParsedArgs) bool {
	if id, ok := am.lookupInLangMaps(tok, lang, am.typeMap); ok {
		result.Types = append(result.Types, id)
		return true
	}
	return false
}

// tryRaidLevelName matches raid level names like "legendary", "mega", "shadow".
func (am *ArgMatcher) tryRaidLevelName(tok, lang string, result *ParsedArgs) bool {
	if levels, ok := am.lookupSliceInLangMaps(tok, lang, am.raidLevelMap); ok {
		result.RaidLevels = append(result.RaidLevels, levels...)
		return true
	}
	return false
}

// tryMatchPokemon matches a pokemon name via the resolver.
func (am *ArgMatcher) tryMatchPokemon(tok, lang string, result *ParsedArgs) bool {
	if am.resolver == nil {
		return false
	}
	resolved := am.resolver.Resolve(tok, lang)
	if len(resolved) > 0 {
		// Check for + suffix (evolution inclusion)
		if len(tok) > 0 && tok[len(tok)-1] == '+' {
			var expanded []ResolvedPokemon
			for _, r := range resolved {
				ids := am.resolver.ResolveWithEvolutions(r.PokemonID)
				for _, id := range ids {
					expanded = append(expanded, ResolvedPokemon{PokemonID: id, Form: 0})
				}
			}
			result.Pokemon = append(result.Pokemon, expanded...)
		} else {
			result.Pokemon = append(result.Pokemon, resolved...)
		}
		return true
	}
	return false
}

var pvpRangeRe = regexp.MustCompile(`^(\d{1,4})(?:-(\d{1,4}))?$`)

// tryPVPLeague matches PVP league patterns: great5, great:5, ultra10-50, ultra:10-50.
func (am *ArgMatcher) tryPVPLeague(tok, key, lang string, result *ParsedArgs) bool {
	// key is one of: arg.prefix.great, arg.prefix.greathigh, arg.prefix.greatcp,
	//                arg.prefix.ultra, arg.prefix.ultrahigh, arg.prefix.ultracp,
	//                arg.prefix.little, arg.prefix.littlehigh, arg.prefix.littlecp
	prefixes := am.cachedPrefix(key, lang)
	for _, p := range prefixes {
		val, ok := stripPrefix(tok, p)
		if !ok {
			continue
		}

		shortKey := strings.TrimPrefix(key, "arg.prefix.")

		// Determine which league and variant
		var league string
		var variant string // "", "high", "cp"
		for _, l := range []string{"great", "ultra", "little"} {
			if strings.HasPrefix(shortKey, l) {
				league = l
				variant = strings.TrimPrefix(shortKey, l)
				break
			}
		}
		if league == "" {
			continue
		}

		m := pvpRangeRe.FindStringSubmatch(val)
		if m == nil {
			continue
		}

		first, _ := strconv.Atoi(m[1])
		second := 0
		if m[2] != "" {
			second, _ = strconv.Atoi(m[2])
		}

		filter := result.PVP[league]
		if filter.Best == 0 {
			filter.Best = 1 // default
		}
		if filter.Worst == 0 {
			filter.Worst = 4096 // default
		}

		switch variant {
		case "": // great5 or great1-5
			if second > 0 {
				filter.Best = first
				filter.Worst = second
			} else {
				filter.Worst = first
			}
		case "high": // greathigh3
			filter.Best = first
		case "cp": // greatcp1400
			filter.MinCP = first
		}

		result.PVP[league] = filter
		return true
	}
	return false
}

// tryRemoveUID matches "id:45" or "id:123" — tracking UID for removal.
// Collects into ParsedArgs.RemoveUIDs (supports multiple in one command).
func tryRemoveUID(tok string, result *ParsedArgs) bool {
	if !strings.HasPrefix(tok, "id:") {
		return false
	}
	val := tok[3:]
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil || n <= 0 {
		return false
	}
	result.RemoveUIDs = append(result.RemoveUIDs, n)
	return true
}

var latLonRe = regexp.MustCompile(`^(-?\d+\.?\d*),(-?\d+\.?\d*)$`)

// tryLatLon matches coordinate pairs like "51.28,1.08".
func (am *ArgMatcher) tryLatLon(tok string, result *ParsedArgs) bool {
	m := latLonRe.FindStringSubmatch(tok)
	if m == nil {
		return false
	}
	lat, err1 := strconv.ParseFloat(m[1], 64)
	lon, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil {
		return false
	}
	result.Coords = &LatLon{Lat: lat, Lon: lon}
	return true
}

// resolvePrefix returns the translated prefix strings to try (user lang + English).
// Returns lowercase strings. Deduplicates if both return the same value.
func (am *ArgMatcher) resolvePrefix(key, lang string) []string {
	var prefixes []string
	seen := make(map[string]bool)

	// User's language first
	if tr := am.bundle.For(lang); tr != nil {
		val := strings.ToLower(tr.T(key))
		if val != key && val != "" {
			prefixes = append(prefixes, val)
			seen[val] = true
		}
	}

	// English fallback
	if lang != "en" {
		if tr := am.bundle.For("en"); tr != nil {
			val := strings.ToLower(tr.T(key))
			if val != key && val != "" && !seen[val] {
				prefixes = append(prefixes, val)
			}
		}
	}

	return prefixes
}

// lookupInLangMaps checks user lang first, then English fallback.
func (am *ArgMatcher) lookupInLangMaps(tok, lang string, maps map[string]map[string]int) (int, bool) {
	if m, ok := maps[lang]; ok {
		if id, ok := m[tok]; ok {
			return id, true
		}
	}
	if lang != "en" {
		if m, ok := maps["en"]; ok {
			if id, ok := m[tok]; ok {
				return id, true
			}
		}
	}
	return 0, false
}

// lookupSliceInLangMaps checks user lang first, then English fallback.
func (am *ArgMatcher) lookupSliceInLangMaps(tok, lang string, maps map[string]map[string][]int) ([]int, bool) {
	if m, ok := maps[lang]; ok {
		if ids, ok := m[tok]; ok {
			return ids, true
		}
	}
	if lang != "en" {
		if m, ok := maps["en"]; ok {
			if ids, ok := m[tok]; ok {
				return ids, true
			}
		}
	}
	return nil, false
}

// ReportUnrecognized returns a reply warning about unrecognized arguments,
// or nil if all args were recognized.
func ReportUnrecognized(parsed *ParsedArgs, tr *i18n.Translator) *Reply {
	if len(parsed.Unrecognized) == 0 {
		return nil
	}
	msg := tr.Tf("msg.unrecognized", strings.Join(parsed.Unrecognized, ", "))
	return &Reply{React: "🙅", Text: msg}
}
