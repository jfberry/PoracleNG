package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
)

const mysqlErrDuplicate = 1062

func (s *SQLHumanStore) ListLocations(id string) ([]UserLocation, error) {
	var rows []UserLocation
	err := s.db.Select(&rows,
		`SELECT uid, id, label, latitude, longitude FROM user_locations WHERE id = ? ORDER BY label`, id)
	return rows, err
}

func (s *SQLHumanStore) GetLocation(id, label string) (*UserLocation, error) {
	var row UserLocation
	err := s.db.Get(&row,
		`SELECT uid, id, label, latitude, longitude FROM user_locations WHERE id = ? AND LOWER(label) = LOWER(?) LIMIT 1`,
		id, label)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *SQLHumanStore) AddLocation(loc UserLocation) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO user_locations (id, label, latitude, longitude) VALUES (?, ?, ?, ?)`,
		loc.ID, loc.Label, loc.Latitude, loc.Longitude)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlErrDuplicate {
			return 0, fmt.Errorf("%w: %q for user %q", ErrDuplicateLocation, loc.Label, loc.ID)
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLHumanStore) DeleteLocation(id, label string) error {
	_, err := s.db.Exec(
		`DELETE FROM user_locations WHERE id = ? AND LOWER(label) = LOWER(?)`, id, label)
	return err
}

// trackingTypePathMap maps tracking-table names → URL path segment
// used by the tracking API (and by !tracked output) so referencing-rule
// listings are stable across surfaces.
var trackingTypePathMap = map[string]string{
	"monsters":  "pokemon",
	"raid":      "raid",
	"egg":       "egg",
	"quest":     "quest",
	"invasion":  "invasion",
	"lures":     "lure",
	"nests":     "nest",
	"gym":       "gym",
	"forts":     "fort",
	"maxbattle": "maxbattle",
}

func (s *SQLHumanStore) CountLocationReferences(id, label string) ([]ReferencingRule, error) {
	var out []ReferencingRule
	for table, typePath := range trackingTypePathMap {
		var uids []int64
		q := fmt.Sprintf(
			`SELECT uid FROM %s WHERE id = ? AND LOWER(override_location_label) = LOWER(?)`,
			table)
		if err := s.db.Select(&uids, q, id, label); err != nil {
			return nil, fmt.Errorf("count refs in %s: %w", table, err)
		}
		for _, u := range uids {
			out = append(out, ReferencingRule{Type: typePath, UID: u})
		}
	}
	sortReferences(out)
	return out, nil
}

func sortReferences(refs []ReferencingRule) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Type != refs[j].Type {
			return refs[i].Type < refs[j].Type
		}
		return refs[i].UID < refs[j].UID
	})
}

// trackingTablesWithOverrideAreas lists all tables that carry an override_areas column.
// Mirrors humanOwnedTables in db/human_queries.go minus user_locations (no override_areas).
var trackingTablesWithOverrideAreas = []string{
	"monsters", "raid", "egg", "quest", "invasion",
	"lures", "nests", "gym", "forts", "maxbattle",
}

// PruneOverrideAreas walks every tracking row for the given human and drops
// areas no longer in permitted from the override_areas JSON column. When the
// pruned list is empty the column is set to NULL so the rule falls back to the
// human's own area list.
func (s *SQLHumanStore) PruneOverrideAreas(humanID string, permitted map[string]bool) error {
	type row struct {
		UID           int64  `db:"uid"`
		OverrideAreas string `db:"override_areas"`
	}

	for _, table := range trackingTablesWithOverrideAreas {
		var rows []row
		q := fmt.Sprintf(
			"SELECT uid, COALESCE(override_areas, '') AS override_areas FROM `%s` WHERE id = ? AND override_areas IS NOT NULL AND override_areas != ''",
			table,
		)
		if err := s.db.Select(&rows, q, humanID); err != nil {
			return fmt.Errorf("prune %s: %w", table, err)
		}

		for _, r := range rows {
			areas := unmarshalStringSlice(r.OverrideAreas)
			if areas == nil {
				// Malformed JSON — skip rather than corrupt the row.
				continue
			}

			kept := filterPermittedAreas(areas, permitted)
			if len(kept) == len(areas) {
				continue // nothing changed
			}

			newVal := nullStringSlice(kept)

			updQ := fmt.Sprintf("UPDATE `%s` SET override_areas = ? WHERE uid = ?", table)
			if _, err := s.db.Exec(updQ, newVal, r.UID); err != nil {
				return fmt.Errorf("update %s uid %d: %w", table, r.UID, err)
			}
		}
	}

	return nil
}

// filterPermittedAreas returns a new slice containing only the areas whose
// lowercase form appears in permitted. The original case of each area name is
// preserved. Returns nil (not an empty slice) when every area is filtered out,
// so callers can distinguish "nothing left" from "nothing provided".
func filterPermittedAreas(areas []string, permitted map[string]bool) []string {
	if len(areas) == 0 {
		return nil
	}
	var kept []string
	for _, area := range areas {
		if permitted[strings.ToLower(area)] {
			kept = append(kept, area)
		}
	}
	return kept
}
