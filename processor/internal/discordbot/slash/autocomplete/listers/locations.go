package listers

import (
	"context"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListUserLocations enumerates the user's saved named locations whose label
// contains the focused partial (case-insensitive). The synthetic "default"
// entry is always prepended when the partial matches it, so users can
// discover /location show default and /location remove default without
// reading docs. Returns (nil, nil) for an unregistered user so callers can
// surface "no choices" without an error path.
//
// Used by /location show name, /location remove name, and the `location`
// option on tracker slash commands (Task 22).
func ListUserLocations(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	if deps.Humans == nil {
		return nil, nil
	}
	locs, err := deps.Humans.ListLocations(userID)
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(hint.Focused)
	out := make([]autocomplete.Choice, 0, len(locs)+1)
	// Prepend "default" when the partial matches (case-insensitive).
	if low == "" || strings.Contains("default", low) {
		out = append(out, autocomplete.Choice{Label: "default", Value: "default"})
	}
	for _, l := range locs {
		if low == "" || strings.Contains(strings.ToLower(l.Label), low) {
			out = append(out, autocomplete.Choice{Label: l.Label, Value: l.Label})
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
