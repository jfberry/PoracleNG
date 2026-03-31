package bot

import "github.com/jmoiron/sqlx"

// LookupUserState loads basic user info from the humans table.
// isRegistered is true when the user exists in the humans table.
func LookupUserState(db *sqlx.DB, userID, defaultLocale string) (lang string, profileNo int, hasLocation, hasArea, isRegistered bool) {
	lang = defaultLocale
	profileNo = 1

	var h struct {
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := db.Get(&h, "SELECT language, current_profile_no, latitude, longitude, area FROM humans WHERE id = ? LIMIT 1", userID)
	if err != nil {
		return
	}

	isRegistered = true
	if h.Language != nil && *h.Language != "" {
		lang = *h.Language
	}
	profileNo = h.ProfileNo
	hasLocation = h.Latitude != 0 || h.Longitude != 0
	hasArea = h.Area != nil && *h.Area != "" && *h.Area != "[]"
	return
}
