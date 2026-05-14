package db

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"
)

// ProfileKey uniquely identifies a profile.
type ProfileKey struct {
	ID        string
	ProfileNo int
}

// ActiveHourEntry represents a time-of-day rule for auto-switching
// profiles or firing summaries. Fields may be stored as numbers or
// strings in the DB JSON (including zero-padded strings like "00"),
// so we decode into interface{} and coerce.
//
// Two shapes are supported on disk:
//
//   - **Single fire**: Day, Hours, Mins set; EndHours/EndMins/Step
//     absent (or zero). Fires once that day at HH:MM.
//   - **Range with step**: Day, Hours, Mins set, plus EndHours/EndMins
//     and Step (hours). Fires at HH:MM, HH:MM+Step*h, HH:MM+2*Step*h,
//     …, up to and including EndHours:EndMins. Cross-midnight is
//     rejected by the parser; here we trust the on-disk shape.
//
// Range entries are marked by Step > 0. The omitempty tags on the
// new fields preserve the existing on-disk shape for single-fire
// entries — older readers see no change.
type ActiveHourEntry struct {
	Day      int `json:"day"`
	Hours    int `json:"hours"`
	Mins     int `json:"mins"`
	EndHours int `json:"end_hours,omitempty"`
	EndMins  int `json:"end_mins,omitempty"`
	Step     int `json:"step,omitempty"`
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
	// Range fields are optional — absent in the JSON is fine (single-fire entry).
	if v, ok := raw["end_hours"]; ok {
		if e.EndHours, err = flexToInt(v); err != nil {
			return fmt.Errorf("end_hours: %w", err)
		}
	}
	if v, ok := raw["end_mins"]; ok {
		if e.EndMins, err = flexToInt(v); err != nil {
			return fmt.Errorf("end_mins: %w", err)
		}
	}
	if v, ok := raw["step"]; ok {
		if e.Step, err = flexToInt(v); err != nil {
			return fmt.Errorf("step: %w", err)
		}
	}
	return nil
}

// IsRange reports whether this entry is the range+step shape (true)
// or a single-fire entry (false). Driven by Step > 0 so existing data
// without the new fields is treated as single-fire automatically.
func (e ActiveHourEntry) IsRange() bool { return e.Step > 0 }

// Fires returns every (hour, minute) fire-point this entry produces
// in a given day, in chronological order. Single-fire entries return
// one pair; range entries return start, start+step*h, … up to and
// including end-inclusive. Always returns at least one entry.
func (e ActiveHourEntry) Fires() [][2]int {
	if !e.IsRange() {
		return [][2]int{{e.Hours, e.Mins}}
	}
	startMin := e.Hours*60 + e.Mins
	endMin := e.EndHours*60 + e.EndMins
	stepMin := e.Step * 60
	if stepMin <= 0 || endMin < startMin {
		// Defensive: shape we should have rejected at parse. Fall
		// back to single-fire so the scheduler still produces *some*
		// reasonable behaviour.
		return [][2]int{{e.Hours, e.Mins}}
	}
	out := make([][2]int, 0, (endMin-startMin)/stepMin+1)
	for m := startMin; m <= endMin; m += stepMin {
		out = append(out, [2]int{m / 60, m % 60})
	}
	return out
}

// ParseActiveHours decodes the on-disk active_hours JSON shape into
// typed entries. Empty / placeholder strings (`""`, `"[]"`, `"{}"`)
// return (nil, nil) — a missing schedule is not an error. Malformed
// JSON returns the underlying error so callers can choose to log
// vs. fail.
//
// We explicitly enumerate the placeholder forms rather than using a
// short-length heuristic: a `len(raw) <= 5` cutoff would silently
// accept `"[null]"` (6 chars) which json.Unmarshal happily decodes
// into a single zero-value ActiveHourEntry (Day=0, 00:00 — fires
// every Sunday at midnight). Better to short-circuit on the actual
// placeholders and let everything else go through the parser.
func ParseActiveHours(raw string) ([]ActiveHourEntry, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" || trimmed == "{}" {
		return nil, nil
	}
	var entries []ActiveHourEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	return entries, nil
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
		entries, err := ParseActiveHours(p.ActiveHours)
		if err != nil {
			log.Warnf("Profile %s/%d: failed to parse active_hours: %s", p.ID, p.ProfileNo, err)
		} else {
			p.ParsedActiveHours = entries
		}
		profiles[ProfileKey{ID: p.ID, ProfileNo: p.ProfileNo}] = p
	}
	return profiles, nil
}
