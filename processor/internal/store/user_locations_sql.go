package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/pokemon/poracleng/processor/internal/geofence"
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

func (s *SQLHumanStore) UpdateLocation(id, label string, lat, lon float64) error {
	res, err := s.db.Exec(
		`UPDATE user_locations SET latitude = ?, longitude = ? WHERE id = ? AND LOWER(label) = LOWER(?)`,
		lat, lon, id, label)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// MySQL reports rows *changed*, not matched (the DSN doesn't set
		// CLIENT_FOUND_ROWS): re-submitting the current coordinates affects
		// zero rows even though the row exists. Distinguish the no-op update
		// from a genuinely missing label before reporting not-found.
		var exists int
		if err := s.db.Get(&exists,
			`SELECT COUNT(*) FROM user_locations WHERE id = ? AND LOWER(label) = LOWER(?)`, id, label); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %q for user %q", ErrLocationNotFound, label, id)
		}
	}
	return nil
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
	"tappable":  "tappable",
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
	"lures", "nests", "gym", "forts", "maxbattle", "tappable",
}

// StripOverrideAreas walks every tracking row for the given human and drops
// areas present in denied from the override_areas JSON column. Names absent
// from denied are kept. When the resulting list is empty the column is set to
// NULL so the rule falls back to the human's own area list.
func (s *SQLHumanStore) StripOverrideAreas(humanID string, denied map[string]bool) error {
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

			kept := dropDeniedAreas(areas, denied)
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

// dropDeniedAreas returns a new slice containing only the areas whose
// lowercase form does NOT appear in denied. The original case of each area name
// is preserved. Returns nil (not an empty slice) when every area is dropped,
// so callers can distinguish "nothing left" from "nothing provided".
func dropDeniedAreas(areas []string, denied map[string]bool) []string {
	if len(areas) == 0 {
		return nil
	}
	var kept []string
	for _, area := range areas {
		if !denied[strings.ToLower(area)] {
			kept = append(kept, area)
		}
	}
	return kept
}

// areaNameMatches compares a stored area name with a requested one by
// geofence.NormalizeAreaKey, so case and underscore/space spellings are
// interchangeable (humans.area keeps fence spelling, legacy override_areas
// may hold spaced forms).
func areaNameMatches(stored, requested string) bool {
	return geofence.NormalizeAreaKey(stored) == geofence.NormalizeAreaKey(requested)
}

// removeAreaName returns areas without entries matching name; kept entries are
// returned exactly as stored. The result is never nil.
func removeAreaName(areas []string, name string) (kept []string, changed bool) {
	kept = make([]string, 0, len(areas))
	for _, a := range areas {
		if !areaNameMatches(a, name) {
			kept = append(kept, a)
		}
	}
	return kept, len(kept) != len(areas)
}

// editAreaList applies an EditOverrideAreas edit to one list. Empty to removes
// from (other entries untouched); otherwise entries matching from OR to become
// to (so a legacy spelling of to is normalised to the written form) and
// duplicates by key collapse, keeping the first.
func editAreaList(areas []string, from, to string) (out []string, changed bool) {
	if to == "" {
		return removeAreaName(areas, from)
	}
	out = make([]string, 0, len(areas))
	seen := map[string]bool{}
	for _, a := range areas {
		if areaNameMatches(a, from) || areaNameMatches(a, to) {
			a = to
		}
		k := geofence.NormalizeAreaKey(a)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	return out, !slices.Equal(out, areas)
}

// withProfileNo adds n to the ascending profile list nos unless already present.
// The area ops use it to report the current profile when only humans.area
// changed (a legacy human whose current profile has no profiles row).
func withProfileNo(nos []int, n int) []int {
	i, found := slices.BinarySearch(nos, n)
	if found {
		return nos
	}
	return slices.Insert(nos, i, n)
}

// RemoveAreaFromProfiles implements HumanStore.
func (s *SQLHumanStore) RemoveAreaFromProfiles(id, name string, profileNos []int, dryRun bool) (removedFrom []int, changed bool, err error) {
	if len(profileNos) == 0 {
		return nil, false, nil
	}
	q, args, err := sqlx.In(`SELECT profile_no, area FROM profiles WHERE id = ? AND profile_no IN (?) ORDER BY profile_no`, id, profileNos)
	if err != nil {
		return nil, false, fmt.Errorf("remove area %s: build query: %w", id, err)
	}
	var rows []struct {
		ProfileNo int            `db:"profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = s.db.Select(&rows, s.db.Rebind(q), args...); err != nil {
		return nil, false, fmt.Errorf("remove area %s: select profiles: %w", id, err)
	}
	for _, r := range rows {
		kept, rowChanged := removeAreaName(unmarshalStringSlice(r.Area.String), name)
		if !rowChanged {
			continue
		}
		if !dryRun {
			if _, err = s.db.Exec(`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
				marshalStringSlice(kept), id, r.ProfileNo); err != nil {
				return nil, false, fmt.Errorf("remove area %s/%d: %w", id, r.ProfileNo, err)
			}
		}
		removedFrom = append(removedFrom, r.ProfileNo)
		changed = true
	}

	var cur struct {
		ProfileNo int            `db:"current_profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = s.db.Get(&cur, `SELECT current_profile_no, area FROM humans WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return removedFrom, changed, nil
		}
		return nil, false, fmt.Errorf("remove area %s: select human: %w", id, err)
	}
	if slices.Contains(profileNos, cur.ProfileNo) {
		if kept, hc := removeAreaName(unmarshalStringSlice(cur.Area.String), name); hc {
			changed = true
			removedFrom = withProfileNo(removedFrom, cur.ProfileNo)
			if !dryRun {
				if _, err = s.db.Exec(`UPDATE humans SET area = ? WHERE id = ?`, marshalStringSlice(kept), id); err != nil {
					return nil, false, fmt.Errorf("remove area %s: update human: %w", id, err)
				}
			}
		}
	}
	return removedFrom, changed, nil
}

// RenameAreaInProfiles implements HumanStore.
func (s *SQLHumanStore) RenameAreaInProfiles(id, from, to string, profileNos []int, dryRun bool) (renamedIn []int, changed bool, err error) {
	if len(profileNos) == 0 {
		return nil, false, nil
	}
	q, args, err := sqlx.In(`SELECT profile_no, area FROM profiles WHERE id = ? AND profile_no IN (?) ORDER BY profile_no`, id, profileNos)
	if err != nil {
		return nil, false, fmt.Errorf("rename area %s: build query: %w", id, err)
	}
	var rows []struct {
		ProfileNo int            `db:"profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = s.db.Select(&rows, s.db.Rebind(q), args...); err != nil {
		return nil, false, fmt.Errorf("rename area %s: select profiles: %w", id, err)
	}
	for _, r := range rows {
		out, rowChanged := editAreaList(unmarshalStringSlice(r.Area.String), from, to)
		if !rowChanged {
			continue
		}
		if !dryRun {
			if _, err = s.db.Exec(`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
				marshalStringSlice(out), id, r.ProfileNo); err != nil {
				return nil, false, fmt.Errorf("rename area %s/%d: %w", id, r.ProfileNo, err)
			}
		}
		renamedIn = append(renamedIn, r.ProfileNo)
		changed = true
	}

	var cur struct {
		ProfileNo int            `db:"current_profile_no"`
		Area      sql.NullString `db:"area"`
	}
	if err = s.db.Get(&cur, `SELECT current_profile_no, area FROM humans WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return renamedIn, changed, nil
		}
		return nil, false, fmt.Errorf("rename area %s: select human: %w", id, err)
	}
	if slices.Contains(profileNos, cur.ProfileNo) {
		if out, hc := editAreaList(unmarshalStringSlice(cur.Area.String), from, to); hc {
			changed = true
			renamedIn = withProfileNo(renamedIn, cur.ProfileNo)
			if !dryRun {
				if _, err = s.db.Exec(`UPDATE humans SET area = ? WHERE id = ?`, marshalStringSlice(out), id); err != nil {
					return nil, false, fmt.Errorf("rename area %s: update human: %w", id, err)
				}
			}
		}
	}
	return renamedIn, changed, nil
}

// EditOverrideAreas implements HumanStore.
func (s *SQLHumanStore) EditOverrideAreas(id string, profileNos []int, from, to string, dryRun bool) (map[string][]int64, bool, error) {
	broadened := map[string][]int64{}
	anyChanged := false
	if len(profileNos) == 0 {
		return broadened, false, nil
	}
	type row struct {
		UID           int64  `db:"uid"`
		OverrideAreas string `db:"override_areas"`
	}
	for _, table := range trackingTablesWithOverrideAreas {
		q, args, err := sqlx.In(fmt.Sprintf(
			"SELECT uid, COALESCE(override_areas, '') AS override_areas FROM `%s` WHERE id = ? AND profile_no IN (?) AND override_areas IS NOT NULL AND override_areas != ''",
			table), id, profileNos)
		if err != nil {
			return nil, false, fmt.Errorf("edit override areas %s: build query: %w", table, err)
		}
		var rows []row
		if err := s.db.Select(&rows, s.db.Rebind(q), args...); err != nil {
			return nil, false, fmt.Errorf("edit override areas %s: %w", table, err)
		}
		for _, r := range rows {
			areas := unmarshalStringSlice(r.OverrideAreas)
			if areas == nil {
				continue // malformed JSON: skip rather than corrupt the row
			}
			out, changed := editAreaList(areas, from, to)
			if !changed {
				continue
			}
			anyChanged = true
			var newVal any
			if len(out) > 0 {
				newVal = nullStringSlice(out)
			} else {
				broadened[trackingTypePathMap[table]] = append(broadened[trackingTypePathMap[table]], r.UID)
			}
			if dryRun {
				continue
			}
			if _, err := s.db.Exec(fmt.Sprintf("UPDATE `%s` SET override_areas = ? WHERE uid = ?", table), newVal, r.UID); err != nil {
				return nil, false, fmt.Errorf("edit override areas %s uid %d: %w", table, r.UID, err)
			}
		}
	}
	return broadened, anyChanged, nil
}
