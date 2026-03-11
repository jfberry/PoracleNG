package db

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/jmoiron/sqlx"
)

// humanRow maps directly to the humans table columns.
type humanRow struct {
	ID               string         `db:"id"`
	Name             string         `db:"name"`
	Type             string         `db:"type"`
	Enabled          bool           `db:"enabled"`
	AdminDisable     bool           `db:"admin_disable"`
	Area             string         `db:"area"`
	AreaRestriction  sql.NullString `db:"area_restriction"`
	Latitude         float64        `db:"latitude"`
	Longitude        float64        `db:"longitude"`
	Language         sql.NullString `db:"language"`
	CurrentProfileNo int            `db:"current_profile_no"`
	BlockedAlerts    sql.NullString `db:"blocked_alerts"`
}

// Human represents a processed human record ready for matching.
type Human struct {
	ID               string
	Name             string
	Type             string
	Enabled          bool
	AdminDisable     bool
	Area             []string // parsed and normalized area names
	AreaRestriction  []string // parsed and normalized, nil if not set
	Latitude         float64
	Longitude        float64
	Language         string
	CurrentProfileNo int
	BlockedAlerts    string
}

// LoadHumans loads all enabled, non-admin-disabled humans from the database.
func LoadHumans(db *sqlx.DB) (map[string]*Human, error) {
	var rows []humanRow
	err := db.Select(&rows,
		`SELECT id, name, type, enabled, admin_disable, area, area_restriction,
		        latitude, longitude, language, current_profile_no, blocked_alerts
		 FROM humans
		 WHERE enabled = 1 AND admin_disable = 0`)
	if err != nil {
		return nil, err
	}

	humans := make(map[string]*Human, len(rows))
	for _, r := range rows {
		h := &Human{
			ID:               r.ID,
			Name:             r.Name,
			Type:             r.Type,
			Enabled:          r.Enabled,
			AdminDisable:     r.AdminDisable,
			Latitude:         r.Latitude,
			Longitude:        r.Longitude,
			Language:         r.Language.String,
			CurrentProfileNo: r.CurrentProfileNo,
			BlockedAlerts:    r.BlockedAlerts.String,
			Area:             parseAndNormalizeAreas(r.Area),
		}
		if r.AreaRestriction.Valid {
			h.AreaRestriction = parseAndNormalizeAreas(r.AreaRestriction.String)
		}
		humans[h.ID] = h
	}
	return humans, nil
}

func parseAndNormalizeAreas(jsonStr string) []string {
	var areas []string
	if err := json.Unmarshal([]byte(jsonStr), &areas); err != nil {
		return nil
	}
	for i, a := range areas {
		areas[i] = strings.ToLower(strings.ReplaceAll(a, "_", " "))
	}
	return areas
}
