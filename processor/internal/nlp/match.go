package nlp

import (
	"strings"
)

// MatchResult holds the outcome of greedy multi-word token matching.
type MatchResult struct {
	Pokemon    []string // canonical pokemon names
	Forms      []string // "form:alolan", etc.
	Types      []string // type names (for invasion/nest)
	Items      []string // item names (for quest)
	Moves      []string // move names (for raid/maxbattle)
	Filters    []string // iv100, d1000, level5, great5, male, size:xxl, etc.
	Everything bool
	Unmatched  []string
}

// matchTokens performs greedy multi-word token matching against vocabularies
// and filter patterns. Multi-word matches are tried first (longest first),
// then remaining single tokens are classified.
// invasionEvents is optional; if non-nil, tokens matching invasion event names
// are treated as Pokemon (passed through as-is in invasion commands).
func matchTokens(input string, intent string, vocabs *Vocabularies, invasionEvents map[string]bool) *MatchResult {
	result := &MatchResult{}
	if input == "" {
		return result
	}

	remaining := input

	// --- Phase 1: Multi-word matching (longest first) ---
	// Try multi-word pokemon names (includes fuzzy variants like "mr mime" → "mr. mime")
	for _, variant := range vocabs.Pokemon.MultiWordNames() {
		if idx := findPhrase(remaining, variant); idx >= 0 {
			canonical := vocabs.Pokemon.multiMap[variant]
			result.Pokemon = append(result.Pokemon, canonical)
			remaining = removePhrase(remaining, idx, len(variant))
		}
	}

	// Try multi-word item names (quest context)
	if intent == "quest" {
		for _, name := range vocabs.Items.MultiWordNames() {
			if idx := findPhrase(remaining, name); idx >= 0 {
				result.Items = append(result.Items, name)
				remaining = removePhrase(remaining, idx, len(name))
			}
		}
	}

	// Try multi-word move names (raid/maxbattle context)
	if intent == "raid" || intent == "maxbattle" {
		for _, name := range vocabs.Moves.MultiWordNames() {
			if idx := findPhrase(remaining, name); idx >= 0 {
				result.Moves = append(result.Moves, name)
				remaining = removePhrase(remaining, idx, len(name))
			}
		}
	}

	// --- Phase 2: Apply filter matching on remaining tokens ---
	tokens := strings.Fields(remaining)
	if len(tokens) == 0 {
		return result
	}

	filterResult := matchFilters(tokens, intent)
	result.Filters = filterResult.Filters
	result.Everything = filterResult.Everything

	// --- Phase 3: Classify unconsumed tokens ---
	for i, t := range tokens {
		if filterResult.Consumed[i] {
			continue
		}

		// Invasion event names (e.g., kecleon, showcase)
		if invasionEvents != nil && invasionEvents[t] {
			result.Pokemon = append(result.Pokemon, t)
			continue
		}

		// Pokemon lookup
		if name := vocabs.Pokemon.Lookup(t); name != "" {
			result.Pokemon = append(result.Pokemon, name)
			continue
		}

		// Form lookup
		if f := vocabs.Forms.Lookup(t); f != "" {
			// In raid context, "shadow" is handled as a filter synonym
			if intent == "raid" && t == "shadow" {
				// Already handled by filter synonyms
			} else {
				result.Forms = append(result.Forms, f)
				continue
			}
		}

		// Type lookup (invasion/nest context)
		if intent == "invasion" || intent == "nest" {
			if typeName := vocabs.Types.Lookup(t); typeName != "" {
				result.Types = append(result.Types, typeName)
				continue
			}
		}

		// Single-word item lookup (quest context)
		if intent == "quest" {
			if itemName := vocabs.Items.Lookup(t); itemName != "" {
				result.Items = append(result.Items, itemName)
				continue
			}
		}

		// Single-word move lookup (raid/maxbattle context)
		if intent == "raid" || intent == "maxbattle" {
			if moveName := vocabs.Moves.Lookup(t); moveName != "" {
				result.Moves = append(result.Moves, moveName)
				continue
			}
		}

		// Skip common generic nouns that are semantic noise
		if isGenericNoun(t) {
			continue
		}

		// Unmatched
		result.Unmatched = append(result.Unmatched, t)
	}

	return result
}

// isGenericNoun returns true for words that carry no command information.
func isGenericNoun(t string) bool {
	switch t {
	case "pokemon", "type", "added", "taken", "pokestops",
		"anything", "within":
		return true
	}
	return false
}

// findPhrase finds a phrase as whole words within text, returning the byte index or -1.
func findPhrase(text, phrase string) int {
	idx := strings.Index(text, phrase)
	if idx < 0 {
		return -1
	}
	// Verify word boundaries
	if idx > 0 && text[idx-1] != ' ' {
		return -1
	}
	end := idx + len(phrase)
	if end < len(text) && text[end] != ' ' {
		return -1
	}
	return idx
}

// removePhrase removes a substring at the given position and cleans up whitespace.
func removePhrase(text string, idx, length int) string {
	result := text[:idx] + text[idx+length:]
	return collapseSpaces(result)
}
