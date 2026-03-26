package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// GymRowText generates a human-readable description of a gym tracking rule.
func (g *Generator) GymRowText(tr *i18n.Translator, gym *db.GymTracking) string {
	var teamGyms string
	if gym.Team == 4 {
		teamGyms = tr.T("tracking.all_teams_gyms")
	} else {
		teamName := tr.T(fmt.Sprintf("team_%d", gym.Team))
		teamGyms = tr.Tf("tracking.team_gyms_fmt", teamName)
	}

	gymID := ""
	if gym.GymID != nil {
		gymID = *gym.GymID
	}
	gymNameText := resolveGymName(g.Scanner, gymID)

	s := fmt.Sprintf("**%s**", teamGyms)

	if gym.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", gym.Distance)
	}

	if gym.SlotChanges {
		s += " | " + tr.T("tracking.including_slot_changes")
	}

	if gym.BattleChanges {
		s += " | " + tr.T("tracking.including_battle_changes")
	}

	s += " " + standardText(tr, gym.Template, g.DefaultTemplateName, gym.Clean)

	if gymID != "" {
		s += " " + tr.Tf("tracking.at_gym_fmt", gymNameText)
	}

	return s
}
