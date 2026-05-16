package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Summary maps /summary sub-command-group invocations to the text-command
// tokens read by SummaryCommand in processor/internal/bot/commands/summary.go.
//
// The text grammar is `!summary <alertType> [settime <times>|cleartime|now]`
// where the bare `<alertType>` form (no further action) renders the
// current schedule and buffer count.
//
// Slash structure:
//
//	/summary
//	  <alertType>             ← sub-command group (today: only `quest`)
//	    show                  ← bare-alertType status
//	    settime <times>       ← set schedule
//	    cleartime             ← clear schedule
//	    now                   ← force-dispatch buffer
//
// Token emission:
//
//	show       → [alertType]
//	settime    → [alertType, "settime", <times>]
//	cleartime  → [alertType, "cleartime"]
//	now        → [alertType, "now"]
//
// The text bot does an English-fallback lookup against `arg.settime`,
// `arg.cleartime`, and `arg.now` (see commands/summary.go), so emitting the
// canonical English tokens here works regardless of the user's language.
func Summary(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.summary.no_alert_type"}
	}
	group := opts[0]
	if group == nil || group.Type != discordgo.ApplicationCommandOptionSubCommandGroup {
		return nil, &MapperError{Key: "error.slash.summary.no_alert_type"}
	}
	alertType := group.Name

	if len(group.Options) == 0 {
		return nil, &MapperError{Key: "error.slash.summary.no_action"}
	}
	action := group.Options[0]
	if action == nil || action.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.summary.no_action"}
	}

	switch action.Name {
	case "show":
		return []string{alertType}, nil
	case "settime":
		var times string
		for _, o := range action.Options {
			if o != nil && o.Name == "times" {
				times = o.StringValue()
			}
		}
		if times == "" {
			return nil, &MapperError{Key: "error.slash.summary.no_times"}
		}
		return []string{alertType, "settime", times}, nil
	case "cleartime":
		return []string{alertType, "cleartime"}, nil
	case "now":
		return []string{alertType, "now"}, nil
	}
	return nil, &MapperError{Key: "error.slash.summary.unknown_action"}
}

func init() { registry["summary"] = Summary }
