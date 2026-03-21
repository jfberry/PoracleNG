package db

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"
	"github.com/jmoiron/sqlx"
)

// ProfileKey uniquely identifies a profile.
type ProfileKey struct {
	ID        string
	ProfileNo int
}

// ActiveHourEntry represents a time-of-day rule for auto-switching profiles.
type ActiveHourEntry struct {
	Day   int `json:"day"`   // ISO weekday: 1=Monday, 7=Sunday
	Hours int `json:"hours"`
	Mins  int `json:"mins"`
}

// Profile represents a row from the profiles table.
type Profile struct {
	ID          string  `db:"id"`
	ProfileNo   int    `db:"profile_no"`
	Name        string  `db:"name"`
	Area        string  `db:"area"`
	Latitude    float64 `db:"latitude"`
	Longitude   float64 `db:"longitude"`
	ActiveHours string  `db:"active_hours"`

	// ParsedActiveHours is computed after load, not a DB column.
	ParsedActiveHours []ActiveHourEntry `db:"-"`
}

// LoadProfiles loads all profiles from the database.
func LoadProfiles(db *sqlx.DB) (map[ProfileKey]*Profile, error) {
	var rows []Profile
	err := db.Select(&rows,
		`SELECT id, profile_no, name,
		        COALESCE(area, '[]') AS area,
		        COALESCE(latitude, 0) AS latitude,
		        COALESCE(longitude, 0) AS longitude,
		        COALESCE(active_hours, '') AS active_hours
		 FROM profiles`)
	if err != nil {
		return nil, err
	}

	profiles := make(map[ProfileKey]*Profile, len(rows))
	for i := range rows {
		p := &rows[i]
		if len(p.ActiveHours) > 5 {
			var entries []ActiveHourEntry
			if err := json.Unmarshal([]byte(p.ActiveHours), &entries); err != nil {
				log.Warnf("Profile %s/%d: failed to parse active_hours: %s", p.ID, p.ProfileNo, err)
			} else {
				p.ParsedActiveHours = entries
			}
		}
		profiles[ProfileKey{ID: p.ID, ProfileNo: p.ProfileNo}] = p
	}
	return profiles, nil
}
