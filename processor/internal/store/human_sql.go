package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/guregu/null/v6"
	"github.com/jmoiron/sqlx"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// humanRow maps directly to the humans table columns with JSON stored as strings.
type humanRow struct {
	ID                  string      `db:"id"`
	Type                string      `db:"type"`
	Name                string      `db:"name"`
	Enabled             int         `db:"enabled"`
	Area                string      `db:"area"`
	Latitude            float64     `db:"latitude"`
	Longitude           float64     `db:"longitude"`
	Fails               int         `db:"fails"`
	LastChecked         null.Time   `db:"last_checked"`
	Language            null.String `db:"language"`
	AdminDisable        int         `db:"admin_disable"`
	DisabledDate        null.Time   `db:"disabled_date"`
	CurrentProfileNo    int         `db:"current_profile_no"`
	CommunityMembership string      `db:"community_membership"`
	AreaRestriction     null.String `db:"area_restriction"`
	Notes               string      `db:"notes"`
	BlockedAlerts       null.String `db:"blocked_alerts"`
}

// profileRow maps directly to the profiles table.
type profileRow struct {
	UID         int     `db:"uid"`
	ID          string  `db:"id"`
	ProfileNo   int     `db:"profile_no"`
	Name        string  `db:"name"`
	Area        string  `db:"area"`
	Latitude    float64 `db:"latitude"`
	Longitude   float64 `db:"longitude"`
	ActiveHours string  `db:"active_hours"`
}

// SQLHumanStore implements HumanStore using sqlx against MySQL/MariaDB.
type SQLHumanStore struct {
	db *sqlx.DB
}

// NewSQLHumanStore creates a new SQL-backed HumanStore.
func NewSQLHumanStore(db *sqlx.DB) *SQLHumanStore {
	return &SQLHumanStore{db: db}
}

// DB returns the underlying database connection for callers that need
// direct access during migration.
func (s *SQLHumanStore) DB() *sqlx.DB {
	return s.db
}

func (s *SQLHumanStore) Get(id string) (*Human, error) {
	var r humanRow
	err := s.db.Get(&r, `SELECT `+db.HumanFullColumns+` FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select human %s: %w", id, err)
	}
	return rowToHuman(&r), nil
}

func (s *SQLHumanStore) Create(h *Human) error {
	_, err := s.db.Exec(
		`INSERT INTO humans (id, name, type, enabled, area, latitude, longitude,
		  admin_disable, language, current_profile_no, community_membership,
		  area_restriction, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.Name, h.Type, boolToInt(h.Enabled),
		marshalStringSlice(h.Area), h.Latitude, h.Longitude,
		boolToInt(h.AdminDisable), nullIfEmpty(h.Language),
		h.CurrentProfileNo, marshalStringSlice(h.CommunityMembership),
		nullStringSlice(h.AreaRestriction), h.Notes)
	if err != nil {
		return fmt.Errorf("insert human %s: %w", h.ID, err)
	}
	return nil
}

func (s *SQLHumanStore) Delete(id string) error {
	for _, table := range trackingTables {
		if _, err := s.db.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE id = ?", table), id); err != nil {
			return fmt.Errorf("delete %s for human %s: %w", table, id, err)
		}
	}
	if _, err := s.db.Exec("DELETE FROM `profiles` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete profiles for human %s: %w", id, err)
	}
	if _, err := s.db.Exec("DELETE FROM `humans` WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete human %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetEnabled(id string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE humans SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("update human enabled %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetEnabledWithFails(id string) error {
	_, err := s.db.Exec(`UPDATE humans SET enabled = 1, fails = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("update human enabled+fails %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetAdminDisable(id string, disable bool) error {
	if disable {
		_, err := s.db.Exec(`UPDATE humans SET admin_disable = 1, disabled_date = NOW() WHERE id = ?`, id)
		// Note: does NOT set enabled=0 — admin_disable is a separate flag
		if err != nil {
			return fmt.Errorf("admin disable human %s: %w", id, err)
		}
	} else {
		_, err := s.db.Exec(`UPDATE humans SET admin_disable = 0, disabled_date = NULL, enabled = 1, fails = 0 WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("admin enable human %s: %w", id, err)
		}
	}
	return nil
}

func (s *SQLHumanStore) SetLocation(id string, profileNo int, lat, lon float64) error {
	if _, err := s.db.Exec(
		`UPDATE humans SET latitude = ?, longitude = ? WHERE id = ?`,
		lat, lon, id); err != nil {
		return fmt.Errorf("update human location %s: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE profiles SET latitude = ?, longitude = ? WHERE id = ? AND profile_no = ?`,
		lat, lon, id, profileNo); err != nil {
		return fmt.Errorf("update profile location %s/%d: %w", id, profileNo, err)
	}
	return nil
}

func (s *SQLHumanStore) SetArea(id string, profileNo int, areas []string) error {
	areaJSON := marshalStringSlice(areas)
	if _, err := s.db.Exec(
		`UPDATE humans SET area = ? WHERE id = ?`, areaJSON, id); err != nil {
		return fmt.Errorf("update human areas %s: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE profiles SET area = ? WHERE id = ? AND profile_no = ?`,
		areaJSON, id, profileNo); err != nil {
		return fmt.Errorf("update profile areas %s/%d: %w", id, profileNo, err)
	}
	return nil
}

func (s *SQLHumanStore) SetLanguage(id string, lang string) error {
	_, err := s.db.Exec(`UPDATE humans SET language = ? WHERE id = ?`, nullIfEmpty(lang), id)
	if err != nil {
		return fmt.Errorf("update human language %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetCommunity(id string, communities []string, restrictions []string) error {
	_, err := s.db.Exec(
		`UPDATE humans SET community_membership = ?, area_restriction = ? WHERE id = ?`,
		marshalStringSlice(communities), nullStringSlice(restrictions), id)
	if err != nil {
		return fmt.Errorf("update human community %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetBlockedAlerts(id string, alerts []string) error {
	_, err := s.db.Exec(
		`UPDATE humans SET blocked_alerts = ? WHERE id = ?`,
		nullStringSlice(alerts), id)
	if err != nil {
		return fmt.Errorf("update human blocked_alerts %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) SetName(id string, name string) error {
	_, err := s.db.Exec(`UPDATE humans SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("update human name %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) Update(id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	// Sort keys for deterministic SQL (aids debugging and query plan caching).
	keys := make([]string, 0, len(fields))
	for col := range fields {
		keys = append(keys, col)
	}
	sort.Strings(keys)

	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for _, col := range keys {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, fields[col])
	}
	args = append(args, id)
	query := "UPDATE humans SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("update human %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) ListByType(typ string) ([]*Human, error) {
	var rows []humanRow
	err := s.db.Select(&rows, `SELECT `+db.HumanFullColumns+` FROM humans WHERE type = ?`, typ)
	if err != nil {
		return nil, fmt.Errorf("list humans by type %s: %w", typ, err)
	}
	return rowsToHumans(rows), nil
}

func (s *SQLHumanStore) ListByTypeEnabled(typ string) ([]*Human, error) {
	var rows []humanRow
	err := s.db.Select(&rows, `SELECT `+db.HumanFullColumns+` FROM humans WHERE type = ? AND admin_disable = 0`, typ)
	if err != nil {
		return nil, fmt.Errorf("list humans by type enabled %s: %w", typ, err)
	}
	return rowsToHumans(rows), nil
}

func (s *SQLHumanStore) ListByTypes(types []string) ([]*Human, error) {
	if len(types) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(`SELECT `+db.HumanFullColumns+` FROM humans WHERE type IN (?) AND admin_disable = 0`, types)
	if err != nil {
		return nil, fmt.Errorf("build IN query: %w", err)
	}
	query = s.db.Rebind(query)
	var rows []humanRow
	if err := s.db.Select(&rows, query, args...); err != nil {
		return nil, fmt.Errorf("list humans by types: %w", err)
	}
	return rowsToHumans(rows), nil
}

func (s *SQLHumanStore) ListAll() ([]*Human, error) {
	var rows []humanRow
	err := s.db.Select(&rows, `SELECT `+db.HumanFullColumns+` FROM humans ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("list all humans: %w", err)
	}
	return rowsToHumans(rows), nil
}

func (s *SQLHumanStore) LookupWebhookByName(name string) (string, error) {
	var id string
	err := s.db.Get(&id, `SELECT id FROM humans WHERE name = ? AND type = 'webhook' LIMIT 1`, name)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("lookup webhook by name %s: %w", name, err)
	}
	return id, nil
}

func (s *SQLHumanStore) CountByName(name string) (int, error) {
	var count int
	err := s.db.Get(&count, `SELECT COUNT(*) FROM humans WHERE name = ?`, name)
	if err != nil {
		return 0, fmt.Errorf("count humans by name %s: %w", name, err)
	}
	return count, nil
}

// --- Profile operations ---

func (s *SQLHumanStore) GetProfiles(id string) ([]Profile, error) {
	var rows []profileRow
	err := s.db.Select(&rows, `SELECT * FROM profiles WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("select profiles for %s: %w", id, err)
	}
	profiles := make([]Profile, len(rows))
	for i, r := range rows {
		profiles[i] = profileRowToProfile(&r)
	}
	return profiles, nil
}

func (s *SQLHumanStore) SwitchProfile(id string, profileNo int) (bool, error) {
	var profile profileRow
	err := s.db.Get(&profile, `SELECT * FROM profiles WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("select profile %s/%d: %w", id, profileNo, err)
	}
	_, err = s.db.Exec(
		`UPDATE humans SET current_profile_no = ?, area = ?, latitude = ?, longitude = ? WHERE id = ?`,
		profileNo, profile.Area, profile.Latitude, profile.Longitude, id)
	if err != nil {
		return false, fmt.Errorf("update human for switch profile %s: %w", id, err)
	}
	return true, nil
}

func (s *SQLHumanStore) AddProfile(id string, name string, activeHours string) error {
	var profiles []profileRow
	if err := s.db.Select(&profiles, `SELECT * FROM profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select profiles for %s: %w", id, err)
	}

	var human humanRow
	if err := s.db.Get(&human, `SELECT `+db.HumanFullColumns+` FROM humans WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select human %s for add profile: %w", id, err)
	}

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

	_, err := s.db.Exec(
		`INSERT INTO profiles (id, profile_no, name, area, latitude, longitude, active_hours)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, newProfileNo, name, human.Area, human.Latitude, human.Longitude, activeHours)
	if err != nil {
		return fmt.Errorf("insert profile %s/%d: %w", id, newProfileNo, err)
	}
	return nil
}

func (s *SQLHumanStore) DeleteProfile(id string, profileNo int) error {
	_, err := s.db.Exec(`DELETE FROM profiles WHERE id = ? AND profile_no = ?`, id, profileNo)
	if err != nil {
		return fmt.Errorf("delete profile %s/%d: %w", id, profileNo, err)
	}

	var remaining []profileRow
	if err := s.db.Select(&remaining, `SELECT * FROM profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("select remaining profiles for %s: %w", id, err)
	}

	originalCount := len(remaining) + 1
	if originalCount != 1 || profileNo != 1 {
		for _, table := range trackingTables {
			if _, err := s.db.Exec(
				fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
				id, profileNo); err != nil {
				return fmt.Errorf("delete tracking from %s for %s/%d: %w", table, id, profileNo, err)
			}
		}
	}

	var human humanRow
	err = s.db.Get(&human, `SELECT `+db.HumanFullColumns+` FROM humans WHERE id = ?`, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("select human %s after profile delete: %w", id, err)
	}

	if human.CurrentProfileNo == profileNo {
		if len(remaining) == 0 {
			_, err = s.db.Exec(`UPDATE humans SET current_profile_no = 1 WHERE id = ?`, id)
		} else {
			lowest := remaining[0]
			for _, p := range remaining[1:] {
				if p.ProfileNo < lowest.ProfileNo {
					lowest = p
				}
			}
			_, err = s.db.Exec(
				`UPDATE humans SET current_profile_no = ?, area = ?, latitude = ?, longitude = ? WHERE id = ?`,
				lowest.ProfileNo, lowest.Area, lowest.Latitude, lowest.Longitude, id)
		}
		if err != nil {
			return fmt.Errorf("update human after profile delete %s: %w", id, err)
		}
	}
	return nil
}

func (s *SQLHumanStore) CopyProfile(id string, fromProfile, toProfile int) error {
	for _, table := range trackingTables {
		if _, err := s.db.Exec(
			fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
			id, toProfile); err != nil {
			return fmt.Errorf("delete %s for copy %s/%d: %w", table, id, toProfile, err)
		}

		rows, err := s.db.Query(
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

		var selectCols, insertCols []string
		for _, col := range cols {
			if col == "uid" {
				continue
			}
			insertCols = append(insertCols, fmt.Sprintf("`%s`", col))
			if col == "profile_no" {
				selectCols = append(selectCols, fmt.Sprintf("%d AS profile_no", toProfile))
			} else {
				selectCols = append(selectCols, fmt.Sprintf("`%s`", col))
			}
		}
		if len(insertCols) == 0 {
			continue
		}

		query := fmt.Sprintf(
			"INSERT INTO `%s` (%s) SELECT %s FROM `%s` WHERE id = ? AND profile_no = ?",
			table, strings.Join(insertCols, ", "), strings.Join(selectCols, ", "), table)
		if _, err = s.db.Exec(query, id, fromProfile); err != nil {
			return fmt.Errorf("copy %s from profile %d to %d: %w", table, fromProfile, toProfile, err)
		}
	}
	return nil
}

func (s *SQLHumanStore) CreateDefaultProfile(id, name string, areas []string, lat, lon float64) error {
	_, err := s.db.Exec(
		`INSERT INTO profiles (id, profile_no, name, area, latitude, longitude)
		 VALUES (?, 1, ?, ?, ?, ?)`,
		id, name, marshalStringSlice(areas), lat, lon)
	if err != nil {
		return fmt.Errorf("insert default profile for %s: %w", id, err)
	}
	return nil
}

func (s *SQLHumanStore) UpdateProfileHours(id string, profileNo int, activeHours string) error {
	_, err := s.db.Exec(
		`UPDATE profiles SET active_hours = ? WHERE id = ? AND profile_no = ?`,
		activeHours, id, profileNo)
	if err != nil {
		return fmt.Errorf("update profile hours %s/%d: %w", id, profileNo, err)
	}
	return nil
}

// --- Tracking tables list ---

var trackingTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather", "lures", "gym", "nests", "maxbattle", "forts",
}

// --- Conversion helpers ---

func rowToHuman(r *humanRow) *Human {
	h := &Human{
		ID:               r.ID,
		Type:             r.Type,
		Name:             r.Name,
		Enabled:          r.Enabled != 0,
		Latitude:         r.Latitude,
		Longitude:        r.Longitude,
		Fails:            r.Fails,
		LastChecked:      r.LastChecked,
		Language:         r.Language.ValueOrZero(),
		AdminDisable:     r.AdminDisable != 0,
		DisabledDate:     r.DisabledDate,
		CurrentProfileNo: r.CurrentProfileNo,
		Notes:            r.Notes,
	}
	h.Area = unmarshalStringSlice(r.Area)
	h.CommunityMembership = unmarshalStringSlice(r.CommunityMembership)
	if r.AreaRestriction.Valid {
		h.AreaRestriction = unmarshalStringSlice(r.AreaRestriction.ValueOrZero())
	}
	if r.BlockedAlerts.Valid {
		h.BlockedAlerts = unmarshalStringSlice(r.BlockedAlerts.ValueOrZero())
	}
	return h
}

func rowsToHumans(rows []humanRow) []*Human {
	humans := make([]*Human, len(rows))
	for i := range rows {
		humans[i] = rowToHuman(&rows[i])
	}
	return humans
}

func profileRowToProfile(r *profileRow) Profile {
	return Profile{
		UID:         r.UID,
		ID:          r.ID,
		ProfileNo:   r.ProfileNo,
		Name:        r.Name,
		Area:        unmarshalStringSlice(r.Area),
		Latitude:    r.Latitude,
		Longitude:   r.Longitude,
		ActiveHours: r.ActiveHours,
	}
}

func marshalStringSlice(s []string) string {
	if s == nil {
		return "[]"
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func nullStringSlice(s []string) any {
	if s == nil {
		return nil
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func unmarshalStringSlice(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	return result
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
