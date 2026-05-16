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

// Form returns autocomplete choices for /track's form option, cascading
// from the user's currently-selected pokemon. pokemonName is the canonical
// (or localised) name the user already picked for the pokemon option; we
// resolve it to an ID, enumerate (ID, *) entries in GameData.Monsters,
// and translate each FormID into a user-facing label.
//
// Returns nil when no pokemon has been selected yet — Discord then shows
// nothing, which is the right cue for the user to fill in pokemon first.
func Form(ctx context.Context, deps *bot.BotDeps, pokemonName, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.GameData == nil || deps.Translations == nil {
		return nil
	}
	pokemonID := resolvePokemonID(deps, pokemonName)
	if pokemonID <= 0 {
		return nil
	}
	focused = strings.ToLower(strings.TrimSpace(focused))

	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)

	type formChoice struct {
		label   string
		value   string
		formID  int // 0 means "default/any"; named forms have non-zero IDs
	}
	seenForm := make(map[int]bool)
	var out []formChoice
	for key := range deps.GameData.Monsters {
		if key.ID != pokemonID {
			continue
		}
		if seenForm[key.Form] {
			continue
		}
		seenForm[key.Form] = true

		label, value := formLabel(enTr, userTr, key.Form)
		if value == "" {
			continue
		}
		if focused != "" && !strings.Contains(strings.ToLower(label), focused) {
			continue
		}
		out = append(out, formChoice{label: label, value: value, formID: key.Form})
	}

	// Dedupe by lowercase label: a species can carry both form 0 (the
	// generic "any form" placeholder we render as "Normal") AND a named
	// "Normal" form (e.g. Exeggutor has both — the named one means
	// specifically the Kanto variant, distinct from Alolan). Prefer the
	// named form so picking "Normal" emits a specific value the text
	// bot resolves to the right form ID. When two named forms collide on
	// label (rare), the lower form ID wins for stability.
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].label), strings.ToLower(out[j].label)
		if li != lj {
			return li < lj
		}
		// Same label: prefer named (non-zero) form; among named forms
		// prefer lower formID for determinism.
		if (out[i].formID == 0) != (out[j].formID == 0) {
			return out[j].formID == 0
		}
		return out[i].formID < out[j].formID
	})
	deduped := out[:0]
	var lastLabel string
	for i, c := range out {
		lower := strings.ToLower(c.label)
		if i > 0 && lower == lastLabel {
			continue
		}
		lastLabel = lower
		deduped = append(deduped, c)
	}
	out = deduped

	if len(out) > 25 {
		out = out[:25]
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, len(out))
	for i, c := range out {
		choices[i] = &discordgo.ApplicationCommandOptionChoice{Name: c.label, Value: c.value}
	}
	return choices
}

// resolvePokemonID accepts either the canonical lowercase English name
// (what autocomplete.Pokemon emits as the Value) or a numeric ID string.
// Returns 0 when no match.
func resolvePokemonID(deps *bot.BotDeps, name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return 0
	}
	if n, err := strconv.Atoi(name); err == nil && n > 0 {
		return n
	}
	enTr := deps.Translations.For("en")
	if enTr == nil {
		return 0
	}
	// Walk distinct species and compare against the English name —
	// autocomplete.Pokemon emits canonical English lowercase as the Value
	// so the typical cascading flow takes this path.
	visited := map[int]bool{}
	for key := range deps.GameData.Monsters {
		if visited[key.ID] {
			continue
		}
		visited[key.ID] = true
		if got := strings.ToLower(pokemonNameForLang(enTr, key.ID)); got != "" && got == name {
			return key.ID
		}
	}
	return 0
}

// formLabel produces the user-facing label and value for a form option.
// Form 0 is the species' default — emitted as "Normal" so the user can
// pick the no-form variant explicitly. Non-default forms use the
// translated form_<id> string.
func formLabel(enTr, userTr interface{ T(string) string }, formID int) (label, value string) {
	if formID == 0 {
		return "Normal", "normal"
	}
	key := gamedata.FormTranslationKey(formID)
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
	if name == "" {
		return "", ""
	}
	return name, strings.ToLower(name)
}

