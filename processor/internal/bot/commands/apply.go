package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// ApplyCommand implements !apply <targetIDs> | <command> <args> | <command> <args> ...
// It executes commands as other users/channels/webhooks.
// The parser splits by "|" into groups:
//   group[0] = target IDs (space-separated)
//   group[1..n] = commands to execute
type ApplyCommand struct{}

func (c *ApplyCommand) Name() string      { return "cmd.apply" }
func (c *ApplyCommand) Aliases() []string { return nil }

// Run handles the apply command. Because the parser splits by "|", we receive
// only the first pipe group here. The remaining groups are handled by the
// bot framework dispatching multiple ParsedCommands with "cmd.apply" key.
//
// However, looking at the alerter's apply.js, it receives ALL pipe groups
// at once via the `commands` parameter (the apply special uses the raw
// multi-group parse). In our framework, apply needs to receive the full
// args including pipes. We handle this by consuming the first group as
// targets and executing subsequent groups as commands.
//
// Since the parser has already split by pipe, we need a different approach:
// The apply command receives ALL remaining args (the bot framework should
// NOT pipe-split for apply). We'll handle the raw args ourselves.
func (c *ApplyCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	if ctx.Registry == nil {
		log.Error("apply: no command registry available")
		return []bot.Reply{{React: "🙅"}}
	}

	// Split args by "|" to get groups
	var groups [][]string
	current := make([]string, 0)
	for _, arg := range args {
		if arg == "|" {
			if len(current) > 0 {
				groups = append(groups, current)
			}
			current = make([]string, 0)
		} else {
			current = append(current, arg)
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	// Need at least target group + one command group.
	// If no pipe found, the parser already split. Check if we have at least 2 groups.
	// If only 1 group, that's just targets with no commands.
	if len(groups) < 2 {
		return []bot.Reply{{Text: "Usage: apply <targets> | <command> <args> | <command> <args>"}}
	}

	targetArgs := groups[0]
	commandGroups := groups[1:]

	// Resolve targets
	type targetInfo struct {
		ID   string
		Name string
		Type string
	}
	var targets []targetInfo

	for _, targ := range targetArgs {
		// Try by ID
		h, err := ctx.Humans.Get(targ)
		if err == nil && h != nil {
			targets = append(targets, targetInfo{ID: h.ID, Name: h.Name, Type: h.Type})
		}

		// Try by name (webhooks, channels, groups)
		// This needs a name-based search that HumanStore doesn't expose yet,
		// so we fall back to direct DB for now.
		var byName []struct {
			ID   string `db:"id"`
			Name string `db:"name"`
			Type string `db:"type"`
		}
		err = ctx.DB.Select(&byName,
			`SELECT id, name, type FROM humans WHERE type IN ('webhook', 'discord:channel', 'telegram:channel', 'telegram:group') AND name LIKE ?`,
			targ)
		if err == nil {
			for _, bm := range byName {
				// Deduplicate
				found := false
				for _, t := range targets {
					if t.ID == bm.ID {
						found = true
						break
					}
				}
				if !found {
					targets = append(targets, targetInfo{ID: bm.ID, Name: bm.Name, Type: bm.Type})
				}
			}
		}
	}

	if len(targets) == 0 {
		return []bot.Reply{{Text: "No matching targets found"}}
	}

	var allReplies []bot.Reply

	for _, target := range targets {
		idDisplay := ""
		if target.Type != bot.TypeWebhook {
			idDisplay = " " + target.ID
		}
		allReplies = append(allReplies, bot.Reply{
			Text: fmt.Sprintf(">>> Executing as %s / %s%s", target.Type, target.Name, idDisplay),
		})

		for _, cmdGroup := range commandGroups {
			if len(cmdGroup) == 0 {
				continue
			}

			cmdName := cmdGroup[0]
			cmdArgs := cmdGroup[1:]

			allReplies = append(allReplies, bot.Reply{
				Text: fmt.Sprintf(">> %s", strings.Join(cmdGroup, " ")),
			})

			// Try multiple key formats: "cmd.X", "cmd.X" with hyphens→underscores
			handler := ctx.Registry.Lookup("cmd." + cmdName)
			if handler == nil {
				handler = ctx.Registry.Lookup("cmd." + strings.ReplaceAll(cmdName, "-", "_"))
			}
			if handler == nil {
				allReplies = append(allReplies, bot.Reply{Text: ">> Unknown command"})
				continue
			}

			// Build a new context with the target override
			targetCtx := *ctx // shallow copy
			targetCtx.TargetID = target.ID
			targetCtx.TargetName = target.Name
			targetCtx.TargetType = target.Type

			// Look up target's profile and language
			if targetHuman, err := ctx.Humans.Get(target.ID); err == nil && targetHuman != nil {
				targetCtx.ProfileNo = targetHuman.CurrentProfileNo
				if targetHuman.Language != "" {
					targetCtx.Language = targetHuman.Language
				}
			}

			replies := handler.Run(&targetCtx, cmdArgs)
			allReplies = append(allReplies, replies...)
		}
	}

	allReplies = append(allReplies, bot.Reply{React: "✅"})
	return allReplies
}
