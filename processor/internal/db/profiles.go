package db

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"
)

// ProfileKey uniquely identifies a profile.
type ProfileKey struct {
	ID        string
	ProfileNo int
}

// ActiveHourEntry represents a time-of-day rule for auto-switching profiles.
// Fields may be stored as numbers or strings in the DB JSON (including
// zero-padded strings like "00"), so we decode into interface{} and coerce.
type ActiveHourEntry struct {
	Day   int
	Hours int
	Mins  int
}

func (e *ActiveHourEntry) UnmarshalJSON(b []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var err error
	if e.Day, err = flexToInt(raw["day"]); err != nil {
		return fmt.Errorf("day: %w", err)
	}
	if e.Hours, err = flexToInt(raw["hours"]); err != nil {
		return fmt.Errorf("hours: %w", err)
	}
	if e.Mins, err = flexToInt(raw["mins"]); err != nil {
		return fmt.Errorf("mins: %w", err)
	}
	return nil
}

// flexToInt converts a JSON value that may be a number (9), a string ("9"),
// or a zero-padded string ("00") to an int.
func flexToInt(v any) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case string:
		return strconv.Atoi(val)
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}

// Profile represents a row from the profiles table.
type Profile struct {
	ID          string  `db:"id"`
	ProfileNo   int     `db:"profile_no"`
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
