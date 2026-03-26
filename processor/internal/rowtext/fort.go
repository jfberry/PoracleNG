package rowtext

import (
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// FortUpdateRowText generates a human-readable description of a fort update tracking rule.
func (g *Generator) FortUpdateRowText(tr *i18n.Translator, fort *db.FortTracking) string {
	s := tr.Tf("tracking.fort_updates_fmt", tr.T(fort.FortType))

	if fort.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", fort.Distance)
	}

	s += " " + fort.ChangeTypes

	if fort.IncludeEmpty {
		s += " " + tr.T("tracking.including_empty")
	}

	s += " " + standardText(tr, fort.Template, g.DefaultTemplateName, false)

	return s
}
