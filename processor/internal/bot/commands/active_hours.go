package commands

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// Sentinel errors returned by ParseSettimeArg. Callers wrap these via
// SettimeErrorMessage to get a localized user-facing message — the raw
// error.Error() text is English-only and intended for logs / debug
// output, not for surfacing back to end users.
//
// errUnknownDayPrefix wraps the offending prefix as a string suffix so
// callers can use errors.Is for classification while still recovering
// the bad value when formatting the user-facing message. We don't use
// a typed struct because the only metadata that varies is the prefix
// itself, captured below.
var (
	errUnknownDayPrefix      = errors.New("unknown day prefix")
	errTimeOutOfRange        = errors.New("time out of range")
	errEndBeforeOrEqualStart = errors.New("end time must be after start time (cross-midnight not supported)")
	errStepOutOfRange        = errors.New("step must be 1..23 hours")
)

// settimeError carries an unknown-prefix sentinel plus the offending
// prefix value, so SettimeErrorMessage can render a translated message
// like "unknown day prefix \"junk\"" without leaking the English
// template through fmt.Errorf.
type settimeError struct {
	err    error  // one of the sentinels above
	prefix string // populated for errUnknownDayPrefix
}

func (e *settimeError) Error() string {
	if e.err == errUnknownDayPrefix {
		return fmt.Sprintf("%s %q", e.err.Error(), e.prefix)
	}
	return e.err.Error()
}
func (e *settimeError) Unwrap() error { return e.err }

// SettimeErrorMessage maps a ParseSettimeArg error to its localized
// user-facing message via the supplied translator. Falls back to the
// raw error.Error() text (English) if the error isn't one we know how
// to translate.
//
// i18n keys used:
//
//	msg.settime.err_unknown_prefix  ({0} = bad prefix)
//	msg.settime.err_time_out_of_range
//	msg.settime.err_end_before_start
//	msg.settime.err_step_out_of_range
func SettimeErrorMessage(tr *i18n.Translator, err error) string {
	if err == nil {
		return ""
	}
	var se *settimeError
	if !errors.As(err, &se) {
		return err.Error()
	}
	switch se.err {
	case errUnknownDayPrefix:
		return tr.Tf("msg.settime.err_unknown_prefix", se.prefix)
	case errTimeOutOfRange:
		return tr.T("msg.settime.err_time_out_of_range")
	case errEndBeforeOrEqualStart:
		return tr.T("msg.settime.err_end_before_start")
	case errStepOutOfRange:
		return tr.T("msg.settime.err_step_out_of_range")
	}
	return err.Error()
}

// rangeRe matches the new "range + step" settime form:
//
//	[<dayPrefix>[:]]?HH[:MM]-HH[:MM][/N]
//
// Examples that match:
//
//	weekday:9-17/2     mon9:30-17/3      everyday:09:00-17:00/2
//	9-17               9-17/1            mon:9-17
//
// Capture groups:
//
//	1 = day prefix (optional; defaults to "every" / all days)
//	2 = start hours
//	3 = start minutes (optional; default "00")
//	4 = end hours
//	5 = end minutes (optional; default "00")
//	6 = step in hours (optional; default 1 = hourly)
//
// Single-fire forms (no dash) are parsed by settimeRe instead.
var rangeRe = regexp.MustCompile(`^([a-zA-Z]+)?:?(\d{1,2})(?::?(\d{2}))?-(\d{1,2})(?::?(\d{2}))?(?:/(\d{1,2}))?$`)

// settimeRe matches the single-fire settime form: [<dayPrefix>[:]]HH[:MM].
// Day prefix is optional — without it the parser defaults to "every"
// (all seven days), matching the range form's behaviour.
var settimeRe = regexp.MustCompile(`^([a-zA-Z]+)?:?(\d{1,2}):?(\d{2})?$`)

// ParseSettimeArg parses one settime token (e.g. "mon7:30",
// "weekday:9-17/2", "9-17") into the discrete-per-day ActiveHourEntry
// list that gets written to storage.
//
// Returns:
//   - (nil, nil) if the token matches neither the single-fire nor the
//     range regex (e.g. obvious junk like "not-a-time"). Callers
//     silently skip these — the previous behaviour was to drop them
//     without comment, and this preserves it.
//   - (nil, sentinel error) if the token matches a regex but fails
//     validation (unknown day prefix, out-of-range hours/mins,
//     cross-midnight range, step out of range). Wraps one of the
//     exported errXxx sentinels via settimeError; callers should
//     translate via SettimeErrorMessage rather than printing
//     err.Error() directly.
//
// Each returned entry has Day populated; range entries also carry
// EndHours/EndMins/Step so the scheduler tick can expand fires
// without duplicating the time-arithmetic at runtime.
func ParseSettimeArg(arg string, dayPrefixes map[string][]int) ([]db.ActiveHourEntry, error) {
	arg = strings.ToLower(arg)

	// Try the range form first (it's more specific — must contain a `-`).
	if m := rangeRe.FindStringSubmatch(arg); m != nil {
		prefix := m[1]
		if prefix == "" {
			prefix = "every"
		}
		days, ok := dayPrefixes[prefix]
		if !ok {
			return nil, &settimeError{err: errUnknownDayPrefix, prefix: prefix}
		}
		startH, _ := strconv.Atoi(m[2])
		startMin := 0
		if m[3] != "" {
			startMin, _ = strconv.Atoi(m[3])
		}
		endH, _ := strconv.Atoi(m[4])
		endMin := 0
		if m[5] != "" {
			endMin, _ = strconv.Atoi(m[5])
		}
		step := 1
		if m[6] != "" {
			step, _ = strconv.Atoi(m[6])
		}
		if startH > 23 || endH > 23 || startMin > 59 || endMin > 59 {
			return nil, &settimeError{err: errTimeOutOfRange}
		}
		startTotalMin := startH*60 + startMin
		endTotalMin := endH*60 + endMin
		if endTotalMin <= startTotalMin {
			// Equal or end-before-start: range must be non-empty
			// (we reject cross-midnight by policy; equal is also
			// nonsensical — use the single-fire form for one time).
			return nil, &settimeError{err: errEndBeforeOrEqualStart}
		}
		if step < 1 || step > 23 {
			return nil, &settimeError{err: errStepOutOfRange}
		}
		out := make([]db.ActiveHourEntry, 0, len(days))
		for _, d := range days {
			out = append(out, db.ActiveHourEntry{
				Day:      d,
				Hours:    startH,
				Mins:     startMin,
				EndHours: endH,
				EndMins:  endMin,
				Step:     step,
			})
		}
		return out, nil
	}

	// Fall through to single-fire form.
	if m := settimeRe.FindStringSubmatch(arg); m != nil {
		prefix := m[1]
		if prefix == "" {
			prefix = "every"
		}
		days, ok := dayPrefixes[prefix]
		if !ok {
			return nil, &settimeError{err: errUnknownDayPrefix, prefix: prefix}
		}
		h, _ := strconv.Atoi(m[2])
		min := 0
		if m[3] != "" {
			min, _ = strconv.Atoi(m[3])
		}
		if h > 23 || min > 59 {
			return nil, &settimeError{err: errTimeOutOfRange}
		}
		out := make([]db.ActiveHourEntry, 0, len(days))
		for _, d := range days {
			out = append(out, db.ActiveHourEntry{Day: d, Hours: h, Mins: min})
		}
		return out, nil
	}
	return nil, nil
}

// activeHoursDayKeys maps ISO weekday (1=Mon ... 7=Sun) to the i18n
// day-name key used by both the profile listing and the summary
// schedule display.
var activeHoursDayKeys = []string{
	"day.monday",
	"day.tuesday",
	"day.wednesday",
	"day.thursday",
	"day.friday",
	"day.saturday",
	"day.sunday",
}

// formatActiveHours renders parsed active_hours entries as one
// "<day> HH:MM" string per entry (or "<day> HH:MM-HH:MM/N" for the
// range form), joined with separator. Profile's listing path uses
// "\n" (one entry per indented line); summary's inline status uses
// ", " (comma list). Empty input returns "". Day numbers outside
// 1..7 render with an empty day name.
func formatActiveHours(tr *i18n.Translator, entries []db.ActiveHourEntry, separator string) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		day := ""
		if e.Day >= 1 && e.Day <= 7 {
			day = tr.T(activeHoursDayKeys[e.Day-1])
		}
		if e.IsRange() {
			parts = append(parts, fmt.Sprintf("%s %02d:%02d-%02d:%02d/%d", day, e.Hours, e.Mins, e.EndHours, e.EndMins, e.Step))
		} else {
			parts = append(parts, fmt.Sprintf("%s %02d:%02d", day, e.Hours, e.Mins))
		}
	}
	return strings.Join(parts, separator)
}
