package rowtext

import (
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// InvasionRowText generates a human-readable description of an invasion tracking rule.
func (g *Generator) InvasionRowText(tr *i18n.Translator, invasion *db.InvasionTracking) string {
	var genderText string
	switch invasion.Gender {
	case 1:
		genderText = tr.T("tracking.male")
	case 2:
		genderText = tr.T("tracking.female")
	default:
		genderText = tr.T("tracking.any")
	}

	typeText := tr.T("tracking.any")
	if invasion.GruntType != "" {
		typeText = tr.T(invasion.GruntType)
	}

	s := tr.Tf("tracking.grunt_type_fmt", typeText)

	if invasion.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", invasion.Distance)
	}

	s += " | " + tr.Tf("tracking.gender_fmt", genderText)

	s += " " + standardText(tr, invasion.Template, g.DefaultTemplateName, invasion.Clean)

	return s
}
