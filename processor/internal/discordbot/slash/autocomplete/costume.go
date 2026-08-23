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

// Costume returns autocomplete choices for /track's costume option.
//
// Unlike forms, costumes are NOT species-scoped — GameData.Costumes is a
// flat, global list (e.g. "Holiday 2016", "Flying") that applies across
// many pokemon, so there is no cascading from a sibling pokemon option.
// Every entry is a candidate on every keystroke.
//
// Costume ID 0 ("Unset") is a real, selectable filter value — it means
// "no costume applied" — distinct from the matcher's internal "any
// costume" sentinel (9000), which is never surfaced as a choice. This is
// the opposite of Form's ID-0 handling, where 0 is a placeholder that the
// picker omits.
//
// Label is the translated costume name (user's language, falling back to
// English); Value is the costume ID as a string, matching
// ArgMatcher.ResolveCostume's numeric fast-path so no name-translation
// round-trip is needed when the token flows back through the text parser.
func Costume(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return nil
	}
	focused = strings.ToLower(strings.TrimSpace(focused))

	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)

	type costumeChoice struct {
		label string
		value string
		id    int
	}
	var out []costumeChoice
	for id := range deps.GameData.Costumes {
		label := costumeLabel(enTr, userTr, id)
		if label == "" {
			continue
		}
		if focused != "" && !strings.Contains(strings.ToLower(label), focused) {
			continue
		}
		out = append(out, costumeChoice{label: label, value: strconv.Itoa(id), id: id})
	}

	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].label), strings.ToLower(out[j].label)
		if li != lj {
			return li < lj
		}
		return out[i].id < out[j].id
	})

	if len(out) > 25 {
		out = out[:25]
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, len(out))
	for i, c := range out {
		choices[i] = &discordgo.ApplicationCommandOptionChoice{Name: c.label, Value: c.value}
	}
	return choices
}

// costumeLabel produces the user-facing label for a costume ID, preferring
// the user's language and falling back to English. Mirrors formLabel's
// fallback logic in form.go.
func costumeLabel(enTr, userTr interface{ T(string) string }, id int) string {
	key := gamedata.CostumeTranslationKey(id)
	name := ""
	if userTr != nil {
		if v := userTr.T(key); v != "" && v != key {
			name = v
		}
	}
	if name == "" && enTr != nil {
		if v := enTr.T(key); v != "" && v != key {
			name = v
		}
	}
	return name
}
