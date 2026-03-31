package commands

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/nlp"
)

// AskCommand implements !ask — converts natural language into Poracle commands.
type AskCommand struct{}

func (c *AskCommand) Name() string      { return "cmd.ask" }
func (c *AskCommand) Aliases() []string { return nil }

func (c *AskCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if ctx.NLP == nil {
		return []bot.Reply{{React: "\xf0\x9f\x99\x85"}}
	}

	if r := usageReply(ctx, args, "cmd.ask.usage"); r != nil {
		return []bot.Reply{*r}
	}

	input := strings.Join(args, " ")
	result := ctx.NLP.Parse(input)
	prefix := commandPrefix(ctx)

	suggestion := FormatNLPSuggestion(result, prefix)
	if suggestion != "" {
		return []bot.Reply{{Text: suggestion}}
	}

	if result.Error != "" {
		return []bot.Reply{{Text: result.Error}}
	}
	return []bot.Reply{{React: "\xf0\x9f\x99\x85"}}
}

// FormatNLPSuggestion formats an NLP parse result into a user-facing suggestion
// message. Returns empty string if the result cannot be presented as a suggestion.
func FormatNLPSuggestion(result nlp.ParseResult, prefix string) string {
	switch result.Status {
	case "ok":
		lines := strings.Split(result.Command, "\n")
		var sb strings.Builder
		sb.WriteString("Did you mean:\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			line = replacePrefix(line, prefix)
			sb.WriteString("`")
			sb.WriteString(line)
			sb.WriteString("`\n")
		}
		sb.WriteString("\nCopy and paste to run.")
		return sb.String()

	case "ambiguous":
		var sb strings.Builder
		sb.WriteString("Did you mean:\n")
		for i, opt := range result.Options {
			cmd := replacePrefix(opt.Command, prefix)
			sb.WriteString(fmt.Sprintf("%d. %s: `%s`\n", i+1, opt.Label, cmd))
		}
		sb.WriteString("\nCopy and paste the command you want.")
		return sb.String()
	}

	return ""
}

// replacePrefix swaps a leading "!" with the platform-appropriate command prefix.
func replacePrefix(cmd, prefix string) string {
	if strings.HasPrefix(cmd, "!") {
		return prefix + cmd[1:]
	}
	return cmd
}
