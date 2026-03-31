package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// StartCommand implements !start — enables alert delivery for the user.
type StartCommand struct{}

func (c *StartCommand) Name() string      { return "cmd.start" }
func (c *StartCommand) Aliases() []string { return nil }

func (c *StartCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	_, err := ctx.DB.Exec(
		"UPDATE humans SET enabled = 1, fails = 0 WHERE id = ? AND type = ?",
		ctx.TargetID, ctx.TargetType,
	)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.T("cmd.start.success")}}
}

// StopCommand implements !stop — disables alert delivery for the user.
type StopCommand struct{}

func (c *StopCommand) Name() string      { return "cmd.stop" }
func (c *StopCommand) Aliases() []string { return nil }

func (c *StopCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := commandPrefix(ctx)

	if len(args) > 0 {
		// User typed "!stop pokemon" or similar — warn them and do NOT stop
		return []bot.Reply{{
			React: "🙅",
			Text:  tr.Tf("cmd.stop.warn_args", prefix, tr.T("cmd.stop")),
		}}
	}

	_, err := ctx.DB.Exec(
		"UPDATE humans SET enabled = 0 WHERE id = ? AND type = ?",
		ctx.TargetID, ctx.TargetType,
	)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.stop.success", prefix, tr.T("cmd.start"))}}
}
