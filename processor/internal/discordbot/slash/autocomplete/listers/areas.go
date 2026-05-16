package listers

import (
	"context"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListAreas enumerates the geofence areas currently selected on the user's
// human record. Each Choice has Label == Value == the area name. Returns
// (nil, nil) for an unregistered user so callers can surface "no choices"
// without an error path.
//
// HumanLite does not carry the Area slice (it is a JSON column parsed only
// by Get), so this lister calls the full Get. Autocomplete callsites are
// already off the matching hot path, so the extra parse is cheap.
func ListAreas(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	if deps.Humans == nil {
		return nil, nil
	}
	human, err := deps.Humans.Get(userID)
	if err != nil {
		return nil, err
	}
	if human == nil {
		return nil, nil
	}
	out := make([]autocomplete.Choice, 0, len(human.Area))
	for _, area := range human.Area {
		out = append(out, autocomplete.Choice{Label: area, Value: area})
	}
	return out, nil
}
