package api

import "github.com/pokemon/poracleng/processor/internal/store"

// ProfileResponse is the JSON shape returned by /api/profiles/* endpoints
// and by tracking responses that include profile info. Mirrors the legacy
// db.ProfileRow JSON layout so existing clients (PoracleWeb) continue to
// receive the same wire format after the internal migration to store.Profile.
type ProfileResponse struct {
	UID         int     `json:"uid"`
	ID          string  `json:"id"`
	ProfileNo   int     `json:"profile_no"`
	Name        string  `json:"name"`
	Area        string  `json:"area"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	ActiveHours string  `json:"active_hours"`
}

// profileToResponse converts a typed store.Profile into the legacy API
// response shape (area encoded as JSON string, not array).
func profileToResponse(p store.Profile) ProfileResponse {
	return ProfileResponse{
		UID:         p.UID,
		ID:          p.ID,
		ProfileNo:   p.ProfileNo,
		Name:        p.Name,
		Area:        stringSliceToJSON(p.Area),
		Latitude:    p.Latitude,
		Longitude:   p.Longitude,
		ActiveHours: p.ActiveHours,
	}
}

// profilesToResponse converts a slice of store.Profile to a slice of DTOs.
func profilesToResponse(profiles []store.Profile) []ProfileResponse {
	out := make([]ProfileResponse, len(profiles))
	for i, p := range profiles {
		out[i] = profileToResponse(p)
	}
	return out
}
