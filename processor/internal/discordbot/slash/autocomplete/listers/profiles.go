package listers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListProfiles enumerates the user's profiles. Each Choice label has the
// shape "name [#N]" so FilterAndCap's tail-truncation preserves the
// profile-number suffix the caller switches on; the Value is the profile
// number as a decimal string.
func ListProfiles(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	if deps.Humans == nil {
		return nil, nil
	}
	profiles, err := deps.Humans.GetProfiles(userID)
	if err != nil {
		return nil, err
	}
	out := make([]autocomplete.Choice, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, autocomplete.Choice{
			Label: fmt.Sprintf("%s [#%d]", p.Name, p.ProfileNo),
			Value: strconv.Itoa(p.ProfileNo),
		})
	}
	return out, nil
}
