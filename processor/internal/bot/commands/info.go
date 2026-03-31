package commands

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
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

	switch sub {
	case "moves":
		return c.listMoves(ctx)
	case "items":
		return c.listItems(ctx)
	case "shiny":
		return c.shinyStats(ctx)
	case "rarity":
		return c.rarityStats(ctx)
	case "weather":
		return c.weatherInfo(ctx, args[1:])
	case "poracle":
		if !ctx.IsAdmin {
			return []bot.Reply{{React: "🙅"}}
		}
		return c.poracleInfo(ctx)
	case "translate":
		if !ctx.IsAdmin {
			return []bot.Reply{{React: "🙅"}}
		}
		return c.translateDebug(ctx, args[1:])
	case "dts":
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
	prefix := commandPrefix(ctx)
	text := tr.Tf("cmd.info.usage", prefix)
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
		if strings.HasPrefix(lower, "form:") {
			formFilter = strings.TrimPrefix(lower, "form:")
		} else {
			nameArgs = append(nameArgs, a)
		}
	}

	name := strings.Join(nameArgs, " ")
	resolved := ctx.Resolver.Resolve(name, ctx.Language)
	if len(resolved) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.Tf("cmd.info.pokemon_not_found", name)}}
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
		return []bot.Reply{{React: "🙅", Text: tr.Tf("cmd.info.pokemon_not_found", name)}}
	}

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")

	// Name
	pokeName := tr.T(gamedata.PokemonTranslationKey(pokemonID))
	enName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**#%d %s**", pokemonID, pokeName))
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

	// Types
	if len(mon.Types) > 0 {
		typeNames := make([]string, 0, len(mon.Types))
		for _, tid := range mon.Types {
			typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
		}
		sb.WriteString(tr.Tf("cmd.info.type", strings.Join(typeNames, " / ")) + "\n")
	}

	// Base stats
	sb.WriteString(tr.Tf("cmd.info.base_stats",
		strconv.Itoa(mon.Attack), strconv.Itoa(mon.Defense), strconv.Itoa(mon.Stamina)) + "\n")

	// Generation
	gen := ctx.GameData.GetGeneration(pokemonID, form)
	if gen > 0 {
		genInfo := ctx.GameData.GetGenerationInfo(gen)
		if genInfo != nil {
			sb.WriteString(tr.Tf("cmd.info.generation", strconv.Itoa(gen), genInfo.Roman) + "\n")
		}
	}

	// Weather boost
	if ctx.GameData != nil && len(mon.Types) > 0 {
		boosting := ctx.GameData.GetBoostingWeathers(mon.Types)
		if len(boosting) > 0 {
			weatherNames := make([]string, 0, len(boosting))
			for _, wid := range boosting {
				weatherNames = append(weatherNames, tr.T(gamedata.WeatherTranslationKey(wid)))
			}
			sb.WriteString(tr.Tf("cmd.info.boosted_by", strings.Join(weatherNames, ", ")) + "\n")
		}
	}

	// Evolutions with candy cost
	if len(mon.Evolutions) > 0 {
		evoStrs := make([]string, 0, len(mon.Evolutions))
		for _, evo := range mon.Evolutions {
			evoName := tr.T(gamedata.PokemonTranslationKey(evo.PokemonID))
			if evo.CandyCost > 0 {
				evoStrs = append(evoStrs, fmt.Sprintf("%s (%d candy)", evoName, evo.CandyCost))
			} else {
				evoStrs = append(evoStrs, evoName)
			}
		}
		sb.WriteString(tr.Tf("cmd.info.evolves_to", strings.Join(evoStrs, ", ")) + "\n")
	}

	// Weakness calculation
	categories := gamedata.CalculateWeaknesses(mon.Types, ctx.GameData.Types)
	if len(categories) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("cmd.info.weakness") + "\n")
		for _, cat := range categories {
			typeNames := make([]string, 0, len(cat.TypeIDs))
			for _, tid := range cat.TypeIDs {
				typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
			}
			sb.WriteString(fmt.Sprintf("  %.2gx: %s\n", cat.Multiplier, strings.Join(typeNames, ", ")))
		}
	}

	return []bot.Reply{{Text: sb.String()}}
}

func (c *InfoCommand) listMoves(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Moves) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.info.no_move_data")}}
	}

	tr := ctx.Tr()
	var sb strings.Builder
	count := 0

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
		if typeName != "" {
			sb.WriteString(fmt.Sprintf("%s (%s)\n", name, typeName))
		} else {
			sb.WriteString(name + "\n")
		}
		count++
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.info.no_move_data")}}
	}

	return []bot.Reply{{
		Text: tr.Tf("cmd.info.moves_header", strconv.Itoa(count)),
		Attachment: &bot.Attachment{
			Filename: "moves.txt",
			Content:  []byte(text),
		},
	}}
}

func (c *InfoCommand) listItems(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Items) == 0 {
		tr := ctx.Tr()
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.info.no_item_data")}}
	}

	tr := ctx.Tr()
	var sb strings.Builder
	count := 0

	for id := 1; id <= 2000; id++ {
		_, ok := ctx.GameData.Items[id]
		if !ok {
			continue
		}
		name := tr.T(gamedata.ItemTranslationKey(id))
		if name == gamedata.ItemTranslationKey(id) {
			continue // no translation
		}
		sb.WriteString(name + "\n")
		count++
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.info.no_item_data")}}
	}

	return []bot.Reply{{
		Text: tr.Tf("cmd.info.items_header", strconv.Itoa(count)),
		Attachment: &bot.Attachment{
			Filename: "items.txt",
			Content:  []byte(text),
		},
	}}
}

func (c *InfoCommand) shinyStats(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Stats == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.info.shiny_unavailable")}}
	}

	tr := ctx.Tr()
	stats := ctx.Stats.ExportShinyStats()
	if len(stats) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.info.shiny_unavailable")}}
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
	sb.WriteString(tr.T("cmd.info.shiny_header") + "\n\n")
	sb.WriteString(fmt.Sprintf("%-25s %6s %6s %8s\n", "Pokemon", tr.T("cmd.info.shiny_seen"), "Shiny", tr.T("cmd.info.shiny_ratio")))
	sb.WriteString(strings.Repeat("-", 50) + "\n")

	for _, e := range entries {
		pokeName := tr.T(gamedata.PokemonTranslationKey(e.id))
		sb.WriteString(fmt.Sprintf("%-25s %6d %6d %8.0f\n", pokeName, e.stat.Total, e.stat.Seen, e.stat.Ratio))
	}

	text := sb.String()

	// If too long for a message, send as file
	if len(text) > 1500 {
		return []bot.Reply{{
			Text: tr.T("cmd.info.shiny_header"),
			Attachment: &bot.Attachment{
				Filename: "shiny_stats.txt",
				Content:  []byte(text),
			},
		}}
	}

	return []bot.Reply{{Text: text}}
}

func (c *InfoCommand) rarityStats(ctx *bot.CommandContext) []bot.Reply {
	if ctx.Stats == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.info.rarity_unavailable")}}
	}

	tr := ctx.Tr()
	groups := ctx.Stats.ExportGroups()
	if len(groups) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.info.rarity_unavailable")}}
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
		return []bot.Reply{{Text: tr.T("cmd.info.rarity_unavailable")}}
	}

	// If too long, send as file
	if len(text) > 1500 {
		return []bot.Reply{{
			Text: tr.T("cmd.info.rarity_header"),
			Attachment: &bot.Attachment{
				Filename: "rarity_stats.txt",
				Content:  []byte(text),
			},
		}}
	}

	return []bot.Reply{{Text: text}}
}

func (c *InfoCommand) weatherInfo(ctx *bot.CommandContext, args []string) []bot.Reply {
	if ctx.Weather == nil {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("cmd.info.weather_unavailable")}}
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
				return []bot.Reply{{Text: tr.T("cmd.info.weather_invalid_coords")}}
			}
		} else {
			return []bot.Reply{{Text: tr.T("cmd.info.weather_invalid_coords")}}
		}
	} else {
		// Look up user's location from DB
		var human struct {
			Latitude  float64 `db:"latitude"`
			Longitude float64 `db:"longitude"`
		}
		err := ctx.DB.Get(&human, "SELECT latitude, longitude FROM humans WHERE id = ? LIMIT 1", ctx.TargetID)
		if err != nil || (human.Latitude == 0 && human.Longitude == 0) {
			return []bot.Reply{{Text: tr.T("cmd.info.weather_no_location")}}
		}
		lat = human.Latitude
		lon = human.Longitude
	}

	cellID := tracker.GetWeatherCellID(lat, lon)
	forecast := ctx.Weather.GetWeatherForecast(cellID)

	var sb strings.Builder
	sb.WriteString(tr.Tf("cmd.info.weather_location", fmt.Sprintf("%.4f", lat), fmt.Sprintf("%.4f", lon)) + "\n")
	sb.WriteString(fmt.Sprintf("S2 Cell: %s\n", cellID))

	if forecast.Current > 0 {
		weatherName := tr.T(gamedata.WeatherTranslationKey(forecast.Current))
		sb.WriteString(tr.Tf("cmd.info.weather_current", weatherName) + "\n")

		// Show boosted types for current weather
		if ctx.GameData != nil {
			boosted := ctx.GameData.GetWeatherBoostTypes(forecast.Current)
			if len(boosted) > 0 {
				typeNames := make([]string, 0, len(boosted))
				for _, tid := range boosted {
					typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
				}
				sb.WriteString(tr.Tf("cmd.info.weather_boosts", strings.Join(typeNames, ", ")) + "\n")
			}
		}
	} else {
		sb.WriteString(tr.T("cmd.info.weather_unknown") + "\n")
	}

	if forecast.Next > 0 {
		nextName := tr.T(gamedata.WeatherTranslationKey(forecast.Next))
		sb.WriteString(tr.Tf("cmd.info.weather_forecast", nextName) + "\n")
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

// dtsInfo lists DTS template configurations (admin only).
func (c *InfoCommand) dtsInfo(ctx *bot.CommandContext) []bot.Reply {
	if ctx.DTS == nil {
		return []bot.Reply{{Text: "DTS templates not loaded"}}
	}

	meta := ctx.DTS.TemplateMetadata(true)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}

	return []bot.Reply{{
		Text: "**DTS Templates:**",
		Attachment: &bot.Attachment{
			Filename: "dts_templates.json",
			Content:  data,
		},
	}}
}
