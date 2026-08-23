package autocomplete

import (
	"sort"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// HelpTopic returns autocomplete choices for /help's topic option: the ids of
// the installed "help" DTS entries.
//
// Both the requested platform AND the platform-agnostic ("") bucket are read.
// That is not defensive tidiness — every shipped help entry in fallbacks/dts/
// help/ carries `"platform": ""`, so a platform-only lookup returns an empty
// list on a stock install, which is precisely the symptom this fixes.
//
// Admin-only topics are withheld from non-admins: the command answers those
// with the 🙅 unknown-topic reply, and offering a suggestion that is then
// refused is worse than offering nothing.
func HelpTopic(deps *bot.BotDeps, focused, platform string, isAdmin bool) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || deps.DTS == nil {
		return nil
	}

	seen := make(map[string]bool)
	var ids []string
	collect := func(p string) {
		for _, info := range deps.DTS.ListForPlatform(p)["help"] {
			if info.ID == "" || seen[info.ID] {
				continue
			}
			if !isAdmin && bot.IsAdminOnlyHelpTopic(info.ID) {
				continue
			}
			seen[info.ID] = true
			ids = append(ids, info.ID)
		}
	}
	collect(platform)
	if platform != "" {
		collect("")
	}
	if len(ids) == 0 {
		return nil
	}

	sort.Strings(ids)
	return filterStringChoices(ids, focused)
}
