package commands

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
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
			displayNames := ctx.AreaLogic.ResolveDisplayNames(currentAreas)
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

	// Match subcommands against translated keywords + English fallback
	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("arg.list"):
		return c.listAreas(ctx)
	case matchSub("arg.add"):
		return c.addAreas(ctx, rest)
	case matchSub("arg.remove"):
		return c.removeAreas(ctx, rest)
	case matchSub("arg.show"):
		return c.showAreas(ctx, rest)
	case matchSub("arg.overview"):
		return c.overviewAreas(ctx, rest)
	default:
		// Treat all args as area names to add
		return c.addAreas(ctx, args)
	}
}

func (c *AreaCommand) listAreas(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	currentAreas := getUserAreas(ctx)
	communities := getUserCommunities(ctx)
	available := ctx.AreaLogic.GetAvailableAreasMarked(communities, currentAreas)
	if len(available) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.area.none_available")}}
	}

	// Sort alphabetically
	sort.Slice(available, func(i, j int) bool {
		return strings.ToLower(available[i].Name) < strings.ToLower(available[j].Name)
	})

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.area.current") + "\n\n")
	for _, a := range available {
		if a.IsActive {
			sb.WriteString(fmt.Sprintf("🟢 %s\n", a.Name))
		} else {
			sb.WriteString(fmt.Sprintf("◽ %s\n", a.Name))
		}
	}
	return []bot.Reply{{Text: sb.String()}}
}

func (c *AreaCommand) addAreas(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.area.specify_add")}}
	}

	currentAreas := getUserAreas(ctx)
	communities := getUserCommunities(ctx)
	added, notFound, newList := ctx.AreaLogic.AddAreas(currentAreas, communities, args)

	if len(added) > 0 {
		setUserAreas(ctx, newList)
	}

	var parts []string
	if len(added) > 0 {
		parts = append(parts, tr.Tf("cmd.area.added", strings.Join(added, ", ")))
	}
	if len(notFound) > 0 {
		parts = append(parts, tr.Tf("cmd.area.not_found", strings.Join(notFound, ", ")))
	}

	// Show current areas after change
	allDisplay := ctx.AreaLogic.ResolveDisplayNames(getUserAreas(ctx))
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
	removed, remaining := ctx.AreaLogic.RemoveAreas(currentAreas, args)

	if len(removed) > 0 {
		setUserAreas(ctx, remaining)
	}

	var parts []string
	if len(removed) > 0 {
		removedDisplay := ctx.AreaLogic.ResolveDisplayNames(removed)
		parts = append(parts, tr.Tf("cmd.area.removed", strings.Join(removedDisplay, ", ")))
	}

	// Show current areas after change
	allDisplay := ctx.AreaLogic.ResolveDisplayNames(getUserAreas(ctx))
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

	var replies []bot.Reply
	for _, area := range areas {
		displayName := area
		fence := ctx.AreaLogic.FindFence(area)
		if fence != nil {
			displayName = fence.Name
		}

		reply := bot.Reply{Text: tr.Tf("cmd.area.display", displayName)}

		// Generate tile if static map is available and fence found
		if ctx.StaticMap != nil && fence != nil {
			paths := api.FencePaths(fence)
			if len(paths) > 0 {
				pos := staticmap.Autoposition(staticmap.AutopositionShape{
					Polygons: api.FenceAutopositionPolygons(paths),
				}, 500, 250, 1.25, 17.5)
				if pos != nil {
					data := map[string]any{
						"zoom":      pos.Zoom,
						"latitude":  pos.Latitude,
						"longitude": pos.Longitude,
						"polygons":  paths,
					}
					reply.ImageURL = ctx.StaticMap.GetPregeneratedTileURL("area", data, "staticMap")
				}
			}
		}

		replies = append(replies, reply)
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
	displayNames := ctx.AreaLogic.ResolveDisplayNames(areas)

	reply := bot.Reply{Text: tr.Tf("cmd.area.your_areas", strings.Join(displayNames, ", "))}

	// Generate overview tile if static map is available
	if ctx.StaticMap != nil {
		var fences []*geofence.Fence
		for _, name := range areas {
			if f := ctx.AreaLogic.FindFence(name); f != nil && len(api.FencePaths(f)) > 0 {
				fences = append(fences, f)
			}
		}
		if len(fences) > 0 {
			var autoPolygons [][]staticmap.LatLon
			for _, f := range fences {
				autoPolygons = append(autoPolygons, api.FenceAutopositionPolygons(api.FencePaths(f))...)
			}
			pos := staticmap.Autoposition(staticmap.AutopositionShape{
				Polygons: autoPolygons,
			}, 1024, 768, 1.25, 17.5)
			if pos != nil {
				var tilePolygons []map[string]any
				for i, f := range fences {
					color := api.Rainbow(len(fences), i)
					for _, path := range api.FencePaths(f) {
						tilePolygons = append(tilePolygons, map[string]any{
							"color": color,
							"path":  path,
						})
					}
				}
				data := map[string]any{
					"zoom":      pos.Zoom,
					"latitude":  pos.Latitude,
					"longitude": pos.Longitude,
					"fences":    tilePolygons,
				}
				reply.ImageURL = ctx.StaticMap.GetPregeneratedTileURL("areaoverview", data, "staticMap")
			}
		}
	}

	return []bot.Reply{reply}
}

// getUserCommunities loads community membership from the DB for area security filtering.
func getUserCommunities(ctx *bot.CommandContext) []string {
	if !ctx.Config.Area.Enabled {
		return nil
	}
	var communityJSON *string
	_ = ctx.DB.Get(&communityJSON, "SELECT community_membership FROM humans WHERE id = ? LIMIT 1", ctx.TargetID)
	if communityJSON == nil || *communityJSON == "" {
		return nil
	}
	return bot.ParseCommunityMembership(*communityJSON)
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
	ctx.TriggerReload()
}
