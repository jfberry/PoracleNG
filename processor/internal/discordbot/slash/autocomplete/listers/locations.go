package listers

import (
	"context"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListUserLocations enumerates the user's saved named locations whose label
// contains the focused partial (case-insensitive). Returns (nil, nil) for an
// unregistered user so callers can surface "no choices" without an error path.
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
	if locs == nil {
		return nil, nil
	}
	low := strings.ToLower(hint.Focused)
	out := make([]autocomplete.Choice, 0, len(locs))
	for _, l := range locs {
		if low == "" || strings.Contains(strings.ToLower(l.Label), low) {
			out = append(out, autocomplete.Choice{Label: l.Label, Value: l.Label})
		}
	}
	return out, nil
}
