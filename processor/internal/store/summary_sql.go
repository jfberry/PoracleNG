package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// summaryScheduleRow maps the summary_schedules table row to Go types.
type summaryScheduleRow struct {
	ID          string `db:"id"`
	AlertType   string `db:"alert_type"`
	ActiveHours string `db:"active_hours"`
}

// SQLSummaryScheduleStore implements SummaryScheduleStore against
// MySQL/MariaDB via sqlx.
type SQLSummaryScheduleStore struct {
	db *sqlx.DB
}

// NewSQLSummaryScheduleStore constructs a SQL-backed SummaryScheduleStore.
func NewSQLSummaryScheduleStore(db *sqlx.DB) *SQLSummaryScheduleStore {
	return &SQLSummaryScheduleStore{db: db}
}

const summaryScheduleColumns = `id, alert_type, active_hours`

// parseActiveHours decodes the JSON active_hours string. A short or
// empty string is treated as "no entries" without raising an error,
// matching db.LoadProfiles behaviour.
func parseActiveHours(raw string, ctx string) []db.ActiveHourEntry {
	if len(raw) <= 5 {
		return nil
	}
	var entries []db.ActiveHourEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		log.Warnf("summary_schedules %s: failed to parse active_hours: %s", ctx, err)
		return nil
	}
	return entries
}

func rowToSummarySchedule(r *summaryScheduleRow) *SummarySchedule {
	return &SummarySchedule{
		ID:                r.ID,
		AlertType:         r.AlertType,
		ActiveHours:       r.ActiveHours,
		ParsedActiveHours: parseActiveHours(r.ActiveHours, fmt.Sprintf("%s/%s", r.ID, r.AlertType)),
	}
}

// Get returns the schedule for (id, alertType), or nil when no row exists.
func (s *SQLSummaryScheduleStore) Get(id, alertType string) (*SummarySchedule, error) {
	var r summaryScheduleRow
	err := s.db.Get(&r,
		`SELECT `+summaryScheduleColumns+` FROM summary_schedules WHERE id = ? AND alert_type = ?`,
		id, alertType)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select summary_schedule %s/%s: %w", id, alertType, err)
	}
	return rowToSummarySchedule(&r), nil
}

// Set upserts a schedule. The activeHoursJSON value is stored verbatim.
func (s *SQLSummaryScheduleStore) Set(id, alertType, activeHoursJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO summary_schedules (id, alert_type, active_hours)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE active_hours = VALUES(active_hours)`,
		id, alertType, activeHoursJSON)
	if err != nil {
		return fmt.Errorf("upsert summary_schedule %s/%s: %w", id, alertType, err)
	}
	return nil
}

// Delete removes a schedule row. Missing rows are not an error.
func (s *SQLSummaryScheduleStore) Delete(id, alertType string) error {
	_, err := s.db.Exec(
		`DELETE FROM summary_schedules WHERE id = ? AND alert_type = ?`,
		id, alertType)
	if err != nil {
		return fmt.Errorf("delete summary_schedule %s/%s: %w", id, alertType, err)
	}
	return nil
}

// ListByType returns every schedule for the given alertType.
func (s *SQLSummaryScheduleStore) ListByType(alertType string) ([]SummarySchedule, error) {
	var rows []summaryScheduleRow
	err := s.db.Select(&rows,
		`SELECT `+summaryScheduleColumns+` FROM summary_schedules WHERE alert_type = ?`,
		alertType)
	if err != nil {
		return nil, fmt.Errorf("list summary_schedules type=%s: %w", alertType, err)
	}
	out := make([]SummarySchedule, 0, len(rows))
	for i := range rows {
		out = append(out, *rowToSummarySchedule(&rows[i]))
	}
	return out, nil
}
