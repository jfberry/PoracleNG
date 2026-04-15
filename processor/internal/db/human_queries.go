package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// DeleteHumanAndTracking deletes a human and all their tracking data.
// Must be used instead of a raw DELETE FROM humans since there are no FK CASCADE constraints.
func DeleteHumanAndTracking(dbx *sqlx.DB, id string) error {
	for _, table := range trackingTables {
		if _, err := dbx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE id = ?", table), id); err != nil {
			return fmt.Errorf("delete %s for human %s: %w", table, id, err)
		}
	}
	if _, err := dbx.Exec("DELETE FROM `profiles` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete profiles for human %s: %w", id, err)
	}
	if _, err := dbx.Exec("DELETE FROM `humans` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete human %s: %w", id, err)
	}
	return nil
}

// trackingTables lists all tracking tables that hold per-profile data.
// Also referenced by store.SQLHumanStore.Delete / CopyProfile via a copy of
// this list. If another tracking table is added, update both.
var trackingTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather", "lures", "gym", "nests", "maxbattle", "forts",
}
