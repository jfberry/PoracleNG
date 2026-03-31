package commands

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	case matchSub("arg.copyto"):
		return c.copyTo(ctx, rest)
	default:
		// Try as switch (profile name or number)
		return c.switchProfile(ctx, args)
	}
}

func (c *ProfileCommand) listProfiles(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var profiles []struct {
		ProfileNo   int     `db:"profile_no"`
		Name        string  `db:"name"`
		ActiveHours string  `db:"active_hours"`
		Area        string  `db:"area"`
		Latitude    float64 `db:"latitude"`
		Longitude   float64 `db:"longitude"`
	}
	err := ctx.DB.Select(&profiles,
		"SELECT profile_no, name, COALESCE(active_hours, '') AS active_hours, COALESCE(area, '[]') AS area, COALESCE(latitude, 0) AS latitude, COALESCE(longitude, 0) AS longitude FROM profiles WHERE id = ? ORDER BY profile_no",
		ctx.TargetID)
	if err != nil {
		log.Errorf("profile: list: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if len(profiles) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.profile.none")}}
	}

	dayKeys := []string{
		"day.monday", "day.tuesday", "day.wednesday",
		"day.thursday", "day.friday", "day.saturday", "day.sunday",
	}

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.profile.list_header"))
	sb.WriteByte('\n')
	for _, p := range profiles {
		activeMarker := ""
		if p.ProfileNo == ctx.ProfileNo {
			activeMarker = "*"
		}
		sb.WriteString(fmt.Sprintf("%d%s. %s", p.ProfileNo, activeMarker, p.Name))

		if p.Area != "" && p.Area != "[]" {
			sb.WriteString(fmt.Sprintf(" - areas: %s", p.Area))
		}

		if p.Latitude != 0 || p.Longitude != 0 {
			sb.WriteString(fmt.Sprintf(" - location: %.5f,%.5f", p.Latitude, p.Longitude))
		}
		sb.WriteByte('\n')

		// Decode and display active hours
		if p.ActiveHours != "" && p.ActiveHours != "[]" {
			var hours []struct {
				Day   int    `json:"day"`
				Hours string `json:"hours"`
				Mins  string `json:"mins"`
			}
			if err := json.Unmarshal([]byte(p.ActiveHours), &hours); err == nil {
				for _, h := range hours {
					dayName := ""
					if h.Day >= 1 && h.Day <= 7 {
						dayName = tr.T(dayKeys[h.Day-1])
					}
					sb.WriteString(fmt.Sprintf("    %s %s:%s\n", dayName, h.Hours, h.Mins))
				}
			}
		}
	}
	return []bot.Reply{{Text: sb.String()}}
}

func (c *ProfileCommand) addProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.specify_name")}}
	}
	name := strings.Join(args, " ")

	// Find next profile number
	var maxNo int
	ctx.DB.Get(&maxNo, "SELECT COALESCE(MAX(profile_no), 0) FROM profiles WHERE id = ?", ctx.TargetID)
	newNo := maxNo + 1

	// Get user's current location/area for the new profile
	var human struct {
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	ctx.DB.Get(&human, "SELECT latitude, longitude, area FROM humans WHERE id = ? LIMIT 1", ctx.TargetID)

	area := "[]"
	if human.Area != nil {
		area = *human.Area
	}

	_, err := ctx.DB.Exec(
		"INSERT INTO profiles (id, profile_no, name, area, latitude, longitude) VALUES (?, ?, ?, ?, ?, ?)",
		ctx.TargetID, newNo, name, area, human.Latitude, human.Longitude)
	if err != nil {
		log.Errorf("profile: add: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.profile.created", newNo, name)}}
}

func (c *ProfileCommand) removeProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.specify")}}
	}

	profileNo := c.resolveProfileNo(ctx, args[0])
	if profileNo < 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.not_found")}}
	}

	if profileNo == 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.cannot_delete_1")}}
	}

	// Delete all tracking for this profile
	for _, table := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "nests", "gym", "forts", "maxbattle"} {
		ctx.DB.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ? AND profile_no = ?", table), ctx.TargetID, profileNo)
	}

	// Delete the profile
	ctx.DB.Exec("DELETE FROM profiles WHERE id = ? AND profile_no = ?", ctx.TargetID, profileNo)

	// If this was the active profile, switch to 1
	if profileNo == ctx.ProfileNo {
		ctx.DB.Exec("UPDATE humans SET current_profile_no = 1 WHERE id = ?", ctx.TargetID)
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.profile.deleted", profileNo)}}
}

func (c *ProfileCommand) switchProfile(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.specify")}}
	}

	profileNo := c.resolveProfileNo(ctx, strings.Join(args, " "))
	if profileNo < 1 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.not_found")}}
	}

	_, err := ctx.DB.Exec("UPDATE humans SET current_profile_no = ? WHERE id = ?", profileNo, ctx.TargetID)
	if err != nil {
		log.Errorf("profile: switch: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	return []bot.Reply{{React: "✅", Text: tr.Tf("profile.switched", profileNo)}}
}

// settimeRe matches day-prefix + hours:mins in multiple formats:
// mon09:00, mon:09:00, mon09, mon:09
var settimeRe = regexp.MustCompile(`^([a-zA-Z]+?):?(\d{1,2}):?(\d{2})?$`)

// buildDayPrefixMap creates a map from translated + English day prefixes to ISO day numbers.
func buildDayPrefixMap(ctx *bot.CommandContext) map[string][]int {
	m := map[string][]int{
		// English always accepted
		"mon": {1}, "tue": {2}, "wed": {3}, "thu": {4},
		"fri": {5}, "sat": {6}, "sun": {7},
		"weekday": {1, 2, 3, 4, 5},
		"weekend": {6, 7},
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
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.settime_usage")}}
	}

	// Parse day:time patterns from all args.
	type entry struct {
		Day   int    `json:"day"`
		Hours string `json:"hours"`
		Mins  string `json:"mins"`
	}
	var entries []entry

	dayPrefixes := buildDayPrefixMap(ctx)

	for _, arg := range args {
		m := settimeRe.FindStringSubmatch(strings.ToLower(arg))
		if m == nil {
			continue
		}
		prefix := m[1]
		hours := m[2]
		mins := m[3]
		if mins == "" {
			mins = "00"
		}

		days, ok := dayPrefixes[prefix]
		if !ok {
			continue
		}
		for _, d := range days {
			entries = append(entries, entry{Day: d, Hours: hours, Mins: mins})
		}
	}

	if len(entries) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.settime_usage")}}
	}

	data, _ := json.Marshal(entries)

	if err := db.UpdateProfileHours(ctx.DB, ctx.TargetID, ctx.ProfileNo, string(data)); err != nil {
		log.Errorf("profile: settime: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.profile.hours_set", ctx.ProfileNo)}}
}

func (c *ProfileCommand) copyTo(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.copyto_usage")}}
	}

	// Load all profiles for this user.
	var profiles []struct {
		ProfileNo int    `db:"profile_no"`
		Name      string `db:"name"`
	}
	if err := ctx.DB.Select(&profiles,
		"SELECT profile_no, name FROM profiles WHERE id = ? ORDER BY profile_no",
		ctx.TargetID); err != nil {
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
		if err := db.CopyProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo, p.ProfileNo); err != nil {
			log.Errorf("profile: copyto %s profile %d: %v", ctx.TargetID, p.ProfileNo, err)
			return []bot.Reply{{React: "🙅"}}
		}
		copiedNames = append(copiedNames, p.Name)
	}

	var parts []string
	if len(copiedNames) > 0 {
		parts = append(parts, tr.Tf("cmd.profile.copied", strings.Join(copiedNames, ", ")))
	}
	if len(invalid) > 0 {
		parts = append(parts, tr.Tf("cmd.profile.copy_invalid", strings.Join(invalid, ", ")))
	}

	if len(copiedNames) == 0 && len(invalid) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.profile.copyto_usage")}}
	}

	ctx.TriggerReload()

	react := "✅"
	if len(copiedNames) == 0 {
		react = "🙅"
	}
	return []bot.Reply{{React: react, Text: strings.Join(parts, "\n")}}
}

func (c *ProfileCommand) resolveProfileNo(ctx *bot.CommandContext, input string) int {
	// Try as number
	if n, err := strconv.Atoi(input); err == nil {
		var count int
		ctx.DB.Get(&count, "SELECT COUNT(*) FROM profiles WHERE id = ? AND profile_no = ?", ctx.TargetID, n)
		if count > 0 {
			return n
		}
	}

	// Try as name
	var profileNo int
	err := ctx.DB.Get(&profileNo,
		"SELECT profile_no FROM profiles WHERE id = ? AND LOWER(name) = ? LIMIT 1",
		ctx.TargetID, strings.ToLower(input))
	if err == nil {
		return profileNo
	}

	return -1
}
