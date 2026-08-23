package api

import "github.com/pokemon/poracleng/processor/internal/store"

// ProfileResponse is the JSON shape returned by /api/profiles/* endpoints
// and by tracking responses that include profile info. Mirrors the legacy
// db.ProfileRow JSON layout so existing clients (PoracleWeb) continue to
// receive the same wire format after the internal migration to store.Profile.
type ProfileResponse struct {
	UID         int     `json:"uid" doc:"Database row id of the profile."`
	ID          string  `json:"id" doc:"Owning destination id (humans.id)."`
	ProfileNo   int     `json:"profile_no" doc:"Profile number within the user (1 = default profile)."`
	Name        string  `json:"name" doc:"Profile display name."`
	Area        string  `json:"area" doc:"Profile geofence-area override as a JSON-encoded string array — clients must parse the string. \"[]\" when the profile has no area override."`
	Latitude    float64 `json:"latitude" doc:"Profile location-override latitude. 0 when the profile has no location override."`
	Longitude   float64 `json:"longitude" doc:"Profile location-override longitude. 0 when the profile has no location override."`
	ActiveHours string  `json:"active_hours" doc:"Auto-switch schedule as a raw JSON string (legacy encoding; the typed shape is the v2 profiles endpoint's active_hours array). \"[]\" or \"\" when unscheduled."`
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
