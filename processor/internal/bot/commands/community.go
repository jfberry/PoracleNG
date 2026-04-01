package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// CommunityCommand implements !community -- admin-only community management.
// Subcommands: list, add, remove, show, clear
type CommunityCommand struct{}

func (c *CommunityCommand) Name() string      { return "cmd.community" }
func (c *CommunityCommand) Aliases() []string { return nil }

func (c *CommunityCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_permission")}}
	}

	if len(args) == 0 {
		return c.usageReply(ctx)
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case tr.T("arg.list"), "list":
		return c.runList(ctx)
	case tr.T("arg.add"), "add":
		return c.runAddRemove(ctx, subArgs, true)
	case "remove", tr.T("arg.remove"):
		return c.runAddRemove(ctx, subArgs, false)
	case tr.T("arg.show"), "show":
		return c.runShow(ctx, subArgs)
	case tr.T("arg.clear"), "clear":
		return c.runClear(ctx, subArgs)
	default:
		return c.usageReply(ctx)
	}
}

func (c *CommunityCommand) usageReply(ctx *bot.CommandContext) []bot.Reply {
	prefix := commandPrefix(ctx)
	tr := ctx.Tr()
	return []bot.Reply{{Text: tr.Tf("cmd.community.usage", prefix)}}
}

func (c *CommunityCommand) runList(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	names := bot.CommunityNames(ctx.Config)
	if len(names) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.community.none")}}
	}

	// Replace spaces with underscores for display (matching alerter)
	display := make([]string, len(names))
	for i, n := range names {
		display[i] = strings.ReplaceAll(n, " ", "_")
	}

	return []bot.Reply{
		{Text: tr.T("cmd.community.valid")},
		{Text: "```\n" + strings.Join(display, "\n") + "```"},
	}
}

func (c *CommunityCommand) runAddRemove(ctx *bot.CommandContext, args []string, isAdd bool) []bot.Reply {
	tr := ctx.Tr()
	if len(args) < 2 {
		return c.usageReply(ctx)
	}

	communityName := strings.ToLower(args[0])
	targetArgs := args[1:]

	targets := extractTargetIDs(targetArgs)
	if len(targets) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.community.no_targets")}}
	}

	var messages []string
	for _, id := range targets {
		human, err := ctx.Humans.Get(id)
		if err != nil {
			log.Errorf("community: select human %s: %v", id, err)
			continue
		}
		if human == nil {
			continue
		}

		var newCommunities []string
		if isAdd {
			newCommunities = bot.AddCommunity(ctx.Config, human.CommunityMembership, communityName)
			messages = append(messages, fmt.Sprintf("Add community %s to target %s %s", communityName, id, human.Name))
		} else {
			newCommunities = bot.RemoveCommunity(ctx.Config, human.CommunityMembership, communityName)
			messages = append(messages, fmt.Sprintf("Remove community %s from target %s %s", communityName, id, human.Name))
		}

		newRestrictions := bot.CalculateLocationRestrictions(ctx.Config, newCommunities)
		if err := ctx.Humans.SetCommunity(id, newCommunities, newRestrictions); err != nil {
			log.Errorf("community: update human %s: %v", id, err)
		}
	}

	ctx.TriggerReload()

	if len(messages) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	return []bot.Reply{{React: "✅", Text: strings.Join(messages, "\n")}}
}

func (c *CommunityCommand) runShow(ctx *bot.CommandContext, args []string) []bot.Reply {
	targets := extractTargetIDs(args)
	tr := ctx.Tr()
	if len(targets) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.community.no_targets")}}
	}

	var messages []string
	for _, id := range targets {
		human, err := ctx.Humans.Get(id)
		if err != nil {
			log.Errorf("community: select human %s: %v", id, err)
			continue
		}
		if human == nil {
			continue
		}

		communityJSON, _ := json.Marshal(human.CommunityMembership)
		restriction := "none"
		if human.AreaRestriction != nil {
			restrictionJSON, _ := json.Marshal(human.AreaRestriction)
			restriction = string(restrictionJSON)
		}
		messages = append(messages, fmt.Sprintf(
			"User target %s %s has communities %s location restrictions %s",
			id, human.Name, string(communityJSON), restriction))
	}

	if len(messages) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	return []bot.Reply{{React: "✅", Text: strings.Join(messages, "\n")}}
}

func (c *CommunityCommand) runClear(ctx *bot.CommandContext, args []string) []bot.Reply {
	targets := extractTargetIDs(args)
	tr := ctx.Tr()
	if len(targets) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.community.no_targets")}}
	}

	var messages []string
	for _, id := range targets {
		if err := ctx.Humans.SetCommunity(id, nil, nil); err != nil {
			log.Errorf("community: clear %s: %v", id, err)
			continue
		}
		messages = append(messages, fmt.Sprintf("Clear target %s", id))
		log.Infof("community: cleared communities for %s", id)
	}

	ctx.TriggerReload()

	if len(messages) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	return []bot.Reply{{React: "✅", Text: strings.Join(messages, "\n")}}
}

// extractTargetIDs extracts user IDs from mentions and plain numeric args.
func extractTargetIDs(args []string) []string {
	var targets []string
	seen := make(map[string]bool)
	for _, arg := range args {
		id := stripMention(arg)
		if id == "" {
			continue
		}
		if !seen[id] {
			seen[id] = true
			targets = append(targets, id)
		}
	}
	return targets
}
