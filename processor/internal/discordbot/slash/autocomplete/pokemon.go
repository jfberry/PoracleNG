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
// When focused is empty (the user clicked the field but hasn't typed yet),
// returns the first 25 entries sorted alphabetically by label so Discord
// has something to show — otherwise the dropdown would be empty and the
// command feels broken. The synthetic "Everything" entry is prepended
// (case-insensitive prefix-match against focused) when
// cfg.Tracking.EverythingFlagPermissions is not "deny" — surfacing the
// keyword the text bot recognises for "track all pokemon".
func Pokemon(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return nil
	}

	// Synthetic "everything" entry — keep it first so users see it without
	// scrolling. Cfg may be nil in tests; treat missing config as "allow"
	// because the gate is operator-applied, not user-applied.
	var head []*discordgo.ApplicationCommandOptionChoice
	if everythingAllowed(deps) {
		if focused == "" || strings.HasPrefix("everything", focused) {
			head = append(head, &discordgo.ApplicationCommandOptionChoice{
				Name:  "Everything",
				Value: "everything",
			})
		}
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
			key := gamedata.PokemonTranslationKey(id)
			if local := userTr.T(key); local != "" && local != key {
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

	// Trim so head + results fits under Discord's 25-choice cap.
	cap := 25 - len(head)
	if len(results) > cap {
		results = results[:cap]
	}

	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(head)+len(results))
	out = append(out, head...)
	for _, r := range results {
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: r.label, Value: r.canonical})
	}
	return out
}

// everythingAllowed reports whether the "Everything" entry should surface
// for the invoking user. Mirrors the text bot's gate at
// commands/track.go:272 — operator config opts in/out. Treats a nil cfg
// as "allow" so tests with bare deps don't have to wire the full config.
func everythingAllowed(deps *bot.BotDeps) bool {
	if deps == nil || deps.Cfg == nil {
		return true
	}
	mode := strings.ToLower(deps.Cfg.Tracking.EverythingFlagPermissions)
	return mode != "deny"
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
