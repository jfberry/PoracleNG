package commands

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

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
// "<day> HH:MM" string per entry, joined with separator. Profile's
// listing path uses "\n" (one entry per indented line); summary's
// inline status uses ", " (comma list). Empty input returns "".
// Day numbers outside 1..7 render with an empty day name.
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
		parts = append(parts, fmt.Sprintf("%s %02d:%02d", day, e.Hours, e.Mins))
	}
	return strings.Join(parts, separator)
}
