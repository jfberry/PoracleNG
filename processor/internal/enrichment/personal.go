package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// AnnotatePersonal adds the per-user personal shiny/background fields for
// every matched user with linked accounts, whichever rule matched them.
// perUser may be nil (no PVP display configured); it is created as needed
// and returned. A nil checker (feature off) leaves perUser untouched.
func AnnotatePersonal(perUser map[string]map[string]any, matched []webhook.MatchedUser, c *personalshiny.Checker) map[string]map[string]any {
	if c == nil {
		return perUser
	}
	for _, u := range matched {
		res, linked := c.Result(u.ID)
		if !linked {
			continue
		}
		if perUser == nil {
			perUser = make(map[string]map[string]any)
		}
		m := perUser[u.ID]
		if m == nil {
			m = make(map[string]any, 5)
			perUser[u.ID] = m
		}
		m["hasLinkedAccounts"] = true
		m["personalShiny"] = len(res.Shiny) > 0
		m["personalShinyAccounts"] = displayNames(res.Shiny)
		m["personalBackground"] = len(res.Background) > 0
		m["personalBackgroundAccounts"] = displayNames(res.Background)
	}
	return perUser
}

func displayNames(as []db.PlayerAccount) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = personalshiny.DisplayName(a)
	}
	return out
}
