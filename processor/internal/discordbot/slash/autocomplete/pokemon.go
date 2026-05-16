package autocomplete

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// Pokemon returns autocomplete choices for pokemon names matching focused.
// The Label is the user's localized name when a translation exists,
// otherwise the canonical English name. The Value is always the canonical
// English lowercase name so downstream parsers see a stable input
// regardless of userLang.
//
// Returns nil for empty focused — the caller (dispatcher) is responsible
// for boosting RecentActivity entries when nothing has been typed yet.
func Pokemon(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	if focused == "" {
		return nil
	}
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return nil
	}

	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)

	type scored struct {
		canonical string
		label     string
		score     int
	}

	// Deduplicate by pokemon ID — Monsters is keyed by (ID, Form) so a
	// single species can appear multiple times.
	seen := make(map[int]bool, len(deps.GameData.Monsters))
	var results []scored

	for key := range deps.GameData.Monsters {
		id := key.ID
		if seen[id] {
			continue
		}
		seen[id] = true

		canonicalName := pokemonNameForLang(enTr, id)
		if canonicalName == "" {
			continue
		}
		canonical := strings.ToLower(canonicalName)
		label := canonicalName
		if userTr != nil && userTr != enTr {
			if local := userTr.T(gamedata.PokemonTranslationKey(id)); local != "" && local != gamedata.PokemonTranslationKey(id) {
				label = local
			}
		}

		score := scorePokemon(focused, canonical, strings.ToLower(label), id)
		if score == 0 {
			continue
		}
		results = append(results, scored{canonical: canonical, label: label, score: score})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].label < results[j].label
	})
	if len(results) > 25 {
		results = results[:25]
	}

	out := make([]*discordgo.ApplicationCommandOptionChoice, len(results))
	for i, r := range results {
		out[i] = &discordgo.ApplicationCommandOptionChoice{Name: r.label, Value: r.canonical}
	}
	return out
}

// scorePokemon ranks a pokemon entry against the user's input.
//
//	100 — exact match (English or local name)
//	 50 — prefix match on English
//	 40 — prefix match on local
//	 10 — substring match in either
//	  5 — numeric needle equals the pokemon ID
//	  0 — no match
func scorePokemon(needle, eng, local string, id int) int {
	if needle == eng || needle == local {
		return 100
	}
	if strings.HasPrefix(eng, needle) {
		return 50
	}
	if strings.HasPrefix(local, needle) {
		return 40
	}
	if strings.Contains(eng, needle) || strings.Contains(local, needle) {
		return 10
	}
	if n, err := strconv.Atoi(needle); err == nil && n == id {
		return 5
	}
	return 0
}

// pokemonNameForLang returns the translated pokemon name for the given
// translator, or "" if no translation is registered. The translator
// returns the raw key (e.g. "poke_25") when no message exists; we treat
// that as "no name" rather than emitting "poke_25" as the user-facing
// label.
func pokemonNameForLang(tr interface{ T(string) string }, id int) string {
	if tr == nil {
		return ""
	}
	key := gamedata.PokemonTranslationKey(id)
	name := tr.T(key)
	if name == "" || name == key {
		return ""
	}
	return name
}
