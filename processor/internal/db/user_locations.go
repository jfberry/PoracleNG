package db

import (
	"strings"

	"github.com/jmoiron/sqlx"
)

// UserLocation is a row from the user_locations table.
type UserLocation struct {
	UID       int64   `db:"uid"`
	ID        string  `db:"id"`
	Label     string  `db:"label"`
	Latitude  float64 `db:"latitude"`
	Longitude float64 `db:"longitude"`
}

// LoadUserLocations loads every saved user location and indexes them by
// (human id) → (lowercased label) → location. Lookups in the matcher are
// case-insensitive, so we lowercase here once at load rather than at each
// match.
func LoadUserLocations(dbx *sqlx.DB) (map[string]map[string]*UserLocation, error) {
	var rows []UserLocation
	if err := dbx.Select(&rows, `SELECT uid, id, label, latitude, longitude FROM user_locations`); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]*UserLocation)
	for i := range rows {
		r := &rows[i]
		m, ok := out[r.ID]
		if !ok {
			m = make(map[string]*UserLocation)
			out[r.ID] = m
		}
		m[strings.ToLower(r.Label)] = r
	}
	return out, nil
}
