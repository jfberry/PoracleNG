package autocomplete

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// raidLevelKeywords are the non-pokemon raid filter tokens (tier numbers
// and special boss categories) offered to the user alongside concrete
// pokemon names.
var raidLevelKeywords = []string{"1", "3", "5", "6", "mega", "legendary", "shadow", "ultra beast"}

// RaidBoss combines three signal sources for the user's raid-boss input:
//
//  1. Currently-active bosses from RecentActivity (when nothing typed).
//  2. The fixed list of tier / category keywords.
//  3. A fall-through to the general Pokemon name autocomplete.
//
// We cap entry sources independently so a quiet RecentActivity doesn't
// starve the keyword/pokemon suggestions and a noisy one doesn't drown
// the keywords.
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

	// Source 1: recently-active raid bosses (only when focused is empty,
	// so a typed query goes straight to keyword/general lookup).
	if focused == "" && deps != nil && deps.RecentActivity != nil {
		for i, id := range deps.RecentActivity.ActiveRaidBosses() {
			if i >= 10 { // leave room for keywords + pokemon
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

	// Source 2: tier / category keywords. Always offered; substring-filter
	// when the user has typed something.
	for _, kw := range raidLevelKeywords {
		if focused != "" && !strings.Contains(kw, focused) {
			continue
		}
		if add(kw, kw) {
			return out
		}
	}

	// Source 3: fall through to the general pokemon autocomplete for
	// anything the user has typed. Pokemon() already caps at 25 entries
	// and skips early on empty focused, so this is a cheap no-op when
	// nothing's been typed.
	if focused != "" {
		for _, p := range Pokemon(ctx, deps, focused, userLang) {
			if add(p.Name, p.Value.(string)) {
				return out
			}
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
