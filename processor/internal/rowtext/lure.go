package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// LureRowText generates a human-readable description of a lure tracking rule.
func (g *Generator) LureRowText(tr *i18n.Translator, lure *db.LureTracking) string {
	typeText := tr.T("tracking.any")
	if lure.LureID != 0 {
		// Look up lure name from game data first, fall back to i18n key
		if g.GD != nil && g.GD.Util != nil {
			if lureInfo, ok := g.GD.Util.Lures[lure.LureID]; ok && lureInfo.Name != "" {
				typeText = lureInfo.Name
			} else {
				typeText = fmt.Sprintf("Lure %d", lure.LureID)
			}
		} else {
			typeText = fmt.Sprintf("Lure %d", lure.LureID)
		}
	}

	s := tr.Tf("tracking.lure_type_fmt", typeText)

	if lure.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", lure.Distance)
	}

	s += " " + standardText(tr, lure.Template, g.DefaultTemplateName, lure.Clean)

	return s
}
