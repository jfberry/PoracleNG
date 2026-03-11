package db

import "github.com/jmoiron/sqlx"

// ProfileKey uniquely identifies a profile.
type ProfileKey struct {
	ID        string
	ProfileNo int
}

// Profile represents a row from the profiles table.
type Profile struct {
	ID          string `db:"id"`
	ProfileNo   int    `db:"profile_no"`
	Name        string `db:"name"`
	ActiveHours string `db:"active_hours"`
}

// LoadProfiles loads all profiles from the database.
func LoadProfiles(db *sqlx.DB) (map[ProfileKey]*Profile, error) {
	var rows []Profile
	err := db.Select(&rows,
		`SELECT id, profile_no, name, COALESCE(active_hours, '') AS active_hours
		 FROM profiles`)
	if err != nil {
		return nil, err
	}

	profiles := make(map[ProfileKey]*Profile, len(rows))
	for i := range rows {
		p := &rows[i]
		profiles[ProfileKey{ID: p.ID, ProfileNo: p.ProfileNo}] = p
	}
	return profiles, nil
}
