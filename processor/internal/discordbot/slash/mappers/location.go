package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Location maps /location sub-command invocations to the text-command tokens
// read by LocationCommand in processor/internal/bot/commands/location.go.
//
// Sub-commands:
//
//	add <name> <place>     → ["add", name, place]   (geocoding done by text cmd)
//	list                   → ["list"]
//	show <name>            → ["show", name]
//	remove <name>          → ["remove", name]
//	set-default            → nil (no-op: needs a separate place arg not yet in UX)
//	remove-default         → ["remove", "default"]
func Location(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.location.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.location.no_subcommand"}
	}
	switch sub.Name {
	case "add":
		o := flattenOptions(sub.Options)
		name := getString(o["name"])
		place := getString(o["place"])
		if name == "" {
			return nil, &MapperError{Key: "error.slash.location.no_name"}
		}
		if place == "" {
			return nil, &MapperError{Key: "error.slash.location.empty"}
		}
		return []string{"add", name, place}, nil
	case "list":
		return []string{"list"}, nil
	case "show":
		if len(sub.Options) == 0 || sub.Options[0] == nil {
			return nil, &MapperError{Key: "error.slash.location.no_name"}
		}
		name := sub.Options[0].StringValue()
		if name == "" {
			return nil, &MapperError{Key: "error.slash.location.no_name"}
		}
		return []string{"show", name}, nil
	case "remove":
		if len(sub.Options) == 0 || sub.Options[0] == nil {
			return nil, &MapperError{Key: "error.slash.location.no_name"}
		}
		name := sub.Options[0].StringValue()
		if name == "" {
			return nil, &MapperError{Key: "error.slash.location.no_name"}
		}
		return []string{"remove", name}, nil
	case "set-default":
		// No place option on this sub-command yet — expose for discoverability
		// in the slash menu but take no action. The !location <lat,lon> text
		// form remains the primary way to set a default location.
		return nil, nil
	case "remove-default":
		return []string{"remove", "default"}, nil
	}
	return nil, &MapperError{Key: "error.slash.location.unknown_subcommand"}
}

func init() { registry["location"] = Location }
