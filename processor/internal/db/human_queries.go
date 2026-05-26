package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// DeleteHumanAndTracking deletes a human and all their tracking data.
// Must be used instead of a raw DELETE FROM humans since there are no FK CASCADE constraints.
func DeleteHumanAndTracking(dbx *sqlx.DB, id string) error {
	for _, table := range humanOwnedTables {
		if _, err := dbx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE id = ?", table), id); err != nil {
			return fmt.Errorf("delete %s for human %s: %w", table, id, err)
		}
	}
	if _, err := dbx.Exec("DELETE FROM `profiles` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete profiles for human %s: %w", id, err)
	}
	// summary_schedules is per-(human, alert_type), not per-profile,
	// so it's outside humanOwnedTables. Clear here too or rows orphan.
	if _, err := dbx.Exec("DELETE FROM `summary_schedules` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete summary_schedules for human %s: %w", id, err)
	}
	if _, err := dbx.Exec("DELETE FROM `humans` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete human %s: %w", id, err)
	}
	return nil
}

// humanOwnedTables lists all tables holding rows keyed by human id that
// must be cleaned up when a human is deleted (since the DB has no FK
// cascades). Includes per-profile tracking tables AND per-user-owned
// helpers like user_locations.
var humanOwnedTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather",
	"lures", "gym", "nests", "maxbattle", "forts",
	"user_locations",
}
