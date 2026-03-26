package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// MaxbattleRowText generates a human-readable description of a maxbattle tracking rule.
func (g *Generator) MaxbattleRowText(tr *i18n.Translator, maxbattle *db.MaxbattleTracking) string {
	name, formName := translateMonsterName(tr, g.GD, maxbattle.PokemonID, maxbattle.Form)
	moveName := translateMoveName(tr, g.GD, maxbattle.Move)

	stationID := ""
	if maxbattle.StationID != nil {
		stationID = *maxbattle.StationID
	}
	stationNameText := resolveStationName(g.Scanner, stationID)

	if maxbattle.PokemonID == 9000 {
		// Generic level maxbattle
		var levelText string
		if maxbattle.Level == 90 {
			levelText = tr.T("tracking.all_level_maxbattles")
		} else {
			levelText = tr.Tf("tracking.level_n_maxbattles", maxbattle.Level)
		}

		s := fmt.Sprintf("**%s**", levelText)

		if maxbattle.Distance != 0 {
			s += " | " + tr.Tf("tracking.distance_fmt", maxbattle.Distance)
		}
		if moveName != "" {
			s += " | " + tr.Tf("tracking.with_move_fmt", moveName)
		}

		s += " " + standardText(tr, maxbattle.Template, g.DefaultTemplateName, maxbattle.Clean)

		if stationID != "" {
			s += " " + tr.Tf("tracking.at_station_fmt", stationNameText)
		}

		return s
	}

	// Specific pokemon maxbattle
	s := fmt.Sprintf("**%s**", name)
	if formName != "" {
		s += " " + tr.Tf("tracking.form_fmt", formName)
	}

	if maxbattle.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", maxbattle.Distance)
	}
	if moveName != "" {
		s += " | " + tr.Tf("tracking.with_move_fmt", moveName)
	}

	s += " " + standardText(tr, maxbattle.Template, g.DefaultTemplateName, maxbattle.Clean)

	if stationID != "" {
		s += " " + tr.Tf("tracking.at_station_fmt", stationNameText)
	}

	return s
}
