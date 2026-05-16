package autocomplete

import (
	"context"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// Template returns autocomplete choices for DTS template IDs for the
// given (commandType, platform). userLang is accepted for future
// localisation of template descriptions but is not currently consulted —
// the underlying TemplateStore enumerates IDs that exist for the
// platform regardless of language, and the selection chain handles
// language fallback at render time.
//
// The values returned are the literal template IDs the user would pass
// via `template:X` — i.e. the same strings the renderer accepts.
func Template(ctx context.Context, deps *bot.BotDeps, focused, commandType, platform, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.DTS == nil {
		return nil
	}
	detailed := deps.DTS.TemplateSummaryDetailed()
	byPlatform := detailed[commandType]
	if byPlatform == nil {
		return nil
	}
	names := byPlatform[platform]
	// Sort for stable autocomplete output regardless of map iteration order.
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return filterStringChoices(sorted, focused)
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
