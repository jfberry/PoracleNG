package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// TrackedCommand implements !tracked — list all active tracking rules.
type TrackedCommand struct{}

func (c *TrackedCommand) Name() string      { return "cmd.tracked" }
func (c *TrackedCommand) Aliases() []string { return nil }

func (c *TrackedCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// !tracked area — show only area info
	if len(args) > 0 && args[0] == "area" {
		currentAreas := humanAreas(getUserHuman(ctx))
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
	monsters, err := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	raids, err := ctx.Tracking.Raids.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	eggs, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	quests, err := ctx.Tracking.Quests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	invasions, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	lures, err := ctx.Tracking.Lures.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	gyms, err := ctx.Tracking.Gyms.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	nests, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	forts, err := ctx.Tracking.Forts.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	maxbattles, err := ctx.Tracking.Maxbattles.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	return bot.SplitTextReply(text)
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
	h, err := ctx.Humans.Get(ctx.TargetID)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, fmt.Errorf("human %s not found", ctx.TargetID)
	}

	areaJSON, _ := json.Marshal(h.Area)
	blockedJSON, _ := json.Marshal(h.BlockedAlerts)

	// Look up profile name
	profileName := ""
	profiles, err := ctx.Humans.GetProfiles(ctx.TargetID)
	if err == nil {
		for _, p := range profiles {
			if p.ProfileNo == ctx.ProfileNo {
				profileName = p.Name
				break
			}
		}
	}

	return &trackedHuman{
		Enabled:       h.Enabled,
		Latitude:      h.Latitude,
		Longitude:     h.Longitude,
		Area:          string(areaJSON),
		ProfileName:   profileName,
		BlockedAlerts: string(blockedJSON),
	}, nil
}
