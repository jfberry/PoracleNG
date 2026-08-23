package api

import (
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// SummaryDeps groups the dependencies for the /api/summaries endpoints.
// Kept narrow on purpose so tests can swap in mocks without dragging the
// full TrackingDeps surface in.
type SummaryDeps struct {
	// Schedules backs Get/Set/Delete/ListByType. nil disables CRUD —
	// handlers respond 503 so callers know the feature is off rather
	// than 404 (which would be misleading).
	Schedules store.SummaryScheduleStore
	// Dispatch is invoked synchronously by the trigger endpoint to flush
	// the buffer for (humanID, alertType). nil disables the trigger
	// endpoint with a 503.
	Dispatch func(humanID, alertType string)
	// ReloadFunc is called after Set / Delete so an in-flight scheduler
	// tick picks up the change without waiting for the next periodic
	// reload. nil is tolerated.
	ReloadFunc func()
}

// summarySetRequest is the POST body shape. We accept either a stringified
// JSON value or an arbitrary structure; both flow through json.Marshal so
// the schedule store always sees a canonical JSON-encoded string.
type summarySetRequest struct {
	ActiveHours any `json:"active_hours"`
}

// activeHourEntry is the typed active-hours entry returned by the summary GET
// endpoints. The json tags match db.ActiveHourEntry exactly so the array shape
// round-trips with the stored value and the scheduler. Step>0 marks a range
// entry (with end_hours/end_mins); step absent/0 is a single-fire entry.
type activeHourEntry struct {
	Day      int `json:"day" doc:"Day of week (0=Sunday … 6=Saturday)"`
	Hours    int `json:"hours" doc:"Start hour (0–23)"`
	Mins     int `json:"mins" doc:"Start minute (0–59)"`
	EndHours int `json:"end_hours,omitempty" doc:"End hour (0–23); present on range entries (step>0)"`
	EndMins  int `json:"end_mins,omitempty" doc:"End minute (0–59); present on range entries (step>0)"`
	Step     int `json:"step,omitempty" doc:"Step in hours; >0 makes this a range entry"`
}

// summaryScheduleResponse is the JSON shape returned by GET endpoints.
// We keep it stable independent of the SummarySchedule struct so adding
// internal fields doesn't leak through to API consumers. active_hours is a
// typed JSON array (empty array when no schedule is stored).
type summaryScheduleResponse struct {
	ID          string            `json:"id"`
	AlertType   string            `json:"alert_type"`
	ActiveHours []activeHourEntry `json:"active_hours"`
}

func toSummaryResponse(s *store.SummarySchedule) summaryScheduleResponse {
	// active_hours is stored as a JSON-array string; parse it into the typed
	// array. A parse failure (malformed stored data) degrades to an empty
	// array rather than leaking raw bytes — the schedule is still listed.
	hours := make([]activeHourEntry, 0)
	if parsed, err := db.ParseActiveHours(s.ActiveHours); err != nil {
		log.Warnf("Summary API: %s/%s active_hours parse: %v", s.ID, s.AlertType, err)
	} else {
		for _, e := range parsed {
			hours = append(hours, activeHourEntry{
				Day:      e.Day,
				Hours:    e.Hours,
				Mins:     e.Mins,
				EndHours: e.EndHours,
				EndMins:  e.EndMins,
				Step:     e.Step,
			})
		}
	}
	return summaryScheduleResponse{
		ID:          s.ID,
		AlertType:   s.AlertType,
		ActiveHours: hours,
	}
}

// knownSummaryAlertTypes lists the alert types that have a summary
// renderer wired in DispatchQuestSummary. The list-for-user endpoint
// iterates these so we don't need a new "list by id" store method.
var knownSummaryAlertTypes = []string{"quest"}

// isKnownSummaryAlertType is the membership test used by every handler
// that accepts an alertType path parameter. Returning a 400 for unknown
// values lets clients building tooling get a clear signal instead of a
// silent 200-with-no-effect (DispatchQuestSummary itself no-ops on
// unknown alert types).
func isKnownSummaryAlertType(t string) bool {
	return slices.Contains(knownSummaryAlertTypes, t)
}
