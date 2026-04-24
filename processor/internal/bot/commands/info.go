package commands

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// InfoCommand implements !info — show pokemon info, type matchups, stats,
// weather, shiny rates, rarity, moves, items, and admin debug tools.
type InfoCommand struct{}

func (c *InfoCommand) Name() string      { return "cmd.info" }
func (c *InfoCommand) Aliases() []string { return nil }

func (c *InfoCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return c.usage(ctx)
	}

	sub := strings.ToLower(args[0])

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("msg.info.sub.moves"):
		return c.listMoves(ctx)
	case matchSub("msg.info.sub.items"):
		return c.listItems(ctx)
	case matchSub("msg.info.sub.shiny"):
		return c.shinyStats(ctx)
	case matchSub("msg.info.sub.rarity"):
		return c.rarityStats(ctx)
	case matchSub("msg.info.sub.weather"):
		return c.weatherInfo(ctx, args[1:])
	case matchSub("msg.info.sub.poracle"):
		if !ctx.IsAdmin {
			return []bot.Reply{{React: "🙅"}}
		}
		return c.poracleInfo(ctx)
	case matchSub("msg.info.sub.translate"):
		if !ctx.IsAdmin {
			return []bot.Reply{{React: "🙅"}}
		}
		return c.translateDebug(ctx, args[1:])
	case matchSub("msg.info.sub.templates"):
		return c.templateList(ctx)
	case matchSub("msg.info.sub.dts"):
		if !ctx.IsAdmin {
			return []bot.Reply{{React: "🙅"}}
		}
		return c.dtsInfo(ctx)
	default:
		// Try as pokemon name
		return c.pokemonInfo(ctx, args)
	}
}

func (c *InfoCommand) usage(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)
	text := tr.Tf("msg.info.usage", prefix)
	return []bot.Reply{{Text: text}}
}

func (c *InfoCommand) pokemonInfo(ctx *bot.CommandContext, args []string) []bot.Reply {
	if ctx.Resolver == nil || ctx.GameData == nil {
		return []bot.Reply{{React: "🙅"}}
	}

	// Parse form: argument out of args
	var formFilter string
	nameArgs := make([]string, 0, len(args))
	for _, a := range args {
		lower := strings.ToLower(a)
		if after, ok := strings.CutPrefix(lower, "form:"); ok {
			formFilter = after
		} else {
			nameArgs = append(nameArgs, a)
		}
	}

	name := strings.Join(nameArgs, " ")
	resolved := ctx.Resolver.Resolve(name, ctx.Language)
	if len(resolved) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.info.pokemon_not_found", name)}}
	}

	pokemonID := resolved[0].PokemonID
	form := resolved[0].Form

	// If a form filter was specified, try to match it
	if formFilter != "" {
		matched := false
		for _, r := range resolved {
			if r.PokemonID != pokemonID {
				continue
			}
			formName := strings.ToLower(ctx.Tr().T(gamedata.FormTranslationKey(r.Form)))
			if formName == formFilter {
				form = r.Form
				matched = true
				break
			}
		}
		if !matched {
			// Also try English form names
			enTr := ctx.Translations.For("en")
			for _, r := range resolved {
				if r.PokemonID != pokemonID {
					continue
				}
				formName := strings.ToLower(enTr.T(gamedata.FormTranslationKey(r.Form)))
				if formName == formFilter {
					form = r.Form
					break
				}
			}
		}
	}

	mon := ctx.GameData.Monsters[gamedata.MonsterKey{ID: pokemonID, Form: form}]
	if mon == nil {
		mon = ctx.GameData.Monsters[gamedata.MonsterKey{ID: pokemonID, Form: 0}]
	}
	if mon == nil {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.info.pokemon_not_found", name)}}
	}

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")

	// Determine platform for emoji resolution
	platform := strings.SplitN(ctx.TargetType, ":", 2)[0]
	if platform == bot.TypeWebhook {
		platform = "discord"
	}
	emoji := ctx.Emoji

	// Name and Pokedex ID
	pokeName := tr.T(gamedata.PokemonTranslationKey(pokemonID))
	enName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s**", pokeName))
	if pokeName != enName {
		sb.WriteString(fmt.Sprintf(" (%s)", enName))
	}

	// Form name
	if form > 0 {
		formName := tr.T(gamedata.FormTranslationKey(form))
		formKey := gamedata.FormTranslationKey(form)
		if formName != formKey {
			sb.WriteString(fmt.Sprintf(" [%s]", formName))
		}
	}
	sb.WriteByte('\n')

	// Pokedex ID
	sb.WriteString(fmt.Sprintf("%s #%d\n", tr.T("msg.info.pokedex_id"), pokemonID))

	// Base stats
	sb.WriteString(tr.Tf("msg.info.base_stats",
		strconv.Itoa(mon.Attack), strconv.Itoa(mon.Defense), strconv.Itoa(mon.Stamina)) + "\n")

	// Generation
	gen := ctx.GameData.GetGeneration(pokemonID, form)
	if gen > 0 {
		genInfo := ctx.GameData.GetGenerationInfo(gen)
		if genInfo != nil {
			sb.WriteString(tr.Tf("msg.info.generation", strconv.Itoa(gen), genInfo.Roman) + "\n")
		}
	}

	// Per-type details: type name, emoji, boosted weather, super effective against
	for i, tid := range mon.Types {
		sb.WriteByte('\n')
		typeLabel := tr.T("msg.info.primary_type")
		if i > 0 {
			typeLabel = tr.T("msg.info.secondary_type")
		}
		sb.WriteString(fmt.Sprintf("**%s**\n", typeLabel))

		typeName := tr.T(gamedata.TypeTranslationKey(tid))
		typeEmoji := ""
		if ti, ok := ctx.GameData.Types[tid]; ok && emoji != nil {
			typeEmoji = emoji.Lookup(ti.Emoji, platform)
		}
		if typeEmoji != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n", typeEmoji, typeName))
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", typeName))
		}

		// Boosted by weather
		singleTypeBoosting := ctx.GameData.GetBoostingWeathers([]int{tid})
		if len(singleTypeBoosting) > 0 {
			var weatherParts []string
			for _, wid := range singleTypeBoosting {
				wName := tr.T(gamedata.WeatherTranslationKey(wid))
				wEmoji := ""
				if wInfo, ok := ctx.GameData.Util.Weather[wid]; ok && emoji != nil {
					wEmoji = emoji.Lookup(wInfo.Emoji, platform)
				}
				if wEmoji != "" {
					weatherParts = append(weatherParts, wEmoji+" "+wName)
				} else {
					weatherParts = append(weatherParts, wName)
				}
			}
			sb.WriteString(fmt.Sprintf("  %s\n", tr.Tf("msg.info.boosted_by", strings.Join(weatherParts, ", "))))
		}

		// Super effective against: find which types have this type in their Weaknesses
		var effectiveAgainst []string
		for otherTID, otherType := range ctx.GameData.Types {
			if slices.Contains(otherType.Weaknesses, tid) {
				otherName := tr.T(gamedata.TypeTranslationKey(otherTID))
				otherEmoji := ""
				if emoji != nil {
					otherEmoji = emoji.Lookup(otherType.Emoji, platform)
				}
				if otherEmoji != "" {
					effectiveAgainst = append(effectiveAgainst, otherEmoji+" "+otherName)
				} else {
					effectiveAgainst = append(effectiveAgainst, otherName)
				}
			}
		}
		if len(effectiveAgainst) > 0 {
			sort.Strings(effectiveAgainst)
			sb.WriteString(fmt.Sprintf("  %s %s\n", tr.T("msg.info.super_effective"), strings.Join(effectiveAgainst, ", ")))
		}
	}

	// Vulnerability section (defensive weakness/resistance)
	categories := gamedata.CalculateWeaknesses(mon.Types, ctx.GameData.Types)
	if len(categories) > 0 {
		sb.WriteByte('\n')

		// Map multiplier to i18n key
		multiplierLabels := map[float64]string{
			4:     "msg.info.very_vulnerable",
			2:     "msg.info.vulnerable",
			0.5:   "msg.info.resistant",
			0.25:  "msg.info.very_resistant",
			0.125: "msg.info.extremely_resistant",
		}

		for _, cat := range categories {
			label, ok := multiplierLabels[cat.Multiplier]
			if !ok {
				continue
			}
			var typeParts []string
			for _, tid := range cat.TypeIDs {
				tName := tr.T(gamedata.TypeTranslationKey(tid))
				tEmoji := ""
				if ti, ok := ctx.GameData.Types[tid]; ok && emoji != nil {
					tEmoji = emoji.Lookup(ti.Emoji, platform)
				}
				if tEmoji != "" {
					typeParts = append(typeParts, tEmoji+" "+tName)
				} else {
					typeParts = append(typeParts, tName)
				}
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", tr.T(label), strings.Join(typeParts, ", ")))
		}
	}

	// Available forms for tracking
	forms := c.availableForms(ctx, pokemonID)
	if len(forms) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("msg.info.available_forms") + "\n")
		for _, f := range forms {
			sb.WriteString("  " + f + "\n")
		}
	}

	// Evolutions with requirement text
	if len(mon.Evolutions) > 0 {
		sb.WriteByte('\n')
		var evoLines []string
		c.collectForwardEvolutions(ctx, mon, tr, &evoLines, 0, 5)
		for _, line := range evoLines {
			sb.WriteString(line + "\n")
		}
	}

	// Previous evolutions (walk backward, max depth 5)
	if ctx.GameData.PrevEvolutions != nil {
		var prevChain []string
		c.collectPrevEvolutions(ctx, pokemonID, &prevChain, 5)
		if len(prevChain) > 0 {
			sb.WriteByte('\n')
			sb.WriteString(tr.Tf("msg.info.evolves_from", strings.Join(prevChain, " <- ")) + "\n")
		}
	}

	// Shiny rate (from stats tracker)
	if ctx.Stats != nil {
		shinyStats := ctx.Stats.ExportShinyStats()
		if s, ok := shinyStats[pokemonID]; ok {
			sb.WriteByte('\n')
			sb.WriteString(fmt.Sprintf("**%s**: %d/%d  (1:%.0f)\n", tr.T("msg.info.shiny_rate"), s.Seen, s.Total, s.Ratio))
		}
	}

	// Hundo CP table
	if mon.Attack > 0 || mon.Defense > 0 || mon.Stamina > 0 {
		sb.WriteByte('\n')
		sb.WriteString("\U0001F4AF " + tr.T("msg.info.hundo_cp") + "\n")
		levels := []int{15, 20, 25, 40, 50, 51}
		levelLabels := []string{"L15", "L20", "L25", "L40", "L50", "L51"}
		for i, level := range levels {
			cp := calculateCP(ctx.GameData, mon.Attack, mon.Defense, mon.Stamina, 15, 15, 15, level)
			sb.WriteString(fmt.Sprintf("  %s: %d\n", levelLabels[i], cp))
		}
	}

	return []bot.Reply{{Text: sb.String()}}
}

// collectForwardEvolutions walks forward through the evolution chain recursively,
// building indented lines like "Evolves to: Clefairy (50 candies)" with sub-evolutions indented.
func (c *InfoCommand) collectForwardEvolutions(ctx *bot.CommandContext, mon *gamedata.Monster, tr *i18n.Translator, lines *[]string, depth, maxDepth int) {
	if depth >= maxDepth || len(mon.Evolutions) == 0 {
		return
	}
	indent := strings.Repeat("  ", depth)
	for _, evo := range mon.Evolutions {
		evoName := tr.T(gamedata.PokemonTranslationKey(evo.PokemonID))
		reqText := gamedata.EvolutionRequirementText(tr, evo)
		if reqText != "" {
			*lines = append(*lines, fmt.Sprintf("%s%s", indent, tr.Tf("msg.info.evolves_to", evoName+" ("+reqText+")")))
		} else {
			*lines = append(*lines, fmt.Sprintf("%s%s", indent, tr.Tf("msg.info.evolves_to", evoName)))
		}
		// Recurse into this evolution's children
		if ctx.GameData != nil {
			evoMon := ctx.GameData.GetMonster(evo.PokemonID, evo.FormID)
			if evoMon != nil {
				c.collectForwardEvolutions(ctx, evoMon, tr, lines, depth+1, maxDepth)
			}
		}
	}
}

// collectPrevEvolutions walks backward through the previous evolution chain,
// collecting display strings like "Eevee (25 candies)". Max depth prevents cycles.
func (c *InfoCommand) collectPrevEvolutions(ctx *bot.CommandContext, pokemonID int, chain *[]string, maxDepth int) {
	if maxDepth <= 0 {
		return
	}
	prevs, ok := ctx.GameData.PrevEvolutions[pokemonID]
	if !ok || len(prevs) == 0 {
		return
	}
	tr := ctx.Tr()
	// Deduplicate by pokemon ID (multiple forms may point to the same prev)
	seen := make(map[int]bool)
	for _, prev := range prevs {
		if seen[prev.PokemonID] {
			continue
		}
		seen[prev.PokemonID] = true
		prevName := tr.T(gamedata.PokemonTranslationKey(prev.PokemonID))
		reqText := gamedata.EvolutionRequirementText(tr, prev.Evolution)
		if reqText != "" {
			*chain = append(*chain, fmt.Sprintf("%s (%s)", prevName, reqText))
		} else {
			*chain = append(*chain, prevName)
		}
		// Walk further back
		c.collectPrevEvolutions(ctx, prev.PokemonID, chain, maxDepth-1)
	}
}

// availableForms returns a list of form display strings for a pokemon,
// formatted as users would type them in tracking commands.
func (c *InfoCommand) availableForms(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.GameData == nil {
		return nil
	}

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	pokeName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))

	type formEntry struct {
		formID   int
		display  string
		sortName string
	}
	var entries []formEntry

	for key, _ := range ctx.GameData.Monsters {
		if key.ID != pokemonID {
			continue
		}
		if key.Form == 0 {
			entries = append(entries, formEntry{
				formID:   0,
				display:  pokeName,
				sortName: "",
			})
			continue
		}
		formName := tr.T(gamedata.FormTranslationKey(key.Form))
		formKey := gamedata.FormTranslationKey(key.Form)
		if formName == formKey {
			// No translation available, use English
			formName = enTr.T(formKey)
			if formName == formKey {
				continue // no translation at all, skip
			}
		}
		// For tracking, users type form names with underscores replacing spaces
		trackingName := strings.ReplaceAll(strings.ToLower(formName), " ", "_")
		entries = append(entries, formEntry{
			formID:   key.Form,
			display:  fmt.Sprintf("%s form:%s", pokeName, trackingName),
			sortName: formName,
		})
	}

	// Only show if there's more than just the base form
	if len(entries) <= 1 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		// Base form first, then alphabetical
		if entries[i].formID == 0 {
			return true
		}
		if entries[j].formID == 0 {
			return true
		}
		return entries[i].sortName < entries[j].sortName
	})

	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = e.display
	}
	return result
}

// calculateCP computes the CP for a pokemon given base stats, IVs, and level.
// CP multipliers are loaded from util.json via GameData.
func calculateCP(gd *gamedata.GameData, baseAtk, baseDef, baseSta, ivAtk, ivDef, ivSta, level int) int {
	if gd == nil || gd.Util == nil || gd.Util.CpMultipliers == nil {
		return 0
	}
	key := strconv.Itoa(level)
	multi, ok := gd.Util.CpMultipliers[key]
	if !ok {
		return 0
	}
	atk := float64(baseAtk + ivAtk)
	def := float64(baseDef + ivDef)
	sta := float64(baseSta + ivSta)
	cp := max(int(math.Floor(atk*math.Sqrt(def)*math.Sqrt(sta)*multi*multi/10)), 10)
	return cp
}

func (c *InfoCommand) listMoves(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Moves) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.info.no_move_data")}}
	}

	tr := ctx.Tr()

	type moveEntry struct {
		name     string
		typeName string
	}

	var entries []moveEntry
	for id := 1; id <= 1000; id++ {
		move, ok := ctx.GameData.Moves[id]
		if !ok {
			continue
		}
		name := tr.T(gamedata.MoveTranslationKey(id))
		if name == gamedata.MoveTranslationKey(id) {
			continue // no translation for this move
		}
		typeName := ""
		if move.TypeID > 0 {
			typeName = tr.T(gamedata.TypeTranslationKey(move.TypeID))
		}
		// For tracking, users type move names with underscores replacing spaces
		trackingName := strings.ReplaceAll(name, " ", "_")
		entries = append(entries, moveEntry{name: trackingName, typeName: typeName})
	}

	// Sort alphabetically
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})

	var sb strings.Builder
	for _, e := range entries {
		if e.typeName != "" {
			sb.WriteString(fmt.Sprintf("%s (%s)\n", e.name, e.typeName))
		} else {
			sb.WriteString(e.name + "\n")
		}
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.info.no_move_data")}}
	}

	return []bot.Reply{{
		Text: tr.Tf("msg.info.moves_header", strconv.Itoa(len(entries))),
		Attachment: &bot.Attachment{
			Filename: "moves.txt",
			Content:  []byte(text),
		},
	}}
}

func (c *InfoCommand) listItems(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Items) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.info.no_item_data")}}
	}

	tr := ctx.Tr()

	// Collect unique item names, deduplicated
	seen := make(map[string]struct{})
	var names []string

	for id := 1; id <= 2000; id++ {
		_, ok := ctx.GameData.Items[id]
		if !ok {
			continue
		}
		name := tr.T(gamedata.ItemTranslationKey(id))
		if name == gamedata.ItemTranslationKey(id) {
			continue // no translation
		}
		// Replace underscores with spaces for readability
		displayName := strings.ReplaceAll(name, "_", " ")
		lower := strings.ToLower(displayName)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		names = append(names, displayName)
	}

	// Sort alphabetically
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	var sb strings.Builder
	for _, n := range names {
		sb.WriteString(n + "\n")
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.info.no_item_data")}}
	}

	return []bot.Reply{{
		Text: tr.Tf("msg.info.items_header", strconv.Itoa(len(names))),
		Attachment: &bot.Attachment{
			Filename: "items.txt",
			Content:  []byte(text),
		},
	}}
}

func (c *InfoCommand) shinyStats(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Stats == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("msg.info.shiny_unavailable")}}
	}

	tr := ctx.Tr()
	stats := ctx.Stats.ExportShinyStats()
	if len(stats) == 0 {
		return []bot.Reply{{Text: tr.T("msg.info.shiny_unavailable")}}
	}

	// Sort by pokemon ID
	type shinyEntry struct {
		id   int
		stat tracker.ShinyStats
	}
	entries := make([]shinyEntry, 0, len(stats))
	for id, s := range stats {
		entries = append(entries, shinyEntry{id, s})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })

	var sb strings.Builder
	sb.WriteString("**" + tr.T("msg.info.shiny_header") + "**\n\n")

	for _, e := range entries {
		pokeName := tr.T(gamedata.PokemonTranslationKey(e.id))
		sb.WriteString(fmt.Sprintf("%s: %s %d - %s 1:%.0f\n", pokeName, tr.T("msg.info.shiny_seen"), e.stat.Total, tr.T("msg.info.shiny_ratio"), e.stat.Ratio))
	}

	return bot.SplitTextReply(sb.String())
}

func (c *InfoCommand) rarityStats(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Stats == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("msg.info.rarity_unavailable")}}
	}

	tr := ctx.Tr()
	groups := ctx.Stats.ExportGroups()
	if len(groups) == 0 {
		return []bot.Reply{{Text: tr.T("msg.info.rarity_unavailable")}}
	}

	var sb strings.Builder

	// Display groups in order: common -> ultra-rare
	groupOrder := []int{
		tracker.RarityCommon,
		tracker.RarityUncommon,
		tracker.RarityRare,
		tracker.RarityVeryRare,
		tracker.RarityUltraRare,
		tracker.RarityNever,
	}

	for _, g := range groupOrder {
		ids, ok := groups[g]
		if !ok || len(ids) == 0 {
			continue
		}

		groupName := tr.T(fmt.Sprintf("rarity_%d", g))
		sb.WriteString(fmt.Sprintf("**%s** (%d):\n", groupName, len(ids)))

		sort.Ints(ids)
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			names = append(names, tr.T(gamedata.PokemonTranslationKey(id)))
		}
		sb.WriteString(strings.Join(names, ", ") + "\n\n")
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{Text: tr.T("msg.info.rarity_unavailable")}}
	}

	return bot.SplitTextReply(text)
}

func (c *InfoCommand) weatherInfo(ctx *bot.CommandContext, args []string) []bot.Reply {
	if ctx.Weather == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("msg.info.weather_unavailable")}}
	}

	tr := ctx.Tr()

	// Parse optional lat,lon from args, otherwise use user's location
	var lat, lon float64
	if len(args) > 0 {
		coords := strings.Join(args, "")
		parts := strings.Split(coords, ",")
		if len(parts) == 2 {
			var err1, err2 error
			lat, err1 = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lon, err2 = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 != nil || err2 != nil {
				return []bot.Reply{{Text: tr.T("msg.info.weather_invalid_coords")}}
			}
		} else {
			return []bot.Reply{{Text: tr.T("msg.info.weather_invalid_coords")}}
		}
	} else {
		// Look up user's location from DB
		human, err := ctx.Humans.Get(ctx.TargetID)
		if err != nil || human == nil || (human.Latitude == 0 && human.Longitude == 0) {
			return []bot.Reply{{Text: tr.T("msg.info.weather_no_location")}}
		}
		lat = human.Latitude
		lon = human.Longitude
	}

	cellID := tracker.GetWeatherCellID(lat, lon)
	forecast := ctx.Weather.GetWeatherForecast(cellID)

	var sb strings.Builder
	sb.WriteString(tr.Tf("msg.info.weather_location", fmt.Sprintf("%.4f", lat), fmt.Sprintf("%.4f", lon)) + "\n")
	sb.WriteString(fmt.Sprintf("S2 Cell: %s\n", cellID))

	if forecast.Current > 0 {
		weatherName := tr.T(gamedata.WeatherTranslationKey(forecast.Current))
		sb.WriteString(tr.Tf("msg.info.weather_current", weatherName) + "\n")

		// Show boosted types for current weather
		if ctx.GameData != nil {
			boosted := ctx.GameData.GetWeatherBoostTypes(forecast.Current)
			if len(boosted) > 0 {
				typeNames := make([]string, 0, len(boosted))
				for _, tid := range boosted {
					typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
				}
				sb.WriteString(tr.Tf("msg.info.weather_boosts", strings.Join(typeNames, ", ")) + "\n")
			}
		}
	} else {
		sb.WriteString(tr.T("msg.info.weather_unknown") + "\n")
	}

	if forecast.Next > 0 {
		nextName := tr.T(gamedata.WeatherTranslationKey(forecast.Next))
		sb.WriteString(tr.Tf("msg.info.weather_forecast", nextName) + "\n")
	}

	return []bot.Reply{{Text: sb.String()}}
}

// poracleInfo shows admin-only system status information.
func (c *InfoCommand) poracleInfo(ctx *bot.CommandContext) []bot.Reply {
	var sb strings.Builder
	sb.WriteString("**Poracle Info**\n")

	// Dispatcher queue depths
	if ctx.Dispatcher != nil {
		sb.WriteString("\n**Delivery Queues:**\n")
		sb.WriteString(fmt.Sprintf("  Queue depth: %d\n", ctx.Dispatcher.QueueDepth()))
		sb.WriteString(fmt.Sprintf("  Discord depth: %d\n", ctx.Dispatcher.DiscordDepth()))
		sb.WriteString(fmt.Sprintf("  Webhook depth: %d\n", ctx.Dispatcher.WebhookDepth()))
		sb.WriteString(fmt.Sprintf("  Telegram depth: %d\n", ctx.Dispatcher.TelegramDepth()))
		sb.WriteString(fmt.Sprintf("  Tracker size: %d\n", ctx.Dispatcher.TrackerSize()))
	}

	// State info
	if ctx.StateMgr != nil {
		s := ctx.StateMgr.Get()
		if s != nil {
			monsterCount := 0
			if s.Monsters != nil {
				monsterCount = s.Monsters.Total
			}
			sb.WriteString(fmt.Sprintf("\n**State:** %d humans, %d monster rules, %d raid rules, %d egg rules\n",
				len(s.Humans), monsterCount, len(s.Raids), len(s.Eggs)))
			sb.WriteString(fmt.Sprintf("  %d invasion rules, %d quest rules, %d lure rules\n",
				len(s.Invasions), len(s.Quests), len(s.Lures)))
			sb.WriteString(fmt.Sprintf("  %d gym rules, %d nest rules, %d fort rules, %d maxbattle rules\n",
				len(s.Gyms), len(s.Nests), len(s.Forts), len(s.Maxbattles)))
		}
	}

	return []bot.Reply{{Text: sb.String()}}
}

// translateDebug shows forward and reverse translation debug info.
func (c *InfoCommand) translateDebug(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return []bot.Reply{{Text: "Usage: info translate <word>"}}
	}

	word := strings.ToLower(strings.Join(args, " "))
	tr := ctx.Tr()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Translate debug for: %s** (language: %s)\n\n", word, ctx.Language))

	// Forward lookup: try the word as a key
	result := tr.T(word)
	if result != word {
		sb.WriteString(fmt.Sprintf("Key `%s` -> `%s`\n", word, result))
	}

	// Reverse lookup: find keys whose value matches the word
	messages := tr.Messages()
	var reverseMatches []string
	for key, val := range messages {
		if strings.EqualFold(val, word) {
			reverseMatches = append(reverseMatches, key)
		}
	}
	if len(reverseMatches) > 0 {
		sort.Strings(reverseMatches)
		sb.WriteString(fmt.Sprintf("\nValue `%s` found in keys:\n", word))
		for _, key := range reverseMatches {
			sb.WriteString(fmt.Sprintf("  `%s`\n", key))
		}
	}

	// Partial match on values
	var partialMatches []string
	for key, val := range messages {
		if strings.Contains(strings.ToLower(val), word) && !strings.EqualFold(val, word) {
			partialMatches = append(partialMatches, fmt.Sprintf("  `%s` -> `%s`", key, val))
		}
	}
	if len(partialMatches) > 0 {
		sort.Strings(partialMatches)
		if len(partialMatches) > 20 {
			partialMatches = partialMatches[:20]
		}
		sb.WriteString(fmt.Sprintf("\nPartial matches (%d, showing max 20):\n", len(partialMatches)))
		for _, m := range partialMatches {
			sb.WriteString(m + "\n")
		}
	}

	if result == word && len(reverseMatches) == 0 && len(partialMatches) == 0 {
		sb.WriteString("No translations found.\n")
	}

	text := sb.String()
	if len(text) > 1800 {
		return []bot.Reply{{
			Text: fmt.Sprintf("**Translate debug for: %s**", word),
			Attachment: &bot.Attachment{
				Filename: "translate_debug.txt",
				Content:  []byte(text),
			},
		}}
	}
	return []bot.Reply{{Text: text}}
}

// dtsInfo shows a summary of loaded DTS templates (admin only).
func (c *InfoCommand) dtsInfo(ctx *bot.CommandContext) []bot.Reply {
	if ctx.DTS == nil {
		return []bot.Reply{{Text: "DTS templates not loaded"}}
	}

	tr := ctx.Tr()
	summary := ctx.DTS.TemplateSummaryDetailed()

	var sb strings.Builder
	sb.WriteString(tr.T("msg.info.dts_summary") + "\n\n")

	// Sort types for consistent output
	types := make([]string, 0, len(summary))
	for t := range summary {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, t := range types {
		byPlatform := summary[t]
		platforms := make([]string, 0, len(byPlatform))
		for p := range byPlatform {
			platforms = append(platforms, p)
		}
		sort.Strings(platforms)

		var parts []string
		for _, p := range platforms {
			ids := byPlatform[p]
			sort.Strings(ids)
			parts = append(parts, fmt.Sprintf("%s(%d): %s", p, len(ids), strings.Join(ids, ", ")))
		}
		sb.WriteString(fmt.Sprintf("**%s**\n  %s\n", t, strings.Join(parts, "\n  ")))
	}

	return bot.SplitTextReply(sb.String())
}

// dtsTypeDisplayName maps DTS type strings to user-friendly display names.
var dtsTypeDisplayName = map[string]string{
	"monster":       "Pokemon",
	"monsterNoIv":   "Pokemon (no IV)",
	"raid":          "Raids",
	"egg":           "Eggs",
	"quest":         "Quests",
	"invasion":      "Invasions",
	"lure":          "Lures",
	"nest":          "Nests",
	"gym":           "Gyms",
	"fort-update":   "Fort Updates",
	"maxbattle":     "Max Battles",
	"weatherchange": "Weather",
	"greeting":      "Greetings",
}

func (c *InfoCommand) templateList(ctx *bot.CommandContext) []bot.Reply {
	if ctx.DTS == nil {
		return []bot.Reply{{Text: "Templates not loaded"}}
	}

	platform := targetDTSPlatform(ctx)
	byType := ctx.DTS.ListForPlatform(platform)

	// Hide types that users can't select templates for
	delete(byType, "weatherchange")
	delete(byType, "monsterNoIv")
	delete(byType, "greeting")

	if len(byType) == 0 {
		return []bot.Reply{{Text: "No templates available"}}
	}

	// Sort types for consistent output
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Available templates (%s):**\n\n", platform))

	for _, t := range types {
		templates := byType[t]
		displayName := dtsTypeDisplayName[t]
		if displayName == "" {
			displayName = t
		}
		sb.WriteString(fmt.Sprintf("**%s**\n", displayName))

		for _, tmpl := range templates {
			sb.WriteString(fmt.Sprintf("  `%s`", tmpl.ID))
			if tmpl.Name != "" {
				sb.WriteString(fmt.Sprintf(" — %s", tmpl.Name))
			}
			sb.WriteByte('\n')
			if tmpl.Description != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", tmpl.Description))
			}
		}
		sb.WriteByte('\n')
	}

	return bot.SplitTextReply(strings.TrimSpace(sb.String()))
}
