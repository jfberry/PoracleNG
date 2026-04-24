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
	if err := ctx.Humans.SetEnabledWithFails(ctx.TargetID); err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.T("msg.start.success")}}
}

// StopCommand implements !stop — disables alert delivery for the user.
type StopCommand struct{}

func (c *StopCommand) Name() string      { return "cmd.stop" }
func (c *StopCommand) Aliases() []string { return nil }

func (c *StopCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	if len(args) > 0 {
		// User typed "!stop pokemon" or similar — warn them and do NOT stop
		return []bot.Reply{{
			React: "🙅",
			Text:  tr.Tf("msg.stop.warn_args", prefix, tr.T("cmd.stop")),
		}}
	}

	if err := ctx.Humans.SetEnabled(ctx.TargetID, false); err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.stop.success", prefix, tr.T("cmd.start"))}}
}
