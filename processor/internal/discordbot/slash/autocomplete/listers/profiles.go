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
//
// For the copyto sub-command (hint.Subtype == "copyto") the user's current
// profile is omitted: copyto rejects copying a profile onto itself.
func ListProfiles(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	if deps.Humans == nil {
		return nil, nil
	}
	profiles, err := deps.Humans.GetProfiles(userID)
	if err != nil {
		return nil, err
	}
	skip := -1
	if hint.Subtype == "copyto" {
		if h, err := deps.Humans.Get(userID); err == nil && h != nil {
			skip = h.CurrentProfileNo
		}
	}
	out := make([]autocomplete.Choice, 0, len(profiles))
	for _, p := range profiles {
		if p.ProfileNo == skip {
			continue
		}
		out = append(out, autocomplete.Choice{
			Label: fmt.Sprintf("%s [#%d]", p.Name, p.ProfileNo),
			Value: strconv.Itoa(p.ProfileNo),
		})
	}
	return out, nil
}
