package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Profile maps /profile sub-command invocations to the text-command tokens
// read by ProfileCommand in processor/internal/bot/commands/profile.go.
//
// The text grammar uses i18n keywords: `arg.list`, `arg.add <name>`,
// `arg.remove <name>` (also accepts the literal `delete`), `arg.switch <name>`,
// `arg.settime <times>`, `arg.cleartime`, `arg.copyto <target>` — plus a
// fall-through that treats a bare `!profile <name>` as switch.
//
// We map the slash sub-commands to the canonical English tokens:
//
//	list      → list                (lists user's profiles)
//	change    → switch <name>       (changes the active profile)
//	create    → add <name>          (creates a new profile)
//	delete    → remove <name>       (removes the named profile)
//	settime   → settime <times>     (sets active-hours)
//	cleartime → cleartime           (clears active-hours)
//	copyto    → copyto <target>     (copies this profile's tracking to target)
//
// English forms are used unconditionally because the text parser does an
// English-fallback lookup against `arg.*` regardless of the user's language.
//
// Note on settime: the whole `times` string is passed verbatim as one token.
// ParseSettimeArgs splits it on commas and whitespace, so
// `"mon07:30,weekday09-17/2"` sets both. (Until the split existed this
// comment was aspirational — a single ParseSettimeArg call matched neither
// form and silently set nothing.)
func Profile(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.profile.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.profile.no_subcommand"}
	}
	var name, times, target string
	for _, o := range sub.Options {
		if o == nil {
			continue
		}
		switch o.Name {
		case "name":
			name = o.StringValue()
		case "times":
			times = o.StringValue()
		case "profile":
			target = o.StringValue()
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
	case "settime":
		if times == "" {
			return nil, &MapperError{Key: "error.slash.profile.no_times"}
		}
		return []string{"settime", times}, nil
	case "cleartime":
		return []string{"cleartime"}, nil
	case "copyto":
		if target == "" {
			return nil, &MapperError{Key: "error.slash.profile.no_target"}
		}
		return []string{"copyto", target}, nil
	}
	return nil, &MapperError{Key: "error.slash.profile.unknown_subcommand"}
}

func init() { registry["profile"] = Profile }
