package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// NestRowText generates a human-readable description of a nest tracking rule.
func (g *Generator) NestRowText(tr *i18n.Translator, nest *db.NestTracking) string {
	name, formName := translateMonsterName(tr, g.GD, nest.PokemonID, nest.Form)

	s := fmt.Sprintf("**%s**", name)
	if formName != "" {
		s += " " + tr.Tf("tracking.form_fmt", formName)
	}

	if nest.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", nest.Distance)
	}

	if nest.MinSpawnAvg != 0 {
		s += " " + tr.Tf("tracking.min_avg_spawn", nest.MinSpawnAvg)
	}

	s += " " + standardText(tr, nest.Template, g.DefaultTemplateName, nest.Clean)

	return s
}
