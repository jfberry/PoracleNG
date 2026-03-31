package bot

import (
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
	aliases   map[string]int              // alias → pokemon ID
	gameData  *gamedata.GameData
	bundle    *i18n.Bundle
}

// NewPokemonResolver builds name→ID lookup maps for all configured languages.
func NewPokemonResolver(gd *gamedata.GameData, bundle *i18n.Bundle, languages []string, aliases map[string]int) *PokemonResolver {
	r := &PokemonResolver{
		nameToIDs: make(map[string]map[string][]int),
		aliases:   aliases,
		gameData:  gd,
		bundle:    bundle,
	}
	if r.aliases == nil {
		r.aliases = make(map[string]int)
	}

	// Build name→ID maps per language from poke_{id} translation keys
	for _, lang := range languages {
		tr := bundle.For(lang)
		if tr == nil {
			continue
		}
		langMap := make(map[string][]int)
		for id := 1; id <= 2000; id++ {
			key := gamedata.PokemonTranslationKey(id)
			name := tr.T(key)
			if name == key {
				continue // no translation found
			}
			lower := toLower(name)
			langMap[lower] = append(langMap[lower], id)
		}
		r.nameToIDs[lang] = langMap
	}

	return r
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

	// 2. Alias
	if id, ok := r.aliases[cleanName]; ok {
		return []ResolvedPokemon{{PokemonID: id, Form: 0}}
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
	// Fast path for ASCII-only (most pokemon names)
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
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
