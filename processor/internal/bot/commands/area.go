package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type AreaCommand struct{}

func (c *AreaCommand) Name() string      { return "cmd.area" }
func (c *AreaCommand) Aliases() []string { return nil }

func (c *AreaCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return c.listAreas(ctx)
	}

	// Check first arg for subcommand
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
	available := getAvailableAreas(ctx)
	if len(available) == 0 {
		return []bot.Reply{{Text: "No areas available"}}
	}

	// Get user's current areas
	currentAreas := getUserAreas(ctx)
	currentSet := make(map[string]bool)
	for _, a := range currentAreas {
		currentSet[strings.ToLower(a)] = true
	}

	var sb strings.Builder
	sb.WriteString("**Available areas:**\n")
	for _, a := range available {
		marker := ""
		if currentSet[strings.ToLower(a.Name)] {
			marker = " ✓"
		}
		if a.Group != "" {
			sb.WriteString(fmt.Sprintf("  [%s] %s%s\n", a.Group, a.Name, marker))
		} else {
			sb.WriteString(fmt.Sprintf("  %s%s\n", a.Name, marker))
		}
	}
	return []bot.Reply{{Text: sb.String()}}
}

func (c *AreaCommand) addAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: "Please specify area names to add"}}
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

	var reply string
	if len(added) > 0 {
		reply = "Added: " + strings.Join(added, ", ")
	}
	if len(notFound) > 0 {
		if reply != "" {
			reply += "\n"
		}
		reply += "Not found: " + strings.Join(notFound, ", ")
	}

	react := "✅"
	if len(added) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: reply}}
}

func (c *AreaCommand) removeAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: "Please specify area names to remove"}}
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

	react := "✅"
	if len(removed) == 0 {
		react = "👌"
	}
	text := ""
	if len(removed) > 0 {
		text = "Removed: " + strings.Join(removed, ", ")
	}
	return []bot.Reply{{React: react, Text: text}}
}

func (c *AreaCommand) showAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	// For now, list the user's current areas. Map generation requires tile API access.
	currentAreas := getUserAreas(ctx)
	if len(currentAreas) == 0 {
		return []bot.Reply{{Text: "You have not selected any area yet"}}
	}

	// Resolve display names
	displayNames := resolveAreaDisplayNames(ctx, currentAreas)
	return []bot.Reply{{Text: "Your areas: " + strings.Join(displayNames, ", ")}}
}

func (c *AreaCommand) overviewAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	// Overview map generation requires tile API access — return text for now
	currentAreas := getUserAreas(ctx)
	if len(currentAreas) == 0 {
		return []bot.Reply{{Text: "You have not selected any area yet"}}
	}
	displayNames := resolveAreaDisplayNames(ctx, currentAreas)
	return []bot.Reply{{Text: "Your areas: " + strings.Join(displayNames, ", ")}}
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
	// Also update the current profile
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
