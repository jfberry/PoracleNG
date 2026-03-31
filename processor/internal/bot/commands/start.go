package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// StartCommand implements !start — enables alert delivery for the user.
type StartCommand struct{}

func (c *StartCommand) Name() string      { return "cmd.start" }
func (c *StartCommand) Aliases() []string { return nil }

func (c *StartCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	_, err := ctx.DB.Exec(
		"UPDATE humans SET enabled = 1, fails = 0 WHERE id = ? AND type = ?",
		ctx.TargetID, ctx.TargetType,
	)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	return []bot.Reply{{React: "✅"}}
}

// StopCommand implements !stop — disables alert delivery for the user.
type StopCommand struct{}

func (c *StopCommand) Name() string      { return "cmd.stop" }
func (c *StopCommand) Aliases() []string { return nil }

func (c *StopCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) > 0 {
		// Common mistake: !stop pokemon — warn the user
		tr := ctx.Tr()
		return []bot.Reply{{
			React: "🙅",
			Text:  tr.T("cmd.stop.has_args"),
		}}
	}
	_, err := ctx.DB.Exec(
		"UPDATE humans SET enabled = 0 WHERE id = ? AND type = ?",
		ctx.TargetID, ctx.TargetType,
	)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	return []bot.Reply{{React: "✅"}}
}
