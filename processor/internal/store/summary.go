package store

import "github.com/pokemon/poracleng/processor/internal/db"

// SummarySchedule represents a row from the summary_schedules table.
// It binds a (humanID, alertType) pair to a JSON-encoded list of active
// hours that determines when summaries fire for that user/type.
type SummarySchedule struct {
	ID          string
	AlertType   string
	ActiveHours string // raw JSON

	// ParsedActiveHours is computed after load (not a DB column). Reuses
	// the existing profile schedule entry type so callers share validation.
	ParsedActiveHours []db.ActiveHourEntry
}

// SummaryScheduleStore provides typed CRUD over the summary_schedules
// table.
type SummaryScheduleStore interface {
	// Get returns the schedule for the given (id, alertType), or nil with
	// no error if the row does not exist.
	Get(id, alertType string) (*SummarySchedule, error)

	// Set upserts the schedule for (id, alertType). The activeHoursJSON
	// argument is stored verbatim; callers are expected to have validated
	// the JSON.
	Set(id, alertType, activeHoursJSON string) error

	// Delete removes the schedule for (id, alertType). Missing rows are
	// not an error.
	Delete(id, alertType string) error

	// ListByType returns every schedule with the given alertType (used by
	// state load).
	ListByType(alertType string) ([]SummarySchedule, error)
}
