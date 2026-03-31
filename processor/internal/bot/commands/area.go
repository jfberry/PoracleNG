package commands

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type AreaCommand struct{}

func (c *AreaCommand) Name() string      { return "cmd.area" }
func (c *AreaCommand) Aliases() []string { return nil }

func (c *AreaCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if len(args) == 0 {
		// Show current areas + usage hint
		currentAreas := getUserAreas(ctx)
		prefix := commandPrefix(ctx)
		var text string
		if len(currentAreas) > 0 {
			displayNames := resolveAreaDisplayNames(ctx, currentAreas)
			text = tr.Tf("status.areas_set", strings.Join(displayNames, ", "))
		} else {
			text = tr.T("status.no_areas")
		}
		text += fmt.Sprintf("\n\nValid commands are `%sarea list`, `%sarea add <areaname>`, `%sarea remove <areaname>`",
			prefix, prefix, prefix)
		return []bot.Reply{{Text: text}}
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list":
		return c.listAreas(ctx)
	case "add":
		return c.addAreas(ctx, rest)
	case "remove":
		return c.removeAreas(ctx, rest)
	case "show":
		return c.showAreas(ctx, rest)
	case "overview":
		return c.overviewAreas(ctx, rest)
	default:
		// Treat all args as area names to add
		return c.addAreas(ctx, args)
	}
}

func (c *AreaCommand) listAreas(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	available := getAvailableAreas(ctx)
	if len(available) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.area.none_available")}}
	}

	// Sort alphabetically
	sort.Slice(available, func(i, j int) bool {
		return strings.ToLower(available[i].Name) < strings.ToLower(available[j].Name)
	})

	// Get user's current areas for marking
	currentAreas := getUserAreas(ctx)
	currentSet := make(map[string]bool)
	for _, a := range currentAreas {
		currentSet[strings.ToLower(a)] = true
	}

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.area.current") + "\n\n")
	for _, a := range available {
		marker := ""
		if currentSet[strings.ToLower(a.Name)] {
			marker = " ✓"
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", a.Name, marker))
	}
	return []bot.Reply{{Text: sb.String()}}
}

func (c *AreaCommand) addAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.area.specify_add")}}
	}

	available := getAvailableAreas(ctx)
	availableMap := make(map[string]string) // lowercase → display name
	for _, a := range available {
		availableMap[strings.ToLower(a.Name)] = a.Name
	}

	currentAreas := getUserAreas(ctx)
	currentSet := make(map[string]bool)
	for _, a := range currentAreas {
		currentSet[strings.ToLower(a)] = true
	}

	var added []string
	var notFound []string
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if displayName, ok := availableMap[lower]; ok {
			if !currentSet[lower] {
				currentAreas = append(currentAreas, lower)
				currentSet[lower] = true
				added = append(added, displayName)
			}
		} else {
			notFound = append(notFound, arg)
		}
	}

	if len(added) > 0 {
		setUserAreas(ctx, currentAreas)
	}

	var parts []string
	if len(added) > 0 {
		parts = append(parts, tr.Tf("cmd.area.added", strings.Join(added, ", ")))
	}
	if len(notFound) > 0 {
		parts = append(parts, tr.Tf("cmd.area.not_found", strings.Join(notFound, ", ")))
	}

	// Show current areas after change
	allDisplay := resolveAreaDisplayNames(ctx, getUserAreas(ctx))
	if len(allDisplay) > 0 {
		parts = append(parts, tr.Tf("status.areas_set", strings.Join(allDisplay, ", ")))
	}

	react := "✅"
	if len(added) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: strings.Join(parts, "\n")}}
}

func (c *AreaCommand) removeAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.area.specify_remove")}}
	}

	currentAreas := getUserAreas(ctx)
	removeSet := make(map[string]bool)
	for _, arg := range args {
		removeSet[strings.ToLower(arg)] = true
	}

	var remaining []string
	var removed []string
	for _, a := range currentAreas {
		if removeSet[strings.ToLower(a)] {
			removed = append(removed, a)
		} else {
			remaining = append(remaining, a)
		}
	}

	if len(removed) > 0 {
		setUserAreas(ctx, remaining)
	}

	var parts []string
	if len(removed) > 0 {
		removedDisplay := resolveAreaDisplayNames(ctx, removed)
		parts = append(parts, tr.Tf("cmd.area.removed", strings.Join(removedDisplay, ", ")))
	}

	// Show current areas after change
	allDisplay := resolveAreaDisplayNames(ctx, getUserAreas(ctx))
	if len(allDisplay) > 0 {
		parts = append(parts, tr.Tf("status.areas_set", strings.Join(allDisplay, ", ")))
	} else {
		parts = append(parts, tr.T("status.no_areas"))
	}

	react := "✅"
	if len(removed) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: strings.Join(parts, "\n")}}
}

func (c *AreaCommand) showAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	areas := getUserAreas(ctx)
	if len(args) > 0 {
		areas = args
	}
	if len(areas) == 0 {
		return []bot.Reply{{Text: tr.T("status.no_areas")}}
	}

	// Generate tiles for each area via processor API
	var replies []bot.Reply
	for _, area := range areas {
		displayName := area
		for _, f := range ctx.Fences {
			if strings.EqualFold(f.Name, area) {
				displayName = f.Name
				break
			}
		}
		// TODO: call tile generation API for area map
		replies = append(replies, bot.Reply{
			Text: tr.Tf("cmd.area.display", displayName),
		})
	}
	return replies
}

func (c *AreaCommand) overviewAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	areas := getUserAreas(ctx)
	if len(args) > 0 {
		areas = args
	}
	if len(areas) == 0 {
		return []bot.Reply{{Text: tr.T("status.no_areas")}}
	}
	displayNames := resolveAreaDisplayNames(ctx, areas)
	// TODO: call tile generation API for overview map
	return []bot.Reply{{Text: tr.Tf("cmd.area.your_areas", strings.Join(displayNames, ", "))}}
}

type availableArea struct {
	Name  string
	Group string
}

func getAvailableAreas(ctx *bot.CommandContext) []availableArea {
	var areas []availableArea
	for _, f := range ctx.Fences {
		if f.UserSelectable {
			areas = append(areas, availableArea{Name: f.Name, Group: f.Group})
		}
	}
	return areas
}

func getUserAreas(ctx *bot.CommandContext) []string {
	var areaJSON *string
	ctx.DB.Get(&areaJSON, "SELECT area FROM humans WHERE id = ? LIMIT 1", ctx.TargetID)
	if areaJSON == nil || *areaJSON == "" || *areaJSON == "[]" {
		return nil
	}
	var areas []string
	json.Unmarshal([]byte(*areaJSON), &areas)
	return areas
}

func setUserAreas(ctx *bot.CommandContext, areas []string) {
	areaJSON, _ := json.Marshal(areas)
	_, err := ctx.DB.Exec("UPDATE humans SET area = ? WHERE id = ?", string(areaJSON), ctx.TargetID)
	if err != nil {
		log.Errorf("area: update areas: %v", err)
	}
	ctx.DB.Exec("UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?",
		string(areaJSON), ctx.TargetID, ctx.ProfileNo)
}

func resolveAreaDisplayNames(ctx *bot.CommandContext, areas []string) []string {
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
	return displayNames
}
