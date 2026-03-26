package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// RaidRowText generates a human-readable description of a raid tracking rule.
func (g *Generator) RaidRowText(tr *i18n.Translator, raid *db.RaidTracking) string {
	name, formName := translateMonsterName(tr, g.GD, raid.PokemonID, raid.Form)
	raidTeam := tr.T(fmt.Sprintf("team_%d", raid.Team))
	moveName := translateMoveName(tr, g.GD, raid.Move)

	gymID := ""
	if raid.GymID.Valid {
		gymID = raid.GymID.String
	}
	gymNameText := resolveGymName(g.Scanner, gymID)

	rsvp := rsvpText(tr, raid.RSVPChanges)

	if raid.PokemonID == 9000 {
		// Generic level raid
		var levelText string
		if raid.Level == 90 {
			levelText = tr.T("tracking.all_level_raids")
		} else {
			levelText = tr.Tf("tracking.level_n_raids", raid.Level)
		}

		s := fmt.Sprintf("**%s**", levelText)

		if raid.Distance != 0 {
			s += " | " + tr.Tf("tracking.distance_fmt", raid.Distance)
		}
		if moveName != "" {
			s += " | " + tr.Tf("tracking.with_move_fmt", moveName)
		}
		if raid.Team != 4 {
			s += " | " + tr.Tf("tracking.controlled_by_fmt", raidTeam)
		}
		if raid.Exclusive {
			s += " | " + tr.T("tracking.must_be_ex")
		}

		s += " " + standardText(tr, raid.Template, g.DefaultTemplateName, raid.Clean)

		if gymID != "" {
			s += " " + tr.Tf("tracking.at_gym_fmt", gymNameText)
		}

		s += " " + rsvp
		return s
	}

	// Specific pokemon raid
	s := fmt.Sprintf("**%s**", name)
	if formName != "" {
		s += " " + tr.Tf("tracking.form_fmt", formName)
	}

	if raid.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", raid.Distance)
	}
	if moveName != "" {
		s += " | " + tr.Tf("tracking.with_move_fmt", moveName)
	}
	if raid.Team != 4 {
		s += " | " + tr.Tf("tracking.controlled_by_fmt", raidTeam)
	}
	if raid.Exclusive {
		s += " | " + tr.T("tracking.must_be_ex")
	}

	s += " " + standardText(tr, raid.Template, g.DefaultTemplateName, raid.Clean)

	if gymID != "" {
		s += " " + tr.Tf("tracking.at_gym_fmt", gymNameText)
	}

	s += " " + rsvp
	return s
}

// EggRowText generates a human-readable description of an egg tracking rule.
func (g *Generator) EggRowText(tr *i18n.Translator, egg *db.EggTracking) string {
	raidTeam := tr.T(fmt.Sprintf("team_%d", egg.Team))

	gymID := ""
	if egg.GymID.Valid {
		gymID = egg.GymID.String
	}
	gymNameText := resolveGymName(g.Scanner, gymID)

	rsvp := rsvpText(tr, egg.RSVPChanges)

	var levelText string
	if egg.Level == 90 {
		levelText = tr.T("tracking.all_level_eggs")
	} else {
		levelText = tr.Tf("tracking.level_n_eggs", egg.Level)
	}

	s := fmt.Sprintf("**%s**", levelText)

	if egg.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", egg.Distance)
	}

	if egg.Team != 4 {
		s += " | " + tr.Tf("tracking.controlled_by_fmt", raidTeam)
	}
	if egg.Exclusive {
		s += " | " + tr.T("tracking.must_be_ex")
	}

	s += " " + standardText(tr, egg.Template, g.DefaultTemplateName, egg.Clean)

	if gymID != "" {
		s += " " + tr.Tf("tracking.at_gym_fmt", gymNameText)
	}

	s += " " + rsvp
	return s
}
