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
			mapLink := fmt.Sprintf("<https://maps.google.com/maps?q=%f,%f>", human.Latitude, human.Longitude)
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

	// Helper: append ⚠️ to a row if the tracking has a distance/area issue
	hasLocationWarning := false
	hasAreaWarning := false
	warnRow := func(rowText string, distance int) string {
		if distance > 0 && !ctx.HasLocation {
			hasLocationWarning = true
			return rowText + " ⚠️"
		}
		if distance == 0 && !ctx.HasArea {
			hasAreaWarning = true
			return rowText + " ⚠️"
		}
		return rowText
	}

	// Each tracking category: show rules or "not tracking X" (unless disabled in config)
	cfg := ctx.Config.General

	// First pass: collect max UID across all types for padding width
	maxUID := int64(0)
	updateMaxUID := func(uid int64) {
		if uid > maxUID {
			maxUID = uid
		}
	}

	// Pre-query all types to find max UID (queries are reused below)
	var monsters []db.MonsterTrackingAPI
	var raidList []db.RaidTrackingAPI
	var eggList []db.EggTrackingAPI
	var questList []db.QuestTrackingAPI
	var invasionList []db.InvasionTrackingAPI
	var lureList []db.LureTrackingAPI
	var gymList []db.GymTrackingAPI
	var nestList []db.NestTrackingAPI
	var fortList []db.FortTrackingAPI
	var maxbattleList []db.MaxbattleTrackingAPI

	if !cfg.DisablePokemon {
		if v, err := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			monsters = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableRaid {
		if v, err := ctx.Tracking.Raids.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			raidList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
		if v, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			eggList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableQuest {
		if v, err := ctx.Tracking.Quests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			questList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableInvasion {
		if v, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			invasionList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableLure {
		if v, err := ctx.Tracking.Lures.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			lureList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableGym {
		if v, err := ctx.Tracking.Gyms.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			gymList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableNest {
		if v, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			nestList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableFortUpdate {
		if v, err := ctx.Tracking.Forts.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			fortList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}
	if !cfg.DisableMaxBattle {
		if v, err := ctx.Tracking.Maxbattles.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo); err == nil {
			maxbattleList = v
			for i := range v {
				updateMaxUID(v[i].UID)
			}
		}
	}

	// Compute padding width from max UID
	idWidth := max(len(fmt.Sprintf("%d", maxUID)), 1)
	fmtID := func(uid int64) string {
		return fmt.Sprintf("[id:%*d] ", idWidth, uid)
	}
	hasAnyRules := maxUID > 0

	if !cfg.DisablePokemon {
		if len(monsters) > 0 {
			sb.WriteString(tr.T("section.pokemon") + "\n")
			for i := range monsters {
				sb.WriteString(fmtID(monsters[i].UID))
				sb.WriteString(warnRow(ctx.RowText.MonsterRowText(tr, monsterAPIToTracking(&monsters[i])), monsters[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.pokemon.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableRaid {
		if len(raidList) > 0 {
			sb.WriteString(tr.T("section.raids") + "\n")
			for i := range raidList {
				sb.WriteString(fmtID(raidList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.RaidRowText(tr, raidAPIToTracking(&raidList[i])), raidList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.raids.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableRaid {
		if len(eggList) > 0 {
			sb.WriteString(tr.T("section.eggs") + "\n")
			for i := range eggList {
				sb.WriteString(fmtID(eggList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.EggRowText(tr, eggAPIToTracking(&eggList[i])), eggList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.eggs.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableQuest {
		if len(questList) > 0 {
			sb.WriteString(tr.T("section.quests") + "\n")
			for i := range questList {
				sb.WriteString(fmtID(questList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.QuestRowText(tr, questAPIToTracking(&questList[i])), questList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.quests.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableInvasion {
		if len(invasionList) > 0 {
			sb.WriteString(tr.T("section.invasions") + "\n")
			for i := range invasionList {
				sb.WriteString(fmtID(invasionList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&invasionList[i])), invasionList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.invasions.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableLure {
		if len(lureList) > 0 {
			sb.WriteString(tr.T("section.lures") + "\n")
			for i := range lureList {
				sb.WriteString(fmtID(lureList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.LureRowText(tr, lureAPIToTracking(&lureList[i])), lureList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.lures.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableGym {
		if len(gymList) > 0 {
			sb.WriteString(tr.T("section.gyms") + "\n")
			for i := range gymList {
				sb.WriteString(fmtID(gymList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.GymRowText(tr, gymAPIToTracking(&gymList[i])), gymList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.gyms.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableNest {
		if len(nestList) > 0 {
			sb.WriteString(tr.T("section.nests") + "\n")
			for i := range nestList {
				sb.WriteString(fmtID(nestList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.NestRowText(tr, nestAPIToTracking(&nestList[i])), nestList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.nests.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableFortUpdate {
		if len(fortList) > 0 {
			sb.WriteString(tr.T("section.forts") + "\n")
			for i := range fortList {
				sb.WriteString(fmtID(fortList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&fortList[i])), fortList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.forts.none"))
		}
		sb.WriteByte('\n')
	}

	if !cfg.DisableMaxBattle {
		if len(maxbattleList) > 0 {
			sb.WriteString(tr.T("section.maxbattles") + "\n")
			for i := range maxbattleList {
				sb.WriteString(fmtID(maxbattleList[i].UID))
				sb.WriteString(warnRow(ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&maxbattleList[i])), maxbattleList[i].Distance))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(tr.T("section.maxbattles.none"))
		}
		sb.WriteByte('\n')
	}

	// Hint about id-based removal
	if hasAnyRules {
		prefix := bot.CommandPrefix(ctx)
		sb.WriteString("\n" + tr.Tf("tracking.id_hint", prefix, prefix))
	}

	// Summary warnings at the end
	if hasLocationWarning {
		sb.WriteString("\n⚠️ " + tr.Tf("tracking.warn_no_location", bot.CommandPrefix(ctx)))
	}
	if hasAreaWarning {
		sb.WriteString("\n⚠️ " + tr.Tf("tracking.warn_no_area", bot.CommandPrefix(ctx)))
	}
	if human != nil && !human.Enabled {
		prefix := bot.CommandPrefix(ctx)
		sb.WriteString("\n⚠️ " + tr.Tf("tracking.warn_stopped", prefix))
	}

	return bot.SplitTextReply(strings.TrimSpace(sb.String()))
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
