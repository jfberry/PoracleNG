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
		key := fmt.Sprintf("lure_%d", lure.LureID)
		if v := tr.T(key); v != "" && v != key {
			typeText = v
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
