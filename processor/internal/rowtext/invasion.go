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
		typeText = invasionTypeText(tr, invasion.GruntType)
	}

	s := tr.Tf("tracking.grunt_type_fmt", typeText)

	if invasion.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", invasion.Distance)
	}

	s += " | " + tr.Tf("tracking.gender_fmt", genderText)

	s += " " + standardText(tr, invasion.Template, g.DefaultTemplateName, invasion.Clean)
	s = appendOverride(tr, s, invasion.OverrideLocationLabel, invasion.OverrideAreas)

	return s
}

// invasionTypeText renders a stored grunt_type for display.
//
// grunt_type is stored lowercased ("grass"), while the pogo-translations
// locale files are English-as-key and title-cased ("Grass" -> "Pflanze"), so
// looking the stored value up directly missed every time and every non-English
// user saw English type names in their own !tracked list.
//
// Order: the two catch-alls have their own labels; then the title-cased
// English-as-key lookup; then the raw name capitalised, which covers event
// names and npc_* that are translated nowhere.
func invasionTypeText(tr *i18n.Translator, gruntType string) string {
	switch strings.ToLower(gruntType) {
	case "everything":
		return tr.T("tracking.everything")
	case "boss":
		if v := tr.T("tracking.boss"); v != "tracking.boss" {
			return v
		}
		return "Boss"
	}

	titled := strings.ToUpper(gruntType[:1]) + gruntType[1:]
	if v := tr.T(titled); v != titled && v != "" {
		return v
	}
	return titled
}
