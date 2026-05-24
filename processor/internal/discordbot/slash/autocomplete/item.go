package autocomplete

import (
	"context"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// Item returns autocomplete choices for /quest's item option. The Label is
// the translated item name (e.g. "Golden Razz Berry") in the user's
// language, falling back to English; the Value is the same translated
// name so the underlying QuestCommand.matchItemName resolver — which
// compares against translated names — finds it cleanly.
//
// When focused is empty, returns the first 25 entries sorted by label so
// Discord shows a non-empty starter list on focus.
func Item(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return nil
	}
	focused = strings.ToLower(strings.TrimSpace(focused))

	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)

	type scored struct {
		label string
		value string
		score int
	}

	var results []scored
	for id := range deps.GameData.Items {
		canonicalName := itemNameForLang(enTr, id)
		if canonicalName == "" {
			continue
		}
		label := canonicalName
		if userTr != nil && userTr != enTr {
			if local := itemNameForLang(userTr, id); local != "" {
				label = local
			}
		}
		score := scoreItem(focused, strings.ToLower(canonicalName), strings.ToLower(label))
		if score == 0 {
			continue
		}
		results = append(results, scored{label: label, value: label, score: score})
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
		out[i] = &discordgo.ApplicationCommandOptionChoice{Name: r.label, Value: r.value}
	}
	return out
}

// scoreItem mirrors scorePokemon but without an ID-equals-needle branch
// (item IDs aren't typed by users — only names).
//
//	100 — exact name match (English or local)
//	 50 — prefix match on English
//	 40 — prefix match on local
//	 10 — substring match in either
//	  0 — no match
//
// An empty needle scores everything at 50 via the HasPrefix(any, "") rule,
// which is the same starter-list trick autocomplete.Pokemon uses.
func scoreItem(needle, eng, local string) int {
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
	return 0
}

// itemNameForLang resolves the translated item name, treating the
// raw-key fallback as "no name".
func itemNameForLang(tr interface{ T(string) string }, id int) string {
	if tr == nil {
		return ""
	}
	key := gamedata.ItemTranslationKey(id)
	name := tr.T(key)
	if name == "" || name == key {
		return ""
	}
	return name
}
