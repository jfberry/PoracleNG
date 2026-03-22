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
// Fields may be stored as numbers or strings in the DB JSON, so we use
// a custom UnmarshalJSON that handles both.
type ActiveHourEntry struct {
	Day   int
	Hours int
	Mins  int
}

func (e *ActiveHourEntry) UnmarshalJSON(b []byte) error {
	var raw struct {
		Day   json.Number `json:"day"`
		Hours json.Number `json:"hours"`
		Mins  json.Number `json:"mins"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	day, err := raw.Day.Int64()
	if err != nil {
		return err
	}
	hours, err := raw.Hours.Int64()
	if err != nil {
		return err
	}
	mins, err := raw.Mins.Int64()
	if err != nil {
		return err
	}
	e.Day = int(day)
	e.Hours = int(hours)
	e.Mins = int(mins)
	return nil
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
