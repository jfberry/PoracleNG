package autocomplete

import (
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// PrependActivePokemon prepends currently-active pokemon names to a choice
// list. Active IDs come from a tracker.RecentActivity bucket (e.g.
// ActiveMaxBattleBosses, ActiveQuestCandy, ActiveQuestPokemon,
// ActiveQuestMega) — the boost surfaces species that have been seen by the
// processor in the last 6h so users picking from `/maxbattle pokemon` or
// `/quest candy` start with what's actually happening on the map.
//
// Mirrors RaidBoss: caps the boost at 10 entries, dedups against the
// underlying list, and short-circuits at 25 choices (Discord's hard cap).
// Choice Value is the canonical lowercase English name so downstream
// parsers see what the standard Pokemon autocomplete would have emitted.
func PrependActivePokemon(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, activeIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || len(activeIDs) == 0 {
		return base
	}
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
	for i, id := range activeIDs {
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
	for _, c := range base {
		v, _ := c.Value.(string)
		if add(c.Name, v) {
			return out
		}
	}
	return out
}

// PrependActiveItems prepends currently-active quest items to a choice
// list. Active IDs come from tracker.RecentActivity.ActiveQuestItems(),
// which records item IDs from quest webhooks. Label/Value both use the
// translated item name (the Value contract Item autocomplete uses, so
// matchItemName resolves cleanly against translated names).
func PrependActiveItems(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, activeIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || len(activeIDs) == 0 || deps.Translations == nil {
		return base
	}
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
	userTr := deps.Translations.For(userLang)
	enTr := deps.Translations.For("en")
	for i, id := range activeIDs {
		if i >= 10 {
			break
		}
		name := itemNameForLang(userTr, id)
		if name == "" {
			name = itemNameForLang(enTr, id)
		}
		if name == "" {
			continue
		}
		if add(name, name) {
			return out
		}
	}
	for _, c := range base {
		v, _ := c.Value.(string)
		if add(c.Name, v) {
			return out
		}
	}
	return out
}

// PrependActiveGrunts prepends currently-active grunt types to a choice
// list. Active IDs are grunt TypeIDs (the same ID space the webhook
// handler passes to RecordInvasionGrunt). We resolve each TypeID to the
// canonical English name via gamedata.TypeNameFromTemplate on the matching
// Grunt entry, then label it "<Translated Type> Grunt" — matching the
// label scheme used by autocomplete.Grunt for typed grunts.
//
// Skips IDs that don't resolve to a registered Grunt or to a translated
// type name (e.g. boss-only IDs that have no TypeID > 0, or unrecognised
// type IDs).
func PrependActiveGrunts(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, activeIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || len(activeIDs) == 0 || deps.GameData == nil {
		return base
	}
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
	userTr := deps.Translations.For(userLang)
	enTr := deps.Translations.For("en")

	// Reverse index from TypeID → first matching Grunt (any gender works
	// — Template-derived canonical name is gender-agnostic). Active IDs
	// are TypeIDs, but Grunts map is keyed by gruntTypeId (the slot ID
	// per gender). Walk the map once and match on .TypeID.
	gruntByTypeID := map[int]*gamedata.Grunt{}
	for _, g := range deps.GameData.Grunts {
		if g == nil || g.TypeID == 0 {
			continue
		}
		if _, exists := gruntByTypeID[g.TypeID]; !exists {
			gruntByTypeID[g.TypeID] = g
		}
	}

	for i, id := range activeIDs {
		if i >= 10 {
			break
		}
		g, ok := gruntByTypeID[id]
		if !ok {
			continue
		}
		canonical := strings.ToLower(gamedata.TypeNameFromTemplate(g.Template))
		if canonical == "" {
			continue
		}
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
		if typeName == "" {
			continue
		}
		if add(typeName+" Grunt", canonical) {
			return out
		}
	}
	for _, c := range base {
		v, _ := c.Value.(string)
		if add(c.Name, v) {
			return out
		}
	}
	return out
}
