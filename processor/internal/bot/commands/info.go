package commands

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// InfoCommand implements !info — show pokemon info, type matchups, stats.
type InfoCommand struct{}

func (c *InfoCommand) Name() string      { return "cmd.info" }
func (c *InfoCommand) Aliases() []string { return nil }

func (c *InfoCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		prefix := commandPrefix(ctx)
		text := "**Info subcommands:**\n"
		text += prefix + "info <pokemon> — Pokemon info\n"
		text += prefix + "info moves — List all moves\n"
		text += prefix + "info items — List all items\n"
		return []bot.Reply{{Text: text}}
	}

	sub := args[0]

	switch sub {
	case "moves":
		return c.listMoves(ctx)
	case "items":
		return c.listItems(ctx)
	default:
		// Try as pokemon name
		return c.pokemonInfo(ctx, args)
	}
}

func (c *InfoCommand) pokemonInfo(ctx *bot.CommandContext, args []string) []bot.Reply {
	if ctx.Resolver == nil || ctx.GameData == nil {
		return []bot.Reply{{React: "🙅"}}
	}

	name := strings.Join(args, " ")
	resolved := ctx.Resolver.Resolve(name, ctx.Language)
	if len(resolved) == 0 {
		return []bot.Reply{{React: "🙅", Text: fmt.Sprintf("Pokemon not found: %s", name)}}
	}

	pokemonID := resolved[0].PokemonID
	form := resolved[0].Form

	mon := ctx.GameData.Monsters[gamedata.MonsterKey{ID: pokemonID, Form: form}]
	if mon == nil {
		mon = ctx.GameData.Monsters[gamedata.MonsterKey{ID: pokemonID, Form: 0}]
	}
	if mon == nil {
		return []bot.Reply{{React: "🙅", Text: fmt.Sprintf("Pokemon data not found for ID %d", pokemonID)}}
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
	sb.WriteByte('\n')

	// Types
	if len(mon.Types) > 0 {
		typeNames := make([]string, 0, len(mon.Types))
		for _, tid := range mon.Types {
			typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
		}
		sb.WriteString(fmt.Sprintf("Type: %s\n", strings.Join(typeNames, " / ")))
	}

	// Base stats
	sb.WriteString(fmt.Sprintf("Base Stats: ATK %d / DEF %d / STA %d\n", mon.Attack, mon.Defense, mon.Stamina))

	// Generation
	gen := ctx.GameData.GetGeneration(pokemonID, form)
	if gen > 0 {
		genInfo := ctx.GameData.GetGenerationInfo(gen)
		if genInfo != nil {
			sb.WriteString(fmt.Sprintf("Generation: %d (%s)\n", gen, genInfo.Roman))
		}
	}

	// Evolutions
	if len(mon.Evolutions) > 0 {
		evoNames := make([]string, 0, len(mon.Evolutions))
		for _, evo := range mon.Evolutions {
			evoName := tr.T(gamedata.PokemonTranslationKey(evo.PokemonID))
			evoNames = append(evoNames, evoName)
		}
		sb.WriteString(fmt.Sprintf("Evolves to: %s\n", strings.Join(evoNames, ", ")))
	}

	// Weakness calculation
	categories := gamedata.CalculateWeaknesses(mon.Types, ctx.GameData.Types)
	if len(categories) > 0 {
		sb.WriteByte('\n')
		for _, cat := range categories {
			typeNames := make([]string, 0, len(cat.TypeIDs))
			for _, tid := range cat.TypeIDs {
				typeNames = append(typeNames, tr.T(gamedata.TypeTranslationKey(tid)))
			}
			sb.WriteString(fmt.Sprintf("%.2gx: %s\n", cat.Multiplier, strings.Join(typeNames, ", ")))
		}
	}

	return []bot.Reply{{Text: sb.String()}}
}

func (c *InfoCommand) listMoves(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Moves) == 0 {
		return []bot.Reply{{React: "🙅", Text: "No move data loaded"}}
	}

	tr := ctx.Tr()
	var sb strings.Builder

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
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: "No move translations found"}}
	}

	return []bot.Reply{{
		Text: fmt.Sprintf("**Moves** (%d):", len(ctx.GameData.Moves)),
		Attachment: &bot.Attachment{
			Filename: "moves.txt",
			Content:  []byte(text),
		},
	}}
}

func (c *InfoCommand) listItems(ctx *bot.CommandContext) []bot.Reply {
	if ctx.GameData == nil || len(ctx.GameData.Items) == 0 {
		return []bot.Reply{{React: "🙅", Text: "No item data loaded"}}
	}

	tr := ctx.Tr()
	var sb strings.Builder

	for id := 1; id <= 2000; id++ {
		_, ok := ctx.GameData.Items[id]
		if !ok {
			continue
		}
		name := tr.T(gamedata.ItemTranslationKey(id))
		if name == gamedata.ItemTranslationKey(id) {
			continue // no translation
		}
		sb.WriteString(fmt.Sprintf("%s\n", name))
	}

	text := sb.String()
	if text == "" {
		return []bot.Reply{{React: "🙅", Text: "No item translations found"}}
	}

	return []bot.Reply{{
		Text: fmt.Sprintf("**Items** (%d):", len(ctx.GameData.Items)),
		Attachment: &bot.Attachment{
			Filename: "items.txt",
			Content:  []byte(text),
		},
	}}
}
