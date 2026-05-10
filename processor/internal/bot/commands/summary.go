package commands

import (
	"encoding/json"
	"fmt"
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
// The settime parser reuses `buildDayPrefixMap` + `settimeRe` from the
// profile command so users learn one syntax (`mon07:30`, `weekday07:30`,
// `weekend10:00`).
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
		lines = append(lines, tr.Tf("msg.summary.scheduled", alertType, formatActiveHours(tr, schedule.ParsedActiveHours)))
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

// setTime parses day/time tokens and stores them as a JSON-encoded
// active_hours array via SummaryScheduleStore.Set. Reuses the profile
// settime parser so users see one syntax across the bot.
func (c *SummaryCommand) setTime(ctx *bot.CommandContext, alertType string, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := bot.CommandPrefix(ctx)

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.summary.usage", prefix)}}
	}

	type entry struct {
		Day   int    `json:"day"`
		Hours string `json:"hours"`
		Mins  string `json:"mins"`
	}
	var entries []entry

	dayPrefixes := buildDayPrefixMap(ctx)

	for _, arg := range args {
		m := settimeRe.FindStringSubmatch(strings.ToLower(arg))
		if m == nil {
			continue
		}
		dayKey, hours, mins := m[1], m[2], m[3]
		if mins == "" {
			mins = "00"
		}
		days, ok := dayPrefixes[dayKey]
		if !ok {
			continue
		}
		for _, d := range days {
			entries = append(entries, entry{Day: d, Hours: hours, Mins: mins})
		}
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

	// Build a friendly representation of what was just stored.
	dbEntries := make([]ahForDisplay, 0, len(entries))
	for _, e := range entries {
		dbEntries = append(dbEntries, ahForDisplay{Day: e.Day, Hours: e.Hours, Mins: e.Mins})
	}
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.summary.updated", alertType, formatActiveHoursDisplay(tr, dbEntries))}}
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

// ahForDisplay mirrors the lightweight {day,hours,mins} shape used for
// rendering both freshly-set and DB-loaded schedules.
type ahForDisplay struct {
	Day   int
	Hours string
	Mins  string
}

// formatActiveHoursDisplay renders []ahForDisplay using day-name keys.
func formatActiveHoursDisplay(tr interface {
	T(key string) string
}, entries []ahForDisplay) string {
	dayKeys := []string{
		"day.monday", "day.tuesday", "day.wednesday",
		"day.thursday", "day.friday", "day.saturday", "day.sunday",
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		var dayName string
		if e.Day >= 1 && e.Day <= 7 {
			dayName = tr.T(dayKeys[e.Day-1])
		} else {
			dayName = fmt.Sprintf("day%d", e.Day)
		}
		parts = append(parts, fmt.Sprintf("%s %s:%s", dayName, e.Hours, e.Mins))
	}
	return strings.Join(parts, ", ")
}

// formatActiveHours renders []db.ActiveHourEntry (the parsed shape used
// by the schedule store). Mirrors formatActiveHoursDisplay but for the
// integer-based parsed entries.
func formatActiveHours(tr interface {
	T(key string) string
}, entries []db.ActiveHourEntry) string {
	disp := make([]ahForDisplay, 0, len(entries))
	for _, e := range entries {
		disp = append(disp, ahForDisplay{
			Day:   e.Day,
			Hours: fmt.Sprintf("%02d", e.Hours),
			Mins:  fmt.Sprintf("%02d", e.Mins),
		})
	}
	return formatActiveHoursDisplay(tr, disp)
}
