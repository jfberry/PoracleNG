package mappers

import "github.com/bwmarrin/discordgo"

// Account maps /account sub-commands to !account tokens. Values are passed
// through verbatim: player IDs are case-sensitive.
//
//	list                      → ["list"]
//	add <player_id> [label]   → ["add", id, label?]
//	remove <account>          → ["remove", account]
//	label <account> [label]   → ["label", account, label?]
func Account(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 || opts[0] == nil || opts[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.account.no_subcommand"}
	}
	sub := opts[0]
	o := flattenOptions(sub.Options)
	switch sub.Name {
	case "list":
		return []string{"list"}, nil
	case "add":
		id := getString(o["player_id"])
		if id == "" {
			return nil, &MapperError{Key: "error.slash.account.no_player_id"}
		}
		tokens := []string{"add", id}
		if l := getString(o["label"]); l != "" {
			tokens = append(tokens, l)
		}
		return tokens, nil
	case "remove":
		ref := getString(o["account"])
		if ref == "" {
			return nil, &MapperError{Key: "error.slash.account.no_account"}
		}
		return []string{"remove", ref}, nil
	case "label":
		ref := getString(o["account"])
		if ref == "" {
			return nil, &MapperError{Key: "error.slash.account.no_account"}
		}
		tokens := []string{"label", ref}
		if l := getString(o["label"]); l != "" {
			tokens = append(tokens, l)
		}
		return tokens, nil
	}
	return nil, &MapperError{Key: "error.slash.account.no_subcommand"}
}

func init() { registry["account"] = Account }
