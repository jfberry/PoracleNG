package autocomplete

import (
	"context"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// Template returns autocomplete choices for DTS template IDs for the
// given (commandType, platform). Each choice's Name embeds the template's
// description (when set) so the user can pick by intent rather than by
// remembering opaque IDs; the Value is the literal template ID the user
// would otherwise pass via `template:X`.
//
// userLang is accepted for future localisation of descriptions but is not
// consulted today — the underlying TemplateStore enumerates IDs that exist
// for the platform regardless of language, and the selection chain handles
// language fallback at render time.
func Template(ctx context.Context, deps *bot.BotDeps, focused, commandType, platform, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.DTS == nil {
		return nil
	}
	infos := deps.DTS.ListForPlatform(platform)[commandType]
	if len(infos) == 0 {
		return nil
	}

	// Default templates first, then alphabetical by ID — defaults are what
	// most users want, and stable ordering avoids dropdown flicker on
	// repeated invocations.
	sorted := append([]dts.UserTemplateInfo(nil), infos...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].IsDefault != sorted[j].IsDefault {
			return sorted[i].IsDefault
		}
		return sorted[i].ID < sorted[j].ID
	})

	focusedLower := strings.ToLower(strings.TrimSpace(focused))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, info := range sorted {
		label := templateChoiceLabel(info)
		if focusedLower != "" &&
			!strings.Contains(strings.ToLower(info.ID), focusedLower) &&
			!strings.Contains(strings.ToLower(label), focusedLower) {
			continue
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: label, Value: info.ID})
		if len(out) == 25 {
			break
		}
	}
	return out
}

// templateChoiceLabel composes a Discord-friendly choice name from a
// template's ID + Description (and a ⭐ marker for defaults). Trimmed to
// Discord's 100-char choice-name limit, preserving the ID prefix so the
// user can always see which template they're selecting.
func templateChoiceLabel(info dts.UserTemplateInfo) string {
	prefix := info.ID
	if info.IsDefault {
		prefix = "⭐ " + prefix
	}
	if info.Description == "" {
		return truncateChoiceName(prefix)
	}
	return truncateChoiceName(prefix + " — " + info.Description)
}

// truncateChoiceName clips a label to Discord's 100-character choice-name
// limit, appending an ellipsis when truncated.
func truncateChoiceName(s string) string {
	const max = 100
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// filterStringChoices substring-filters names by focused (case-insensitive)
// and caps the result at Discord's 25-choice limit. Exposed for tests and
// for future option-typed autocompletes that already have a flat string
// list in hand.
func filterStringChoices(names []string, focused string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, n := range names {
		if focused != "" && !strings.Contains(strings.ToLower(n), focused) {
			continue
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: n, Value: n})
		if len(out) == 25 {
			break
		}
	}
	return out
}
