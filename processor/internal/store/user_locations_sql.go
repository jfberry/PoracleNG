package store

import (
	"database/sql"
	"errors"
	"fmt"

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
			return 0, fmt.Errorf("duplicate label %q for user %q", loc.Label, loc.ID)
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
	// inline sort to avoid pulling in sort just for this helper; few entries.
	for i := 1; i < len(refs); i++ {
		j := i
		for j > 0 && (refs[j-1].Type > refs[j].Type ||
			(refs[j-1].Type == refs[j].Type && refs[j-1].UID > refs[j].UID)) {
			refs[j-1], refs[j] = refs[j], refs[j-1]
			j--
		}
	}
}
