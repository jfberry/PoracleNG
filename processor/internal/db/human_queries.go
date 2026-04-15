package db

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// ProfileRow represents a row from the profiles table.
// Retained for CopyProfile's dynamic-column code path; API responses that
// include profiles go through store.Profile / api.ProfileResponse.
type ProfileRow struct {
	UID       int     `db:"uid" json:"uid"`
	ID        string  `db:"id" json:"id"`
	ProfileNo int     `db:"profile_no" json:"profile_no"`
	Name      string  `db:"name" json:"name"`
	Area      string  `db:"area" json:"area"`
	Latitude  float64 `db:"latitude" json:"latitude"`
	Longitude float64 `db:"longitude" json:"longitude"`
	ActiveHours string `db:"active_hours" json:"active_hours"`
}

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
var trackingTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather", "lures", "gym", "nests", "maxbattle", "forts",
}

// UpdateProfileHours updates the active_hours on a profile.
// TODO: move to store.HumanStore as a follow-up.
func UpdateProfileHours(dbx *sqlx.DB, id string, profileNo int, activeHours string) error {
	_, err := dbx.Exec(
		`UPDATE profiles SET active_hours = ? WHERE id = ? AND profile_no = ?`,
		activeHours, id, profileNo)
	if err != nil {
		return fmt.Errorf("update profile hours %s/%d: %w", id, profileNo, err)
	}
	return nil
}

// CopyProfile copies all tracking data from one profile to another.
// It deletes existing tracking in the destination profile first, then copies from source.
// TODO: move to store.HumanStore as a follow-up (cross-table operation).
func CopyProfile(dbx *sqlx.DB, id string, fromProfile, toProfile int) error {
	for _, table := range trackingTables {
		// Delete existing data in destination profile.
		_, err := dbx.Exec(
			fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
			id, toProfile)
		if err != nil {
			return fmt.Errorf("delete %s for copy %s/%d: %w", table, id, toProfile, err)
		}

		// Get column names for this table (excluding uid which is auto-increment).
		rows, err := dbx.Query(
			fmt.Sprintf("SELECT * FROM `%s` WHERE id = ? AND profile_no = ? LIMIT 0", table),
			id, fromProfile)
		if err != nil {
			return fmt.Errorf("get columns for %s: %w", table, err)
		}
		cols, err := rows.Columns()
		rows.Close()
		if err != nil {
			return fmt.Errorf("get column names for %s: %w", table, err)
		}

		// Build column lists excluding 'uid'.
		var selectCols []string
		for _, col := range cols {
			if col == "uid" {
				continue
			}
			if col == "profile_no" {
				selectCols = append(selectCols, fmt.Sprintf("%d AS profile_no", toProfile))
			} else {
				selectCols = append(selectCols, fmt.Sprintf("`%s`", col))
			}
		}

		// Build insert columns excluding 'uid'.
		var insertCols []string
		for _, col := range cols {
			if col == "uid" {
				continue
			}
			insertCols = append(insertCols, fmt.Sprintf("`%s`", col))
		}

		if len(insertCols) == 0 {
			continue
		}

		query := fmt.Sprintf(
			"INSERT INTO `%s` (%s) SELECT %s FROM `%s` WHERE id = ? AND profile_no = ?",
			table,
			strings.Join(insertCols, ", "),
			strings.Join(selectCols, ", "),
			table)

		_, err = dbx.Exec(query, id, fromProfile)
		if err != nil {
			return fmt.Errorf("copy %s from profile %d to %d: %w", table, fromProfile, toProfile, err)
		}
	}
	return nil
}
