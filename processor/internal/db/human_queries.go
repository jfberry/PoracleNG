package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/guregu/null/v6"
	"github.com/jmoiron/sqlx"
)

// HumanFull represents a complete human record with all columns.
type HumanFull struct {
	ID                  string         `db:"id" json:"id"`
	Type                string         `db:"type" json:"type"`
	Name                string         `db:"name" json:"name"`
	Enabled             int            `db:"enabled" json:"enabled"`
	Area                string         `db:"area" json:"area"`
	Latitude            float64        `db:"latitude" json:"latitude"`
	Longitude           float64        `db:"longitude" json:"longitude"`
	Fails               int            `db:"fails" json:"fails"`
	LastChecked         null.Time   `db:"last_checked" json:"last_checked"`
	Language            null.String `db:"language" json:"language"`
	AdminDisable        int            `db:"admin_disable" json:"admin_disable"`
	DisabledDate        null.Time   `db:"disabled_date" json:"disabled_date"`
	CurrentProfileNo    int            `db:"current_profile_no" json:"current_profile_no"`
	CommunityMembership string         `db:"community_membership" json:"community_membership"`
	AreaRestriction     null.String `db:"area_restriction" json:"area_restriction"`
	Notes               string         `db:"notes" json:"notes"`
	BlockedAlerts       null.String `db:"blocked_alerts" json:"blocked_alerts"`
}

// ProfileRow represents a row from the profiles table.
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

// SelectOneHumanFull returns all columns for a single human by ID.
func SelectOneHumanFull(db *sqlx.DB, id string) (*HumanFull, error) {
	var h HumanFull
	err := db.Get(&h, `SELECT * FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select human full %s: %w", id, err)
	}
	return &h, nil
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

// UpdateHumanEnabled sets the enabled flag on a human.
func UpdateHumanEnabled(db *sqlx.DB, id string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := db.Exec(`UPDATE humans SET enabled = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("update human enabled %s: %w", id, err)
	}
	return nil
}

// UpdateHumanAdminDisable sets the admin_disable flag on a human.
func UpdateHumanAdminDisable(db *sqlx.DB, id string, disable bool) error {
	val := 0
	if disable {
		val = 1
	}
	_, err := db.Exec(`UPDATE humans SET admin_disable = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("update human admin_disable %s: %w", id, err)
	}
	return nil
}

// SwitchProfile reads the profile row and updates the human's current_profile_no, area,
// latitude, and longitude to match. Returns false if the profile was not found.
func SwitchProfile(db *sqlx.DB, id string, profileNo int) (bool, error) {
	var profile ProfileRow
	err := db.Get(&profile, `SELECT * FROM profiles WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("select profile %s/%d: %w", id, profileNo, err)
	}

	_, err = db.Exec(
		`UPDATE humans SET current_profile_no = ?, area = ?, latitude = ?, longitude = ? WHERE id = ?`,
		profileNo, profile.Area, profile.Latitude, profile.Longitude, id)
	if err != nil {
		return false, fmt.Errorf("update human for switch profile %s: %w", id, err)
	}
	return true, nil
}

// SelectProfiles returns all profiles for a given human ID.
func SelectProfiles(db *sqlx.DB, id string) ([]ProfileRow, error) {
	var profiles []ProfileRow
	err := db.Select(&profiles, `SELECT * FROM profiles WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select profiles for %s: %w", id, err)
	}
	return profiles, nil
}

// trackingTables lists all tracking tables that hold per-profile data.
var trackingTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather", "lures", "gym", "nests", "maxbattle", "forts",
}

// DeleteProfile deletes a profile and all its associated tracking data.
// If the deleted profile was the current one, it switches to the lowest remaining profile.
func DeleteProfile(dbx *sqlx.DB, id string, profileNo int) error {
	// Delete the profile row itself.
	_, err := dbx.Exec(`DELETE FROM profiles WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return fmt.Errorf("delete profile %s/%d: %w", id, profileNo, err)
	}

	// Load remaining profiles to check if we need to handle special cases.
	var remaining []ProfileRow
	if err := dbx.Select(&remaining, `SELECT * FROM profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select remaining profiles for %s: %w", id, err)
	}

	// Delete tracking data for this profile (unless it's the only profile=1 scenario
	// where JS skips deletion — but we match JS: "if profiles.length !== 1 || profileNo !== 1")
	// The JS condition means: skip deletion only when there was exactly 1 profile AND profileNo=1.
	// Since we already deleted the profile row, remaining.length is profiles.length-1.
	// The original check was before deletion, so we replicate with (remaining+1 profiles originally).
	originalCount := len(remaining) + 1
	if originalCount != 1 || profileNo != 1 {
		for _, table := range trackingTables {
			_, err := dbx.Exec(
				fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
				id, profileNo)
			if err != nil {
				return fmt.Errorf("delete tracking from %s for %s/%d: %w", table, id, profileNo, err)
			}
		}
	}

	// If the human's current profile was the deleted one, switch to the lowest remaining.
	var human HumanFull
	err = dbx.Get(&human, `SELECT * FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // human doesn't exist, nothing to update
		}
		return fmt.Errorf("select human %s after profile delete: %w", id, err)
	}

	if human.CurrentProfileNo == profileNo {
		if len(remaining) == 0 {
			_, err = dbx.Exec(`UPDATE humans SET current_profile_no = 1 WHERE id = ?`, id)
		} else {
			// Find lowest profile_no
			lowest := remaining[0]
			for _, p := range remaining[1:] {
				if p.ProfileNo < lowest.ProfileNo {
					lowest = p
				}
			}
			_, err = dbx.Exec(
				`UPDATE humans SET current_profile_no = ?, area = ?, latitude = ?, longitude = ? WHERE id = ?`,
				lowest.ProfileNo, lowest.Area, lowest.Latitude, lowest.Longitude, id)
		}
		if err != nil {
			return fmt.Errorf("update human after profile delete %s: %w", id, err)
		}
	}

	return nil
}

// AddProfile creates a new profile for a human, auto-assigning the next available profile_no.
// The new profile inherits the human's current area, latitude, and longitude.
func AddProfile(dbx *sqlx.DB, id string, name string, activeHours string) error {
	// Load existing profiles to find next available number.
	var profiles []ProfileRow
	if err := dbx.Select(&profiles, `SELECT * FROM profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select profiles for %s: %w", id, err)
	}

	// Load human for area/lat/lon defaults.
	var human HumanFull
	if err := dbx.Get(&human, `SELECT * FROM humans WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select human %s for add profile: %w", id, err)
	}

	// Find next available profile_no (same algorithm as JS).
	newProfileNo := 1
	for {
		found := false
		for _, p := range profiles {
			if p.ProfileNo == newProfileNo {
				newProfileNo++
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	if activeHours == "" {
		activeHours = "{}"
	}

	_, err := dbx.Exec(
		`INSERT INTO profiles (id, profile_no, name, area, latitude, longitude, active_hours)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, newProfileNo, name, human.Area, human.Latitude, human.Longitude, activeHours)
	if err != nil {
		return fmt.Errorf("insert profile %s/%d: %w", id, newProfileNo, err)
	}

	return nil
}

// UpdateProfileHours updates the active_hours on a profile.
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

// UpdateHumanLocation updates the latitude and longitude on both the humans
// table and the matching profile row.
func UpdateHumanLocation(dbx *sqlx.DB, id string, lat, lon float64, profileNo int) error {
	if _, err := dbx.Exec(
		`UPDATE humans SET latitude = ?, longitude = ? WHERE id = ?`,
		lat, lon, id); err != nil {
		return fmt.Errorf("update human location %s: %w", id, err)
	}
	if _, err := dbx.Exec(
		`UPDATE profiles SET latitude = ?, longitude = ? WHERE id = ? AND profile_no = ?`,
		lat, lon, id, profileNo); err != nil {
		return fmt.Errorf("update profile location %s/%d: %w", id, profileNo, err)
	}
	return nil
}

// UpdateHumanAreas updates the area JSON on both the humans table and the
// matching profile row.
func UpdateHumanAreas(dbx *sqlx.DB, id string, areaJSON string, profileNo int) error {
	if _, err := dbx.Exec(
		`UPDATE humans SET area = ? WHERE id = ?`,
		areaJSON, id); err != nil {
		return fmt.Errorf("update human areas %s: %w", id, err)
	}
	if _, err := dbx.Exec(
		`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
		areaJSON, id, profileNo); err != nil {
		return fmt.Errorf("update profile areas %s/%d: %w", id, profileNo, err)
	}
	return nil
}

// CreateHuman inserts a new human record into the humans table.
func CreateHuman(dbx *sqlx.DB, h *HumanFull) error {
	_, err := dbx.Exec(
		`INSERT INTO humans (id, name, type, enabled, area, latitude, longitude, admin_disable, language, current_profile_no, community_membership, area_restriction, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.Name, h.Type, h.Enabled, h.Area, h.Latitude, h.Longitude,
		h.AdminDisable, h.Language, h.CurrentProfileNo,
		h.CommunityMembership, h.AreaRestriction, h.Notes)
	if err != nil {
		return fmt.Errorf("insert human %s: %w", h.ID, err)
	}
	return nil
}

// CreateDefaultProfile inserts profile_no=1 for a new human.
func CreateDefaultProfile(dbx *sqlx.DB, id, name, area string, lat, lon float64) error {
	_, err := dbx.Exec(
		`INSERT INTO profiles (id, profile_no, name, area, latitude, longitude)
		 VALUES (?, 1, ?, ?, ?, ?)`,
		id, name, area, lat, lon)
	if err != nil {
		return fmt.Errorf("insert default profile for %s: %w", id, err)
	}
	return nil
}


