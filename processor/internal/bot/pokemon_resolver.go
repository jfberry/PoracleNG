package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ResolvedPokemon represents a pokemon matched by name resolution.
type ResolvedPokemon struct {
	PokemonID int
	Form      int
}

// PokemonResolver resolves user-typed pokemon names to IDs using the raw
// masterfile, poke_{id} translation keys, and pokemonAlias.json.
type PokemonResolver struct {
	nameToIDs map[string]map[string][]int // lang → lowercase name → pokemon IDs
	aliases   map[string][]int            // alias → pokemon IDs
	gameData  *gamedata.GameData
	bundle    *i18n.Bundle
}

// NewPokemonResolver builds name→ID lookup maps for all configured languages.
func NewPokemonResolver(gd *gamedata.GameData, bundle *i18n.Bundle, languages []string, aliases map[string][]int) *PokemonResolver {
	r := &PokemonResolver{
		nameToIDs: make(map[string]map[string][]int),
		aliases:   aliases,
		gameData:  gd,
		bundle:    bundle,
	}
	if r.aliases == nil {
		r.aliases = make(map[string][]int)
	}

	// Build name→ID maps per language from poke_{id} translation keys.
	// For each pokemon, register both the raw lowercased name and a
	// de-punctuated variant so "Mr. Mime" matches "mr mime" and "mr.
	// mime", "Farfetch'd" matches "farfetch'd" and "farfetchd",
	// "Type: Null" matches "type: null" / "type null" / "typenull".
	for _, lang := range languages {
		tr := bundle.For(lang)
		if tr == nil {
			continue
		}
		langMap := make(map[string][]int)
		add := func(key string, id int) {
			if key == "" {
				return
			}
			// Avoid duplicate entries for the same ID on repeat variants.
			existing := langMap[key]
			for _, e := range existing {
				if e == id {
					return
				}
			}
			langMap[key] = append(existing, id)
		}
		for id := 1; id <= 2000; id++ {
			key := gamedata.PokemonTranslationKey(id)
			name := tr.T(key)
			if name == key {
				continue // no translation found
			}
			lower := toLower(name)
			add(lower, id)
			for _, variant := range depunctuatedVariants(lower) {
				add(variant, id)
			}
		}
		r.nameToIDs[lang] = langMap
	}

	return r
}

// depunctuatedVariants returns variants of a lowercased pokemon name with
// common punctuation stripped or normalised. "mr. mime" yields "mr mime"
// (period + space collapsed to space). "farfetch'd" yields "farfetchd".
// "type: null" yields "type null" and "typenull". Duplicates of the
// input are filtered by the caller.
func depunctuatedVariants(name string) []string {
	var out []string
	seen := map[string]bool{name: true}
	add := func(s string) {
		s = strings.TrimSpace(collapseInternalWhitespace(s))
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	// Drop apostrophes entirely: "farfetch'd" → "farfetchd".
	add(strings.ReplaceAll(name, "'", ""))
	// Drop periods/colons, collapse resulting double spaces: "mr. mime"
	// → "mr mime", "type: null" → "type null".
	noPunct := strings.NewReplacer(".", "", ":", "").Replace(name)
	add(noPunct)
	// Drop all punctuation AND internal spaces: "type null" → "typenull".
	noSpace := strings.ReplaceAll(noPunct, " ", "")
	add(noSpace)
	// Hyphens as spaces: "ho-oh" → "ho oh".
	add(strings.ReplaceAll(name, "-", " "))
	// Hyphens dropped entirely: "ho-oh" → "hooh".
	add(strings.ReplaceAll(name, "-", ""))
	return out
}

func collapseInternalWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if !prevSpace {
				b.WriteRune(r)
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// Resolve returns matching pokemon for a user-typed name.
// Checks in order: numeric ID, alias, exact name (user lang), exact name (English).
// Returns nil if no match.
func (r *PokemonResolver) Resolve(name string, lang string) []ResolvedPokemon {
	if r == nil {
		return nil
	}

	// Strip + suffix (evolution flag handled by caller)
	cleanName := name
	if len(cleanName) > 0 && cleanName[len(cleanName)-1] == '+' {
		cleanName = cleanName[:len(cleanName)-1]
	}

	// 1. Numeric ID
	if id := parseIntSafe(cleanName); id > 0 {
		return []ResolvedPokemon{{PokemonID: id, Form: 0}}
	}

	// 2. Alias (may resolve to multiple pokemon, e.g. "laketrio" → [480, 481, 482])
	if ids, ok := r.aliases[cleanName]; ok {
		return idsToResolved(ids)
	}

	// 3. User's language
	if ids := r.lookupLang(cleanName, lang); len(ids) > 0 {
		return idsToResolved(ids)
	}

	// 4. English fallback
	if lang != "en" {
		if ids := r.lookupLang(cleanName, "en"); len(ids) > 0 {
			return idsToResolved(ids)
		}
	}

	return nil
}

// ResolveWithEvolutions returns the pokemon ID plus all evolution IDs recursively.
func (r *PokemonResolver) ResolveWithEvolutions(pokemonID int) []int {
	if r.gameData == nil {
		return []int{pokemonID}
	}
	result := []int{pokemonID}
	seen := map[int]bool{pokemonID: true}
	r.collectEvolutions(pokemonID, &result, seen, 0)
	return result
}

func (r *PokemonResolver) collectEvolutions(id int, result *[]int, seen map[int]bool, depth int) {
	if depth > 20 {
		return
	}
	// Look up base form (form 0) for evolution data
	pokemon := r.gameData.Monsters[gamedata.MonsterKey{ID: id, Form: 0}]
	if pokemon == nil {
		return
	}
	for _, evo := range pokemon.Evolutions {
		if !seen[evo.PokemonID] {
			seen[evo.PokemonID] = true
			*result = append(*result, evo.PokemonID)
			r.collectEvolutions(evo.PokemonID, result, seen, depth+1)
		}
	}
}

func (r *PokemonResolver) lookupLang(name, lang string) []int {
	if langMap, ok := r.nameToIDs[lang]; ok {
		return langMap[name]
	}
	return nil
}

func idsToResolved(ids []int) []ResolvedPokemon {
	result := make([]ResolvedPokemon, len(ids))
	for i, id := range ids {
		result[i] = ResolvedPokemon{PokemonID: id, Form: 0}
	}
	return result
}

func toLower(s string) string {
	return strings.ToLower(s)
}

func parseIntSafe(s string) int {
	if len(s) == 0 || len(s) > 5 {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// LoadPokemonAliases reads pokemonAlias.json from configDir (preferred) or fallbackDir.
// Returns a map of lowercase alias → pokemon IDs. Each value may be a single int
// or an array of ints (e.g. "laketrio" → [480, 481, 482]).
func LoadPokemonAliases(configDir, fallbackDir string) map[string][]int {
	path := filepath.Join(configDir, "pokemonAlias.json")
	data, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Join(fallbackDir, "pokemonAlias.json")
		data, err = os.ReadFile(path)
		if err != nil {
			log.Debug("No pokemonAlias.json found")
			return nil
		}
	}

	// JSON values are either a single number or an array of numbers
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Warnf("pokemonAlias: failed to parse %s: %v", path, err)
		return nil
	}

	result := make(map[string][]int, len(raw))
	for name, val := range raw {
		key := strings.ToLower(name)
		// Try array first
		var ids []int
		if err := json.Unmarshal(val, &ids); err == nil {
			result[key] = ids
			continue
		}
		// Try single int
		var id int
		if err := json.Unmarshal(val, &id); err == nil {
			result[key] = []int{id}
			continue
		}
		log.Warnf("pokemonAlias: skipping %q: unsupported value type", name)
	}

	log.Infof("Loaded %d pokemon aliases from %s", len(result), path)
	return result
}
