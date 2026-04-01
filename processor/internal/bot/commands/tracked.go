package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// TrackedCommand implements !tracked — list all active tracking rules.
type TrackedCommand struct{}

func (c *TrackedCommand) Name() string      { return "cmd.tracked" }
func (c *TrackedCommand) Aliases() []string { return nil }

func (c *TrackedCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// !tracked area — show only area info
	if len(args) > 0 && args[0] == "area" {
		currentAreas := getUserAreas(ctx)
		if len(currentAreas) > 0 {
			displayNames := ctx.AreaLogic.ResolveDisplayNames(currentAreas)
			return []bot.Reply{{Text: tr.Tf("status.areas_set", strings.Join(displayNames, ", "))}}
		}
		return []bot.Reply{{Text: tr.T("status.no_areas")}}
	}

	var sb strings.Builder

	// Header: human status, location, area, profile
	human, err := lookupHumanForTracked(ctx)
	if err != nil {
		log.Errorf("tracked: lookup human: %v", err)
	}

	if human != nil {
		// Enabled/disabled status
		enabledText := tr.T("status.enabled")
		if !human.Enabled {
			enabledText = tr.T("status.disabled")
		}
		sb.WriteString(tr.Tf("status.alerts_currently", enabledText) + "\n")

		// Location
		if human.Latitude != 0 || human.Longitude != 0 {
			mapLink := fmt.Sprintf("https://maps.google.com/maps?q=%f,%f", human.Latitude, human.Longitude)
			sb.WriteString(tr.Tf("status.location_set", mapLink) + "\n")
		} else {
			sb.WriteString(tr.T("status.no_location") + "\n")
		}

		// Area
		if human.Area != "" && human.Area != "[]" {
			var areas []string
			json.Unmarshal([]byte(human.Area), &areas)
			if len(areas) > 0 {
				// Resolve display names from geofence
				displayNames := make([]string, 0, len(areas))
				for _, a := range areas {
					found := false
					for _, f := range ctx.Fences {
						if strings.EqualFold(f.Name, a) {
							displayNames = append(displayNames, f.Name)
							found = true
							break
						}
					}
					if !found {
						displayNames = append(displayNames, a)
					}
				}
				sb.WriteString(tr.Tf("status.areas_set", strings.Join(displayNames, ", ")) + "\n")
			}
		} else {
			sb.WriteString(tr.T("status.no_areas") + "\n")
		}

		// Profile
		if human.ProfileName != "" {
			sb.WriteString(tr.Tf("status.profile_set", human.ProfileName) + "\n")
		}

		// Blocked alerts
		if human.BlockedAlerts != "" && human.BlockedAlerts != "[]" {
			var blocked []string
			json.Unmarshal([]byte(human.BlockedAlerts), &blocked)
			if len(blocked) > 0 {
				sb.WriteString(tr.Tf("status.blocked_alerts", strings.Join(blocked, ", ")) + "\n")
			}
		}

		sb.WriteByte('\n')
	}

	// Pokemon
	monsters, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select monsters: %v", err)
	} else if len(monsters) > 0 {
		sb.WriteString(tr.T("section.pokemon") + "\n")
		for i := range monsters {
			mt := monsterAPIToTracking(&monsters[i])
			sb.WriteString(ctx.RowText.MonsterRowText(tr, mt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Raids
	raids, err := db.SelectRaidsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select raids: %v", err)
	} else if len(raids) > 0 {
		sb.WriteString(tr.T("section.raids") + "\n")
		for i := range raids {
			rt := raidAPIToTracking(&raids[i])
			sb.WriteString(ctx.RowText.RaidRowText(tr, rt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Eggs
	eggs, err := db.SelectEggsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select eggs: %v", err)
	} else if len(eggs) > 0 {
		sb.WriteString(tr.T("section.eggs") + "\n")
		for i := range eggs {
			et := eggAPIToTracking(&eggs[i])
			sb.WriteString(ctx.RowText.EggRowText(tr, et))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Quests
	quests, err := db.SelectQuestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select quests: %v", err)
	} else if len(quests) > 0 {
		sb.WriteString(tr.T("section.quests") + "\n")
		for i := range quests {
			qt := questAPIToTracking(&quests[i])
			sb.WriteString(ctx.RowText.QuestRowText(tr, qt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Invasions
	invasions, err := db.SelectInvasionsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select invasions: %v", err)
	} else if len(invasions) > 0 {
		sb.WriteString(tr.T("section.invasions") + "\n")
		for i := range invasions {
			it := invasionAPIToTracking(&invasions[i])
			sb.WriteString(ctx.RowText.InvasionRowText(tr, it))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Lures
	lures, err := db.SelectLuresByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select lures: %v", err)
	} else if len(lures) > 0 {
		sb.WriteString(tr.T("section.lures") + "\n")
		for i := range lures {
			lt := lureAPIToTracking(&lures[i])
			sb.WriteString(ctx.RowText.LureRowText(tr, lt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Gyms
	gyms, err := db.SelectGymsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select gyms: %v", err)
	} else if len(gyms) > 0 {
		sb.WriteString(tr.T("section.gyms") + "\n")
		for i := range gyms {
			gt := gymAPIToTracking(&gyms[i])
			sb.WriteString(ctx.RowText.GymRowText(tr, gt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Nests
	nests, err := db.SelectNestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select nests: %v", err)
	} else if len(nests) > 0 {
		sb.WriteString(tr.T("section.nests") + "\n")
		for i := range nests {
			nt := nestAPIToTracking(&nests[i])
			sb.WriteString(ctx.RowText.NestRowText(tr, nt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Forts
	forts, err := db.SelectFortsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select forts: %v", err)
	} else if len(forts) > 0 {
		sb.WriteString(tr.T("section.forts") + "\n")
		for i := range forts {
			ft := fortAPIToTracking(&forts[i])
			sb.WriteString(ctx.RowText.FortUpdateRowText(tr, ft))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	// Maxbattles
	maxbattles, err := db.SelectMaxbattlesByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tracked: select maxbattles: %v", err)
	} else if len(maxbattles) > 0 {
		sb.WriteString(tr.T("section.maxbattles") + "\n")
		for i := range maxbattles {
			mt := maxbattleAPIToTracking(&maxbattles[i])
			sb.WriteString(ctx.RowText.MaxbattleRowText(tr, mt))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		text = tr.T("status.no_tracking")
	}

	// If too long for a single message, send as file attachment
	maxLen := 2000
	if ctx.Platform == "telegram" {
		maxLen = 4095
	}
	if len(text) > maxLen {
		return []bot.Reply{{
			Text: tr.T("status.tracking_file"),
			Attachment: &bot.Attachment{
				Filename: "tracked.txt",
				Content:  []byte(text),
			},
		}}
	}

	return []bot.Reply{{Text: text}}
}

type trackedHuman struct {
	Enabled       bool
	Latitude      float64
	Longitude     float64
	Area          string
	ProfileName   string
	BlockedAlerts string
}

func lookupHumanForTracked(ctx *bot.CommandContext) (*trackedHuman, error) {
	var h struct {
		Enabled       bool    `db:"enabled"`
		Latitude      float64 `db:"latitude"`
		Longitude     float64 `db:"longitude"`
		Area          *string `db:"area"`
		BlockedAlerts *string `db:"blocked_alerts"`
	}
	err := ctx.DB.Get(&h,
		"SELECT enabled, latitude, longitude, area, blocked_alerts FROM humans WHERE id = ? LIMIT 1",
		ctx.TargetID)
	if err != nil {
		return nil, err
	}

	area := ""
	if h.Area != nil {
		area = *h.Area
	}
	blocked := ""
	if h.BlockedAlerts != nil {
		blocked = *h.BlockedAlerts
	}

	// Look up profile name
	profileName := ""
	var pn struct {
		Name string `db:"name"`
	}
	err = ctx.DB.Get(&pn,
		"SELECT name FROM profiles WHERE id = ? AND profile_no = ?",
		ctx.TargetID, ctx.ProfileNo)
	if err == nil {
		profileName = pn.Name
	}

	return &trackedHuman{
		Enabled:       h.Enabled,
		Latitude:      h.Latitude,
		Longitude:     h.Longitude,
		Area:          area,
		ProfileName:   profileName,
		BlockedAlerts: blocked,
	}, nil
}
