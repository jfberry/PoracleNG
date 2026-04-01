package nlp

import (
	"fmt"
	"strings"
)

// assemble converts a MatchResult into one or more Poracle command strings.
// Multiple pokemon produce multiple commands (one per pokemon, shared filters).
func assemble(intent string, isRemove bool, result *MatchResult) []string {
	switch intent {
	case "track":
		return assembleTrack(isRemove, result)
	case "raid":
		return assembleRaid(isRemove, result)
	case "egg":
		return assembleEgg(isRemove, result)
	case "invasion":
		return assembleInvasion(isRemove, result)
	case "quest":
		return assembleQuest(isRemove, result)
	case "lure":
		return assembleLure(isRemove, result)
	case "gym":
		return assembleGym(isRemove, result)
	case "nest":
		return assembleNest(isRemove, result)
	case "fort":
		return assembleFort(isRemove, result)
	case "maxbattle":
		return assembleMaxbattle(isRemove, result)
	default:
		return assembleTrack(isRemove, result)
	}
}

func assembleTrack(isRemove bool, result *MatchResult) []string {
	cmd := "!track"
	if isRemove {
		cmd = "!untrack"
	}

	subjects := result.Pokemon
	if len(subjects) == 0 {
		if result.Everything || len(result.Filters) > 0 || len(result.Forms) > 0 {
			// Have meaningful filters — use "everything" as the subject
			subjects = []string{"everything"}
		} else if len(result.Unmatched) > 0 {
			// Unrecognised tokens with no filters — pass through as pokemon names
			// (they might be valid pokemon the NLP vocabulary doesn't know about)
			subjects = result.Unmatched
		} else {
			return nil // truly empty — nothing to assemble
		}
	}

	filters := joinFilters(result.Forms, result.Filters)

	var commands []string
	for _, poke := range subjects {
		parts := []string{cmd, quoteName(poke)}
		if len(filters) > 0 {
			parts = append(parts, filters...)
		}
		commands = append(commands, strings.Join(parts, " "))
	}
	return commands
}

func assembleRaid(isRemove bool, result *MatchResult) []string {
	parts := []string{"!raid"}
	if isRemove {
		parts = append(parts, "remove")
	}

	// Pokemon names or "everything"
	if len(result.Pokemon) > 0 {
		for _, p := range result.Pokemon {
			parts = append(parts, quoteName(p))
		}
	} else if result.Everything {
		parts = append(parts, "everything")
	}

	// Moves become "move:{name}"
	for _, m := range result.Moves {
		parts = append(parts, "move:"+strings.ReplaceAll(m, " ", "_"))
	}

	parts = append(parts, sortFilters(result.Filters)...)

	return []string{strings.Join(parts, " ")}
}

func assembleEgg(isRemove bool, result *MatchResult) []string {
	parts := []string{"!egg"}
	if isRemove {
		parts = append(parts, "remove")
	}
	if result.Everything {
		parts = append(parts, "everything")
	}
	parts = append(parts, result.Filters...)
	return []string{strings.Join(parts, " ")}
}

func assembleInvasion(isRemove bool, result *MatchResult) []string {
	parts := []string{"!invasion"}
	if isRemove {
		parts = append(parts, "remove")
	}

	// Types are the main argument for invasions
	parts = append(parts, result.Types...)

	// Pokemon names (e.g., kecleon — invasion events passed through)
	for _, p := range result.Pokemon {
		parts = append(parts, quoteName(p))
	}

	parts = append(parts, result.Filters...)

	return []string{strings.Join(parts, " ")}
}

func assembleQuest(isRemove bool, result *MatchResult) []string {
	parts := []string{"!quest"}
	if isRemove {
		parts = append(parts, "remove")
	}

	// Items (quoted if multi-word)
	for _, item := range result.Items {
		parts = append(parts, quoteItem(item))
	}

	// Pokemon as quest rewards
	for _, p := range result.Pokemon {
		parts = append(parts, quoteName(p))
	}

	if result.Everything {
		parts = append(parts, "everything")
	}

	parts = append(parts, result.Filters...)

	return []string{strings.Join(parts, " ")}
}

func assembleLure(isRemove bool, result *MatchResult) []string {
	parts := []string{"!lure"}
	if isRemove {
		parts = append(parts, "remove")
	}

	// Lure type names (from unmatched or types)
	for _, t := range result.Types {
		parts = append(parts, t)
	}

	// Unmatched tokens might be lure type names
	for _, u := range result.Unmatched {
		parts = append(parts, u)
	}

	parts = append(parts, result.Filters...)

	return []string{strings.Join(parts, " ")}
}

func assembleGym(isRemove bool, result *MatchResult) []string {
	parts := []string{"!gym"}
	if isRemove {
		parts = append(parts, "remove")
	}
	if result.Everything {
		parts = append(parts, "everything")
	}
	parts = append(parts, result.Filters...)
	return []string{strings.Join(parts, " ")}
}

func assembleNest(isRemove bool, result *MatchResult) []string {
	parts := []string{"!nest"}
	if isRemove {
		parts = append(parts, "remove")
	}

	// Pokemon names
	for _, p := range result.Pokemon {
		parts = append(parts, quoteName(p))
	}

	// Types are the argument if no pokemon
	if len(result.Pokemon) == 0 {
		parts = append(parts, result.Types...)
	}

	// Only show "everything" if no types or pokemon specified
	if result.Everything && len(result.Types) == 0 && len(result.Pokemon) == 0 {
		parts = append(parts, "everything")
	}

	parts = append(parts, result.Filters...)

	return []string{strings.Join(parts, " ")}
}

func assembleFort(isRemove bool, result *MatchResult) []string {
	parts := []string{"!fort"}
	if isRemove {
		parts = append(parts, "remove")
	}
	if result.Everything {
		parts = append(parts, "everything")
	}
	parts = append(parts, result.Filters...)
	return []string{strings.Join(parts, " ")}
}

func assembleMaxbattle(isRemove bool, result *MatchResult) []string {
	parts := []string{"!maxbattle"}
	if isRemove {
		parts = append(parts, "remove")
	}

	for _, p := range result.Pokemon {
		parts = append(parts, quoteName(p))
	}

	// Moves become "move:{name}"
	for _, m := range result.Moves {
		parts = append(parts, "move:"+strings.ReplaceAll(m, " ", "_"))
	}

	if result.Everything {
		parts = append(parts, "everything")
	}

	parts = append(parts, result.Filters...)

	return []string{strings.Join(parts, " ")}
}

// quoteName quotes a name if it contains spaces or punctuation.
func quoteName(name string) string {
	if strings.ContainsAny(name, " .'") {
		return fmt.Sprintf("%q", name)
	}
	return name
}

// quoteItem quotes an item name if it contains spaces.
func quoteItem(name string) string {
	if strings.Contains(name, " ") {
		return fmt.Sprintf("%q", name)
	}
	return name
}

// joinFilters combines form filters and other filters into one slice,
// with a stable ordering: forms first, then stat filters, then distance, then others.
func joinFilters(forms []string, filters []string) []string {
	var out []string
	out = append(out, forms...)
	out = append(out, sortFilters(filters)...)
	return out
}

// filterPriority returns a sort key for filter ordering.
// Lower number = earlier in output.
func filterPriority(f string) int {
	if strings.HasPrefix(f, "form:") {
		return 0
	}
	if strings.HasPrefix(f, "iv") || strings.HasPrefix(f, "maxiv") {
		return 1
	}
	if strings.HasPrefix(f, "cp") || strings.HasPrefix(f, "maxcp") {
		return 2
	}
	if strings.HasPrefix(f, "level") || strings.HasPrefix(f, "maxlevel") {
		return 3
	}
	if strings.HasPrefix(f, "atk") || strings.HasPrefix(f, "maxatk") ||
		strings.HasPrefix(f, "def") || strings.HasPrefix(f, "maxdef") ||
		strings.HasPrefix(f, "sta") || strings.HasPrefix(f, "maxsta") {
		return 4
	}
	if strings.HasPrefix(f, "great") || strings.HasPrefix(f, "ultra") || strings.HasPrefix(f, "little") {
		return 5
	}
	if strings.HasPrefix(f, "size:") {
		return 6
	}
	if strings.HasPrefix(f, "d") && len(f) > 1 && f[1] >= '0' && f[1] <= '9' {
		return 8
	}
	return 7 // everything else
}

// sortFilters returns a copy of filters sorted by priority.
func sortFilters(filters []string) []string {
	if len(filters) <= 1 {
		return filters
	}
	out := make([]string, len(filters))
	copy(out, filters)
	// Stable insertion sort (small slices)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && filterPriority(out[j]) < filterPriority(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
