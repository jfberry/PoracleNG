package nlp

import "strings"

// IntentResult holds the output of intent detection.
type IntentResult struct {
	CommandType string
	IsRemove    bool
	Remaining   string
}

// removeKeywords trigger removal mode.
var removeKeywords = []string{
	"stop tracking", // multi-word first
	"get rid of",
	"remove",
	"delete",
	"untrack",
	"cancel",
	"disable",
}

// intentKeywords maps single tokens to command types (first match wins in scan order).
// Order matters: we iterate intentKeywordOrder for priority.
var intentKeywords = map[string]string{
	"raid":       "raid",
	"raids":      "raid",
	"egg":        "egg",
	"eggs":       "egg",
	"quest":      "quest",
	"quests":     "quest",
	"research":   "quest",
	"task":       "quest",
	"tasks":      "quest",
	"invasion":   "invasion",
	"invasions":  "invasion",
	"rocket":     "invasion",
	"grunt":      "invasion",
	"grunts":     "invasion",
	"lure":       "lure",
	"lures":      "lure",
	"nest":       "nest",
	"nests":      "nest",
	"nesting":    "nest",
	"gym":        "gym",
	"gyms":       "gym",
	"pokestop":   "fort",
	"pokestops":  "fort",
	"fort":       "fort",
	"maxbattle":  "maxbattle",
	"dynamax":    "maxbattle",
	"gigantamax": "maxbattle",
	"battles":    "", // consumed as noise when near maxbattle keywords
}

// DetectIntent determines the command type, removal flag, and remaining tokens
// from normalized input. invasionEvents is a set of known invasion event names
// (e.g. "kecleon", "showcase", "gold-stop") that force intent to "invasion".
func DetectIntent(normalized string, invasionEvents map[string]bool) IntentResult {
	tokens := strings.Fields(normalized)
	result := IntentResult{CommandType: "track"}

	// --- Pre-scan: check for invasion event names ---
	hasInvasionEvent := false
	for _, t := range tokens {
		if invasionEvents[t] {
			hasInvasionEvent = true
			break
		}
	}
	if hasInvasionEvent {
		result.CommandType = "invasion"
		// Remove intent keywords but keep the event name.
		tokens = removeIntentKeywords(tokens, invasionEvents)
		result.Remaining = strings.Join(tokens, " ")
		return result
	}

	// --- Remove detection (multi-word phrases first, then single words).
	// Match whole tokens / whole phrases only so tokens like "auto-delete"
	// don't get partial-matched on "delete" and misclassified as remove.
	joined := strings.Join(tokens, " ")
	for _, kw := range removeKeywords {
		padded := " " + joined + " "
		search := " " + kw + " "
		if idx := strings.Index(padded, search); idx >= 0 {
			result.IsRemove = true
			start := idx                  // position in padded
			end := idx + len(search) - 1  // keep one surrounding space on the right
			joined = strings.TrimSpace(padded[:start] + " " + padded[end:])
			joined = collapseSpaces(joined)
			break
		}
	}
	tokens = strings.Fields(joined)

	// --- Multi-word intent: "max battle", "raid egg(s)" ---
	tokens = detectMultiWordIntent(tokens, &result)

	// --- Implicit filter injection for gigantamax ---
	injectGigantamax := false

	// --- Single-token intent detection (first match wins) ---
	if result.CommandType == "track" {
		filtered := make([]string, 0, len(tokens))
		found := false
		for _, t := range tokens {
			if !found {
				if cmd, ok := intentKeywords[t]; ok && cmd != "" {
					result.CommandType = cmd
					found = true
					if t == "gigantamax" {
						injectGigantamax = true
					}
					continue // consume the keyword
				}
			}
			filtered = append(filtered, t)
		}
		tokens = filtered
	}

	// When intent is maxbattle, consume "battles" noise token.
	if result.CommandType == "maxbattle" {
		filtered := make([]string, 0, len(tokens))
		for _, t := range tokens {
			if t == "battles" || t == "battle" {
				continue
			}
			filtered = append(filtered, t)
		}
		tokens = filtered
	}

	if injectGigantamax {
		tokens = append(tokens, "level7")
	}

	result.Remaining = strings.Join(tokens, " ")
	return result
}

// removeIntentKeywords removes any token that is an intent keyword but not an invasion event.
func removeIntentKeywords(tokens []string, invasionEvents map[string]bool) []string {
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if invasionEvents[t] {
			filtered = append(filtered, t)
			continue
		}
		if _, isIntent := intentKeywords[t]; isIntent {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// detectMultiWordIntent handles compound intent phrases:
// "max battle" → "maxbattle", "raid egg(s)" → "egg".
func detectMultiWordIntent(tokens []string, result *IntentResult) []string {
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i] == "max" && tokens[i+1] == "battle" {
			result.CommandType = "maxbattle"
			return append(tokens[:i], tokens[i+2:]...)
		}
		// "raid egg" / "raid eggs" → egg intent (consume both tokens)
		if tokens[i] == "raid" && (tokens[i+1] == "egg" || tokens[i+1] == "eggs") {
			result.CommandType = "egg"
			return append(tokens[:i], tokens[i+2:]...)
		}
	}
	return tokens
}
