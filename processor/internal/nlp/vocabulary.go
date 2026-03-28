package nlp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// PokemonVocab provides fuzzy lookup from user input to canonical pokemon names.
type PokemonVocab struct {
	single     map[string]string   // normalized input → canonical lowercase name
	groups     map[string][]string // group alias → list of canonical names
	multiWords []string            // multi-word names sorted longest-first
}

// Lookup returns the canonical lowercase pokemon name for input, or "" if not found.
func (v *PokemonVocab) Lookup(input string) string {
	return v.single[strings.ToLower(input)]
}

// LookupGroup returns multiple canonical names for a group alias, or nil.
func (v *PokemonVocab) LookupGroup(input string) []string {
	return v.groups[strings.ToLower(input)]
}

// MultiWordNames returns multi-word pokemon names sorted longest-first.
func (v *PokemonVocab) MultiWordNames() []string {
	return v.multiWords
}

// TypeVocab maps type name strings to themselves (for membership checks).
type TypeVocab struct {
	names map[string]bool
}

// Lookup returns the type name if known, or "".
func (v *TypeVocab) Lookup(input string) string {
	if v.names[strings.ToLower(input)] {
		return strings.ToLower(input)
	}
	return ""
}

// Names returns all known type names.
func (v *TypeVocab) Names() []string {
	out := make([]string, 0, len(v.names))
	for n := range v.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// FormVocab maps form names to "form:{name}" identifiers.
type FormVocab struct {
	forms map[string]string // lowered name → "form:{name}"
}

// Lookup returns "form:{name}" for a known form, or "".
func (v *FormVocab) Lookup(input string) string {
	return v.forms[strings.ToLower(input)]
}

// ItemVocab provides lookup for item names.
type ItemVocab struct {
	items      map[string]bool // known item names (lowercased)
	multiWords []string        // multi-word item names sorted longest-first
}

// Lookup returns the item name if known, or "".
func (v *ItemVocab) Lookup(input string) string {
	low := strings.ToLower(input)
	if v.items[low] {
		return low
	}
	return ""
}

// MultiWordNames returns multi-word item names sorted longest-first.
func (v *ItemVocab) MultiWordNames() []string {
	return v.multiWords
}

// MoveVocab provides lookup for move names.
type MoveVocab struct {
	moves      map[string]bool
	multiWords []string // multi-word move names sorted longest-first
}

// Lookup returns the move name if known, or "".
func (v *MoveVocab) Lookup(input string) string {
	low := strings.ToLower(input)
	if v.moves[low] {
		return low
	}
	return ""
}

// MultiWordNames returns multi-word move names sorted longest-first.
func (v *MoveVocab) MultiWordNames() []string {
	return v.multiWords
}

// Vocabularies holds all vocabulary lookups built from game data and translations.
type Vocabularies struct {
	Pokemon *PokemonVocab
	Types   *TypeVocab
	Forms   *FormVocab
	Items   *ItemVocab
	Moves   *MoveVocab
}

// BuildVocabularies constructs all vocabularies from the translator and alias files.
func BuildVocabularies(tr *i18n.Translator, baseDir string) *Vocabularies {
	msgs := tr.Messages()
	if msgs == nil {
		msgs = map[string]string{}
	}

	return &Vocabularies{
		Pokemon: buildPokemonVocab(msgs, baseDir),
		Types:   buildTypeVocab(msgs),
		Forms:   buildFormVocab(msgs),
		Items:   buildItemVocab(msgs),
		Moves:   buildMoveVocab(msgs),
	}
}

// stripPunctuation removes . ' ' characters and replaces - with space, then lowercases.
func stripPunctuation(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "\u2019", "") // right single quotation mark
	return s
}

func buildPokemonVocab(msgs map[string]string, baseDir string) *PokemonVocab {
	single := make(map[string]string)
	groups := make(map[string][]string)
	multiWordSet := make(map[string]bool)

	// Map from pokemon ID → canonical lowercase name
	idToName := make(map[int]string)

	for key, val := range msgs {
		if !strings.HasPrefix(key, "poke_") {
			continue
		}
		idStr := strings.TrimPrefix(key, "poke_")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}

		canonical := strings.ToLower(val)
		idToName[id] = canonical

		// Register canonical
		single[canonical] = canonical

		// Stripped variant (remove . ' ', replace - with space)
		stripped := stripPunctuation(val)
		if stripped != canonical {
			single[stripped] = canonical
		}

		// Spaces-removed variant
		noSpaces := strings.ReplaceAll(stripped, " ", "")
		if noSpaces != canonical && noSpaces != stripped {
			single[noSpaces] = canonical
		}

		// Track multi-word names
		if strings.Contains(canonical, " ") || strings.Contains(canonical, "-") {
			multiWordSet[canonical] = true
		}
	}

	// Load aliases
	aliases := loadPokemonAliases(baseDir)
	for alias, val := range aliases {
		aliasLow := strings.ToLower(alias)
		switch v := val.(type) {
		case float64:
			// Single pokemon ID
			id := int(v)
			if name, ok := idToName[id]; ok {
				single[aliasLow] = name
			}
		case []interface{}:
			// Group of pokemon IDs
			var names []string
			for _, item := range v {
				if fid, ok := item.(float64); ok {
					if name, found := idToName[int(fid)]; found {
						names = append(names, name)
					}
				}
			}
			if len(names) > 0 {
				groups[aliasLow] = names
			}
		}
	}

	// Build sorted multi-word list (longest first)
	multiWords := make([]string, 0, len(multiWordSet))
	for name := range multiWordSet {
		multiWords = append(multiWords, name)
	}
	sort.Slice(multiWords, func(i, j int) bool {
		if len(multiWords[i]) != len(multiWords[j]) {
			return len(multiWords[i]) > len(multiWords[j])
		}
		return multiWords[i] < multiWords[j]
	})

	return &PokemonVocab{
		single:     single,
		groups:     groups,
		multiWords: multiWords,
	}
}

func buildTypeVocab(msgs map[string]string) *TypeVocab {
	names := make(map[string]bool)
	for key, val := range msgs {
		if !strings.HasPrefix(key, "poke_type_") {
			continue
		}
		names[strings.ToLower(val)] = true
	}
	return &TypeVocab{names: names}
}

// trackableForms lists form names that can be tracked.
var trackableForms = map[string]bool{
	"alolan":   true,
	"galarian": true,
	"hisuian":  true,
	"paldean":  true,
	"shadow":   true,
	"purified": true,
	"origin":   true,
	"altered":  true,
	"attack":   true,
	"defense":  true,
	"speed":    true,
	"plant":    true,
	"sandy":    true,
	"trash":    true,
}

func buildFormVocab(msgs map[string]string) *FormVocab {
	forms := make(map[string]string)
	for key, val := range msgs {
		if !strings.HasPrefix(key, "form_") {
			continue
		}
		low := strings.ToLower(val)
		if trackableForms[low] {
			forms[low] = "form:" + low
		}
	}
	return &FormVocab{forms: forms}
}

func buildItemVocab(msgs map[string]string) *ItemVocab {
	items := make(map[string]bool)
	var multiWords []string
	for key, val := range msgs {
		if !strings.HasPrefix(key, "item_") {
			continue
		}
		low := strings.ToLower(val)
		if low == "" {
			continue
		}
		items[low] = true
		if strings.Contains(low, " ") {
			multiWords = append(multiWords, low)
		}
	}
	sort.Slice(multiWords, func(i, j int) bool {
		if len(multiWords[i]) != len(multiWords[j]) {
			return len(multiWords[i]) > len(multiWords[j])
		}
		return multiWords[i] < multiWords[j]
	})
	return &ItemVocab{items: items, multiWords: multiWords}
}

func buildMoveVocab(msgs map[string]string) *MoveVocab {
	moves := make(map[string]bool)
	var multiWords []string
	for key, val := range msgs {
		if !strings.HasPrefix(key, "move_") {
			continue
		}
		low := strings.ToLower(val)
		if low == "" {
			continue
		}
		moves[low] = true
		if strings.Contains(low, " ") {
			multiWords = append(multiWords, low)
		}
	}
	sort.Slice(multiWords, func(i, j int) bool {
		if len(multiWords[i]) != len(multiWords[j]) {
			return len(multiWords[i]) > len(multiWords[j])
		}
		return multiWords[i] < multiWords[j]
	})
	return &MoveVocab{moves: moves, multiWords: multiWords}
}

// loadPokemonAliases loads pokemonAlias.json from fallbacks/ then config/ (later overrides).
func loadPokemonAliases(baseDir string) map[string]any {
	result := make(map[string]any)

	paths := []string{
		filepath.Join(baseDir, "fallbacks", "pokemonAlias.json"),
		filepath.Join(baseDir, "config", "pokemonAlias.json"),
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file may not exist
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Fprintf(os.Stderr, "nlp: failed to parse %s: %v\n", path, err)
			continue
		}
		for k, v := range m {
			result[k] = v
		}
	}

	return result
}
