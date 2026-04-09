package rowtext

import (
	"strings"

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
		// Try i18n key first (works for grunt type names that have translations),
		// otherwise capitalize the raw name (pokestop events like kecleon, showcase).
		translated := tr.T(invasion.GruntType)
		if translated == invasion.GruntType && len(translated) > 0 {
			typeText = strings.ToUpper(translated[:1]) + translated[1:]
		} else {
			typeText = translated
		}
	}

	s := tr.Tf("tracking.grunt_type_fmt", typeText)

	if invasion.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", invasion.Distance)
	}

	s += " | " + tr.Tf("tracking.gender_fmt", genderText)

	s += " " + standardText(tr, invasion.Template, g.DefaultTemplateName, invasion.Clean)

	return s
}
