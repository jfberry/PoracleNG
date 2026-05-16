package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Profile maps /profile sub-command invocations to the text-command tokens
// read by ProfileCommand in processor/internal/bot/commands/profile.go.
//
// The text grammar uses i18n keywords: `arg.list`, `arg.add <name>`,
// `arg.remove <name>` (also accepts the literal `delete`), `arg.switch <name>`
// — plus a fall-through that treats a bare `!profile <name>` as switch.
//
// We map the slash sub-commands to the canonical English tokens:
//
//	list   → list      (lists user's profiles)
//	change → switch <name>   (changes the active profile)
//	create → add <name>      (creates a new profile)
//	delete → remove <name>   (removes the named profile)
//
// English forms are used unconditionally because the text parser does an
// English-fallback lookup against `arg.add`/`arg.remove`/`arg.switch`/
// `arg.list` regardless of the user's language.
func Profile(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.profile.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.profile.no_subcommand"}
	}
	var name string
	for _, o := range sub.Options {
		if o != nil && o.Name == "name" {
			name = o.StringValue()
		}
	}
	switch sub.Name {
	case "list":
		return []string{"list"}, nil
	case "change":
		if name == "" {
			return nil, &MapperError{Key: "error.slash.profile.no_name"}
		}
		return []string{"switch", name}, nil
	case "create":
		if name == "" {
			return nil, &MapperError{Key: "error.slash.profile.no_name"}
		}
		return []string{"add", name}, nil
	case "delete":
		if name == "" {
			return nil, &MapperError{Key: "error.slash.profile.no_name"}
		}
		return []string{"remove", name}, nil
	}
	return nil, &MapperError{Key: "error.slash.profile.unknown_subcommand"}
}

func init() { registry["profile"] = Profile }
