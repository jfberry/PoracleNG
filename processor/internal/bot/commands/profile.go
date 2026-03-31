package commands

import (
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
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

	switch subcommand {
	case "add":
		return c.addProfile(ctx, rest)
	case "remove", "delete":
		return c.removeProfile(ctx, rest)
	case "switch":
		return c.switchProfile(ctx, rest)
	case "list":
		return c.listProfiles(ctx)
	default:
		// Try as switch (profile name or number)
		return c.switchProfile(ctx, args)
	}
}

func (c *ProfileCommand) listProfiles(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	var profiles []struct {
		ProfileNo int    `db:"profile_no"`
		Name      string `db:"name"`
	}
	err := ctx.DB.Select(&profiles,
		"SELECT profile_no, name FROM profiles WHERE id = ? ORDER BY profile_no",
		ctx.TargetID)
	if err != nil {
		log.Errorf("profile: list: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if len(profiles) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.profile.none")}}
	}

	var sb strings.Builder
	for _, p := range profiles {
		marker := ""
		if p.ProfileNo == ctx.ProfileNo {
			marker = " ← " + tr.T("cmd.profile.active")
		}
		sb.WriteString(fmt.Sprintf("%d: %s%s\n", p.ProfileNo, p.Name, marker))
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
