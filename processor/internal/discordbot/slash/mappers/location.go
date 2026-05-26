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
//	set-default <place>    → [place]                (bare !location <place> sets default)
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
		var place string
		for _, o := range sub.Options {
			if o.Name == "place" {
				place = o.StringValue()
			}
		}
		if place == "" {
			return nil, &MapperError{Key: "error.slash.location.empty"}
		}
		// The bare !location <place> form sets the default — translate to that.
		return []string{place}, nil
	case "remove-default":
		return []string{"remove", "default"}, nil
	}
	return nil, &MapperError{Key: "error.slash.location.unknown_subcommand"}
}

func init() { registry["location"] = Location }
