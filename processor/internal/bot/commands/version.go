package commands

import (
	"fmt"

	"github.com/pokemon/poracleng/processor"
	"github.com/pokemon/poracleng/processor/internal/bot"
)

type VersionCommand struct{}

func (c *VersionCommand) Name() string      { return "cmd.version" }
func (c *VersionCommand) Aliases() []string { return nil }

func (c *VersionCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	_, commit, _, date := processor.BuildInfo()
	text := fmt.Sprintf("PoracleNG %s (%s, %s)", processor.DisplayVersion(), commit, date)
	return []bot.Reply{{Text: text}}
}
