package commands

import (
	"fmt"
	"runtime/debug"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type VersionCommand struct{}

func (c *VersionCommand) Name() string      { return "cmd.version" }
func (c *VersionCommand) Aliases() []string { return nil }

func (c *VersionCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	version, commit, date := "dev", "unknown", "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) > 7 {
					commit = s.Value[:7]
				} else {
					commit = s.Value
				}
			case "vcs.time":
				date = s.Value
			}
		}
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}

	text := fmt.Sprintf("PoracleNG %s (%s, %s)", version, commit, date)
	return []bot.Reply{{Text: text}}
}
