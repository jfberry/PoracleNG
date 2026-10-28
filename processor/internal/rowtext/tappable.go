package rowtext

import (
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// TappableRowText generates a human-readable description of a tappable
// tracking rule. The reward phrasing is shared with quest rules (same reward
// type ids); tappables have no edit, summary, shiny or form.
func (g *Generator) TappableRowText(tr *i18n.Translator, t *db.TappableTracking) string {
	s := tr.Tf("tracking.reward_fmt", g.rewardText(tr, t.RewardType, t.Reward, 0))

	if t.Amount > 0 {
		s += " " + tr.Tf("tracking.reward_min_fmt", t.Amount)
	}

	if t.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", t.Distance)
	}

	s += " " + standardText(tr, t.Template, g.DefaultTemplateName, t.Clean, false)
	s = appendOverride(tr, s, t.OverrideLocationLabel, t.OverrideAreas)

	return s
}
