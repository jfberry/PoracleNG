package autocomplete

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// RaidBoss returns pokemon name choices for /raid boss. Tier keywords
// ("5", "mega", "legendary", ...) belong to /raid level, which has its
// own static Choices list — mixing them into boss would be redundant
// and visually confuses the user. RecentActivity-active raid bosses
// surface first on empty focused so currently-spawning bosses are one
// click away.
func RaidBoss(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	seen := map[string]bool{}

	add := func(name, value string) bool {
		if seen[value] {
			return false
		}
		seen[value] = true
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: value})
		return len(out) >= 25
	}

	// On empty focused, prepend recently-active raid bosses so live
	// ones are one click away. Capped at 10 to leave room for the
	// general alphabetical pokemon list below.
	if focused == "" && deps != nil && deps.RecentActivity != nil {
		for i, id := range deps.RecentActivity.ActiveRaidBosses() {
			if i >= 10 {
				break
			}
			name := pokemonNameFor(deps, id, userLang)
			if name == "" {
				continue
			}
			if add(name, strings.ToLower(name)) {
				return out
			}
		}
	}

	// Fill the rest from the general pokemon autocomplete (alphabetical
	// starter on empty focused, scored search on typed).
	for _, p := range Pokemon(ctx, deps, focused, userLang) {
		if add(p.Name, p.Value.(string)) {
			return out
		}
	}
	return out
}

// pokemonNameFor returns the user-locale name for a pokemon, falling back
// to the canonical English name. Returns "" if the species isn't loaded
// or no name is registered in either locale.
func pokemonNameFor(deps *bot.BotDeps, id int, userLang string) string {
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return ""
	}
	if _, ok := deps.GameData.Monsters[gamedata.MonsterKey{ID: id, Form: 0}]; !ok {
		// Try any form for this species — some pokemon have no form-0 entry.
		found := false
		for k := range deps.GameData.Monsters {
			if k.ID == id {
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	key := gamedata.PokemonTranslationKey(id)
	if tr := deps.Translations.For(userLang); tr != nil {
		if name := tr.T(key); name != "" && name != key {
			return name
		}
	}
	if enTr := deps.Translations.For("en"); enTr != nil {
		if name := enTr.T(key); name != "" && name != key {
			return name
		}
	}
	return ""
}
