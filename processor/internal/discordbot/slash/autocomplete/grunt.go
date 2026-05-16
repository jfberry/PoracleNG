package autocomplete

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// Grunt returns autocomplete choices for /invasion's grunt_type option.
// Covers two categories — Team Rocket grunts that have a gender variant:
//
//   - Bosses: Giovanni, Arlo, Cliff, Sierra (canonical leader names)
//   - Typed grunts: Fire, Water, Grass, ... using poke_type_N translations
//
// Plus the "Everything" keyword. Pokestop incidents (Kecleon, Showcase,
// Gold Pokestop) live on /incident instead — those events have no gender
// variants and don't share UX with grunt encounters.
//
// Value is always the lowercased English canonical name the text bot's
// matchInvasionType resolver compares against — see
// internal/bot/commands/invasion.go::validTypes.
func Grunt(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.GameData == nil {
		return nil
	}
	focused = strings.ToLower(strings.TrimSpace(focused))

	type entry struct {
		label string // user-facing (translated when applicable)
		value string // canonical English DB value the text bot accepts
		group int    // 0=keywords, 1=bosses, 2=types
	}

	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)
	if userTr == nil {
		userTr = enTr
	}

	seen := make(map[string]bool)
	var entries []entry
	add := func(label, value string, group int) {
		if value == "" || label == "" || seen[value] {
			return
		}
		seen[value] = true
		entries = append(entries, entry{label: label, value: value, group: group})
	}

	add("Everything", "everything", 0)

	bossSeen := map[string]bool{}
	for _, g := range deps.GameData.Grunts {
		canonical := strings.ToLower(gamedata.TypeNameFromTemplate(g.Template))
		if canonical == "" {
			continue
		}
		if g.Boss {
			if !bossSeen[canonical] {
				bossSeen[canonical] = true
				add(titleCase(canonical), canonical, 1)
			}
			continue
		}
		if g.TypeID > 0 {
			typeKey := gamedata.TypeTranslationKey(g.TypeID)
			typeName := ""
			if userTr != nil {
				if v := userTr.T(typeKey); v != "" && v != typeKey {
					typeName = v
				}
			}
			if typeName == "" && enTr != nil {
				if v := enTr.T(typeKey); v != "" && v != typeKey {
					typeName = v
				}
			}
			if typeName != "" {
				add(typeName+" Grunt", canonical, 2)
			}
		} else {
			add(titleCase(canonical), canonical, 2)
		}
	}

	if focused != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.label), focused) ||
				strings.Contains(e.value, focused) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].group != entries[j].group {
			return entries[i].group < entries[j].group
		}
		return entries[i].label < entries[j].label
	})

	if len(entries) > 25 {
		entries = entries[:25]
	}
	out := make([]*discordgo.ApplicationCommandOptionChoice, len(entries))
	for i, e := range entries {
		out[i] = &discordgo.ApplicationCommandOptionChoice{Name: e.label, Value: e.value}
	}
	return out
}

// IncidentType returns autocomplete choices for /incident's type option —
// pokestop events from util.PokestopEvent (Kecleon, Gold Pokestop,
// Showcase, Pokestop Spawn, …). Labels use display_type_N translations
// when present; values are the canonical English name the text bot's
// matchInvasionType resolves against.
func IncidentType(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.GameData == nil || deps.GameData.Util == nil {
		return nil
	}
	focused = strings.ToLower(strings.TrimSpace(focused))

	userTr := deps.Translations.For(userLang)
	if userTr == nil {
		userTr = deps.Translations.For("en")
	}

	type entry struct {
		label string
		value string
	}
	var entries []entry
	for id, ev := range deps.GameData.Util.PokestopEvent {
		canonical := strings.ToLower(ev.Name)
		if canonical == "" {
			continue
		}
		label := ev.Name
		if userTr != nil {
			key := fmt.Sprintf("display_type_%d", id)
			if v := userTr.T(key); v != "" && v != key {
				label = v
			}
		}
		if focused != "" &&
			!strings.Contains(strings.ToLower(label), focused) &&
			!strings.Contains(canonical, focused) {
			continue
		}
		entries = append(entries, entry{label: label, value: canonical})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].label < entries[j].label })

	if len(entries) > 25 {
		entries = entries[:25]
	}
	out := make([]*discordgo.ApplicationCommandOptionChoice, len(entries))
	for i, e := range entries {
		out[i] = &discordgo.ApplicationCommandOptionChoice{Name: e.label, Value: e.value}
	}
	return out
}

// titleCase converts a lowercase canonical name to user-facing title case
// for dropdown labels. "giovanni" → "Giovanni", "gold-stop" → "Gold-Stop".
// Multi-word values pass through unchanged ("pokestop spawn" → "Pokestop
// Spawn") via per-word capitalisation on space and hyphen boundaries.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	upperNext := true
	for _, r := range s {
		switch {
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(r)
			upperNext = true
		case upperNext:
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			upperNext = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
