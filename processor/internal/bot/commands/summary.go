package commands

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// SummaryCommand implements `!summary <alertType> [settime <times>|cleartime|now]`.
//
// Currently the only supported alertType is "quest" (the only renderer we
// have). Subcommands:
//   - `!summary quest`              — show schedule + buffer count
//   - `!summary quest settime <…>`  — set/replace the active_hours schedule
//   - `!summary quest cleartime`    — remove the schedule
//   - `!summary quest now`          — force-dispatch the buffer immediately
//
// The settime parser is the shared `ParseSettimeArg` helper (alongside
// `buildDayPrefixMap`) so users learn one syntax across the bot —
// single-fire (`mon07:30`, `weekday07:30`, `weekend10:00`) and the
// range+step form (`weekday:9-17/2`) both work here.
type SummaryCommand struct{}

func (c *SummaryCommand) Name() string      { return "cmd.summary" }
func (c *SummaryCommand) Aliases() []string { return nil }

// supportedSummaryAlertTypes lists alertType values that have a renderer
// installed in DispatchQuestSummary. Other types are rejected with a
// clear error rather than accepted-and-ignored.
var supportedSummaryAlertTypes = map[string]bool{
	"quest": true,
}

func (c *SummaryCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	if ctx.SummarySchedules == nil {
		// Feature flag off: no store wired.
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	// First positional arg = alertType. Remaining args = subcommand + body.
	alertType := strings.ToLower(args[0])
	rest := args[1:]

	if !supportedSummaryAlertTypes[alertType] {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	// No subcommand → show status.
	if len(rest) == 0 {
		return c.showStatus(ctx, alertType)
	}

	subcommand := rest[0]
	body := rest[1:]

	enTr := ctx.Translations.For("en")
	matchSub := func(key string) bool {
		return subcommand == strings.ToLower(tr.T(key)) || subcommand == strings.ToLower(enTr.T(key))
	}

	switch {
	case matchSub("arg.settime"):
		return c.setTime(ctx, alertType, body)
	case matchSub("arg.cleartime"):
		return c.clearTime(ctx, alertType)
	case matchSub("arg.now"):
		return c.now(ctx, alertType)
	default:
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}
}

// showStatus renders the schedule (if any) and buffer count for the
// (TargetID, alertType) pair.
func (c *SummaryCommand) showStatus(ctx *bot.CommandContext, alertType string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	schedule, err := ctx.SummarySchedules.Get(ctx.TargetID, alertType)
	if err != nil {
		log.Errorf("summary status: get schedule: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var lines []string
	if schedule != nil && len(schedule.ParsedActiveHours) > 0 {
		lines = append(lines, tr.Tf("msg.summary.scheduled", alertType, formatActiveHours(tr, schedule.ParsedActiveHours, ", ")))
		// Append a one-liner explaining which timezone the schedule
		// is being evaluated in, with the current local time. The
		// target's lat/lon is fetched from the humans store so the
		// annotation matches what the scheduler actually does — there
		// is no canned "user timezone" cached anywhere.
		if tzLine := timezoneAnnotation(ctx, alertType); tzLine != "" {
			lines = append(lines, tzLine)
		}
	} else {
		lines = append(lines, tr.Tf("msg.summary.no_schedule", alertType))
	}

	count := 0
	if ctx.SummaryBufferCount != nil {
		count = ctx.SummaryBufferCount(ctx.TargetID, alertType)
	}
	if count == 0 {
		lines = append(lines, tr.Tf("msg.summary.no_buffered", alertType))
	} else {
		lines = append(lines, tr.Tf("msg.summary.buffer_count", count, alertType, prefix))
	}
	return []bot.Reply{{Text: strings.Join(lines, "\n")}}
}

// timezoneAnnotation looks up the target's lat/lon from the humans
// store and builds the localized "Times are in <tz>; currently HH:MM"
// suffix. Returns "" on any error (the schedule display is still
// useful without the annotation). _alertType is reserved for future
// per-alert-type timezone overrides.
func timezoneAnnotation(ctx *bot.CommandContext, _alertType string) string {
	if ctx.Humans == nil || ctx.Config == nil {
		return ""
	}
	human, err := ctx.Humans.Get(ctx.TargetID)
	if err != nil || human == nil {
		return ""
	}
	return FormatScheduleTimezone(ctx.Tr(), human.Latitude, human.Longitude, ctx.Config.General.DefaultTimezone)
}

// setTime parses day/time tokens and stores them as a JSON-encoded
// active_hours array via SummaryScheduleStore.Set. Reuses the profile
// settime parser so users see one syntax across the bot.
func (c *SummaryCommand) setTime(ctx *bot.CommandContext, alertType string, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	dayPrefixes := buildDayPrefixMap(ctx)
	var entries []db.ActiveHourEntry
	for _, arg := range args {
		parsed, err := ParseSettimeArg(arg, dayPrefixes)
		if err != nil {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.settime_invalid", arg, SettimeErrorMessage(tr, err))}}
		}
		entries = append(entries, parsed...)
	}

	if len(entries) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		log.Errorf("summary settime: marshal: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if err := ctx.SummarySchedules.Set(ctx.TargetID, alertType, string(data)); err != nil {
		log.Errorf("summary settime: store.Set: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	// Round-trip the JSON we just stored back through ParseActiveHours so
	// the friendly display goes through the same flex-typed parser the
	// store uses on read — one canonical decode path.
	parsed, _ := db.ParseActiveHours(string(data))
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.summary.updated", alertType, formatActiveHours(tr, parsed, ", "))}}
}

// clearTime removes the schedule via SummaryScheduleStore.Delete.
func (c *SummaryCommand) clearTime(ctx *bot.CommandContext, alertType string) []bot.Reply {
	tr := ctx.Tr()
	if err := ctx.SummarySchedules.Delete(ctx.TargetID, alertType); err != nil {
		log.Errorf("summary cleartime: store.Delete: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.summary.cleared", alertType)}}
}

// now invokes the dispatch callback to force-flush the buffer for the
// (TargetID, alertType) pair. Reports buffered count up-front so users
// understand what was triggered.
func (c *SummaryCommand) now(ctx *bot.CommandContext, alertType string) []bot.Reply {
	tr := ctx.Tr()

	count := 0
	if ctx.SummaryBufferCount != nil {
		count = ctx.SummaryBufferCount(ctx.TargetID, alertType)
	}
	if count == 0 {
		return []bot.Reply{{React: "👌", Text: tr.Tf("msg.summary.no_buffered", alertType)}}
	}

	if ctx.SummaryDispatch != nil {
		ctx.SummaryDispatch(ctx.TargetID, alertType)
	}
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.summary.delivered", alertType)}}
}

