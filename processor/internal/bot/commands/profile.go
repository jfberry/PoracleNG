package commands

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

type ProfileCommand struct{}

func (c *ProfileCommand) Name() string      { return "cmd.profile" }
func (c *ProfileCommand) Aliases() []string { return nil }

func (c *ProfileCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if help := helpArgReply(ctx, args, "msg.profile.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if len(args) == 0 {
		return c.listProfiles(ctx)
	}

	subcommand := args[0]
	rest := args[1:]

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		return subcommand == strings.ToLower(tr.T(key)) || subcommand == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("arg.add"):
		return c.addProfile(ctx, rest)
	case matchSub("arg.remove") || subcommand == "delete":
		return c.removeProfile(ctx, rest)
	case matchSub("arg.switch"):
		return c.switchProfile(ctx, rest)
	case matchSub("arg.list"):
		return c.listProfiles(ctx)
	case matchSub("arg.settime") || subcommand == "hours":
		return c.setTime(ctx, rest)
	case matchSub("arg.cleartime"):
		return c.clearTime(ctx)
	case matchSub("arg.copyto"):
		return c.copyTo(ctx, rest)
	default:
		// Try as switch (profile name or number)
		return c.switchProfile(ctx, args)
	}
}

func (c *ProfileCommand) listProfiles(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	profiles, err := ctx.Humans.GetProfiles(ctx.TargetID)
	if err != nil {
		log.Errorf("profile: list: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if len(profiles) == 0 {
		return []bot.Reply{{Text: tr.T("msg.profile.none") + "\n\n" + inlineUsage(ctx, "msg.profile.usage")}}
	}

	var sb strings.Builder
	sb.WriteString(tr.T("msg.profile.list_header"))
	sb.WriteByte('\n')
	for _, p := range profiles {
		activeMarker := ""
		if p.ProfileNo == ctx.ProfileNo {
			activeMarker = "*"
		}
		sb.WriteString(fmt.Sprintf("%d%s. %s", p.ProfileNo, activeMarker, ctx.EscapeForReply(p.Name)))

		if len(p.Area) > 0 && ctx.AreaLogic != nil {
			displayNames := ctx.AreaLogic.ResolveDisplayNames(p.Area)
			sb.WriteString(" - areas: " + ctx.EscapeJoin(displayNames, ", "))
		} else if len(p.Area) > 0 {
			sb.WriteString(" - areas: " + ctx.EscapeJoin(p.Area, ", "))
		}

		if p.Latitude != 0 || p.Longitude != 0 {
			sb.WriteString(fmt.Sprintf(" - location: %.5f,%.5f", p.Latitude, p.Longitude))
		}
		sb.WriteByte('\n')

		entries, err := db.ParseActiveHours(p.ActiveHours)
		if err == nil && len(entries) > 0 {
			body := formatActiveHours(tr, entries, "\n")
			sb.WriteString("    " + strings.ReplaceAll(body, "\n", "\n    ") + "\n")
		}
	}
	// Append a single timezone-annotation line covering all profiles
	// (they share the human's location). Skip if neither the target
	// nor any profile carries a location — the scheduler will fall
	// back to the operator's default_timezone or the server's local
	// time and FormatScheduleTimezone reports that honestly.
	if ctx.Humans != nil && ctx.Config != nil {
		if human, err := ctx.Humans.Get(ctx.TargetID); err == nil && human != nil {
			sb.WriteString("\n" + FormatScheduleTimezone(tr, human.Latitude, human.Longitude, ctx.Config.General.DefaultTimezone))
		}
	}
	sb.WriteString("\n\n")
	sb.WriteString(inlineUsage(ctx, "msg.profile.usage"))
	return []bot.Reply{{Text: sb.String()}}
}

func (c *ProfileCommand) addProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.specify_name")}}
	}
	name := strings.Join(args, " ")

	if err := ctx.Humans.AddProfile(ctx.TargetID, name, ""); err != nil {
		log.Errorf("profile: add: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Find the new profile number
	newNo := 0
	if profiles, err := ctx.Humans.GetProfiles(ctx.TargetID); err == nil {
		for _, p := range profiles {
			if strings.EqualFold(p.Name, name) && p.ProfileNo > newNo {
				newNo = p.ProfileNo
			}
		}
	}

	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.profile.created", newNo, ctx.EscapeForReply(name))}}
}

func (c *ProfileCommand) removeProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.specify")}}
	}

	profileNo := c.resolveProfileNo(ctx, args[0])
	if profileNo < 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.not_found")}}
	}

	if profileNo == 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.cannot_delete_1")}}
	}

	if err := ctx.Humans.DeleteProfile(ctx.TargetID, profileNo); err != nil {
		log.Errorf("profile: remove: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.profile.deleted", profileNo)}}
}

func (c *ProfileCommand) switchProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.specify")}}
	}

	profileNo := c.resolveProfileNo(ctx, strings.Join(args, " "))
	if profileNo < 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.not_found")}}
	}

	found, err := ctx.Humans.SwitchProfile(ctx.TargetID, profileNo)
	if err != nil {
		log.Errorf("profile: switch: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	if !found {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.profile.not_found")}}
	}

	ctx.TriggerReload()

	// Look up the profile name for the confirmation message
	profileLabel := fmt.Sprintf("%d", profileNo)
	if profiles, err := ctx.Humans.GetProfiles(ctx.TargetID); err == nil {
		for _, p := range profiles {
			if p.ProfileNo == profileNo && p.Name != "" {
				profileLabel = fmt.Sprintf("%d: %s", profileNo, p.Name)
				break
			}
		}
	}

	return []bot.Reply{{React: "✅", Text: tr.Tf("profile.switched", profileLabel)}}
}

// buildDayPrefixMap creates a map from translated + English day prefixes to ISO day numbers.
func buildDayPrefixMap(ctx *bot.CommandContext) map[string][]int {
	m := map[string][]int{
		// English always accepted
		"mon": {1}, "tue": {2}, "wed": {3}, "thu": {4},
		"fri": {5}, "sat": {6}, "sun": {7},
		"weekday": {1, 2, 3, 4, 5},
		"weekend": {6, 7},
		"every":    {1, 2, 3, 4, 5, 6, 7},
		"everyday": {1, 2, 3, 4, 5, 6, 7},
	}

	// Add translated day abbreviations from i18n
	// Uses arg.prefix.mon through arg.prefix.sun and arg.prefix.weekday/weekend
	dayKeys := []struct {
		key  string
		days []int
	}{
		{"arg.prefix.mon", []int{1}},
		{"arg.prefix.tue", []int{2}},
		{"arg.prefix.wed", []int{3}},
		{"arg.prefix.thu", []int{4}},
		{"arg.prefix.fri", []int{5}},
		{"arg.prefix.sat", []int{6}},
		{"arg.prefix.sun", []int{7}},
		{"arg.prefix.weekday", []int{1, 2, 3, 4, 5}},
		{"arg.prefix.weekend", []int{6, 7}},
		{"arg.prefix.every", []int{1, 2, 3, 4, 5, 6, 7}},
		{"arg.prefix.everyday", []int{1, 2, 3, 4, 5, 6, 7}},
	}

	tr := ctx.Tr()
	for _, dk := range dayKeys {
		translated := strings.ToLower(tr.T(dk.key))
		if translated != dk.key && translated != "" {
			m[translated] = dk.days
		}
	}

	return m
}

func (c *ProfileCommand) setTime(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.profile.settime_usage", bot.CommandPrefix(ctx))}}
	}

	dayPrefixes := buildDayPrefixMap(ctx)
	var entries []db.ActiveHourEntry
	for _, arg := range args {
		parsed, err := ParseSettimeArg(arg, dayPrefixes)
		if err != nil {
			// Matched a known form (range) but failed validation
			// — surface the reason instead of silently dropping.
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.profile.settime_invalid", arg, SettimeErrorMessage(tr, err))}}
		}
		entries = append(entries, parsed...)
	}

	if len(entries) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.profile.settime_usage", bot.CommandPrefix(ctx))}}
	}

	data, _ := json.Marshal(entries)

	if err := ctx.Humans.UpdateProfileHours(ctx.TargetID, ctx.ProfileNo, string(data)); err != nil {
		log.Errorf("profile: settime: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.profile.hours_set", ctx.ProfileNo)}}
}

// clearTime removes the profile's auto-switch schedule by writing an
// empty active_hours value. ParseActiveHours treats `""` (and `"[]"` /
// `"{}"`) as "no schedule", so the profile scheduler simply stops
// auto-switching to this profile — the profile itself is untouched.
// Mirrors `!summary cleartime` for symmetry.
func (c *ProfileCommand) clearTime(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	if err := ctx.Humans.UpdateProfileHours(ctx.TargetID, ctx.ProfileNo, ""); err != nil {
		log.Errorf("profile: cleartime: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.profile.hours_cleared", ctx.ProfileNo)}}
}

func (c *ProfileCommand) copyTo(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.profile.copyto_usage", bot.CommandPrefix(ctx))}}
	}

	// Load all profiles for this user.
	profiles, err := ctx.Humans.GetProfiles(ctx.TargetID)
	if err != nil {
		log.Errorf("profile: copyto: load profiles: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Determine which target profiles match.
	var valid []string
	var invalid []string

	for _, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "all" {
			valid = append(valid, "all")
			continue
		}
		found := false
		for _, p := range profiles {
			if strings.ToLower(p.Name) == lower && p.ProfileNo != ctx.ProfileNo {
				found = true
				break
			}
		}
		if found {
			valid = append(valid, arg)
		} else {
			invalid = append(invalid, arg)
		}
	}

	// Copy tracking to each matching profile.
	hasAll := false
	for _, v := range valid {
		if strings.ToLower(v) == "all" {
			hasAll = true
			break
		}
	}

	copiedNames := make([]string, 0)
	for _, p := range profiles {
		if p.ProfileNo == ctx.ProfileNo {
			continue
		}
		nameMatch := false
		for _, v := range valid {
			if strings.ToLower(v) == strings.ToLower(p.Name) {
				nameMatch = true
				break
			}
		}
		if !hasAll && !nameMatch {
			continue
		}
		if err := ctx.Humans.CopyProfile(ctx.TargetID, ctx.ProfileNo, p.ProfileNo); err != nil {
			log.Errorf("profile: copyto %s profile %d: %v", ctx.TargetID, p.ProfileNo, err)
			return []bot.Reply{{React: "🙅"}}
		}
		copiedNames = append(copiedNames, p.Name)
	}

	var parts []string
	if len(copiedNames) > 0 {
		parts = append(parts, tr.Tf("msg.profile.copied", strings.Join(copiedNames, ", ")))
	}
	if len(invalid) > 0 {
		parts = append(parts, tr.Tf("msg.profile.copy_invalid", strings.Join(invalid, ", ")))
	}

	if len(copiedNames) == 0 && len(invalid) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.profile.copyto_usage", bot.CommandPrefix(ctx))}}
	}

	ctx.TriggerReload()

	react := "✅"
	if len(copiedNames) == 0 {
		react = "🙅"
	}
	return []bot.Reply{{React: react, Text: strings.Join(parts, "\n")}}
}

func (c *ProfileCommand) resolveProfileNo(ctx *bot.CommandContext, input string) int {
	profiles, err := ctx.Humans.GetProfiles(ctx.TargetID)
	if err != nil {
		return -1
	}

	// Try as number
	if n, err := strconv.Atoi(input); err == nil {
		for _, p := range profiles {
			if p.ProfileNo == n {
				return n
			}
		}
	}

	// Try as name
	lower := strings.ToLower(input)
	for _, p := range profiles {
		if strings.ToLower(p.Name) == lower {
			return p.ProfileNo
		}
	}

	return -1
}
