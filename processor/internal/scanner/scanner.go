// Package scanner provides an interface and implementations for querying
// stop and gym data from the scanner database (RDM or Golbat).
package scanner

// StopData represents a pokestop or gym from the scanner database.
type StopData struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Type      string  `json:"type"` // "stop" or "gym"
	TeamID    int     `json:"teamId,omitempty"`
	Slots     int     `json:"slots,omitempty"`
}

// Gym is a minimal gym record returned by the resolve helpers used by
// the !raid / !egg / !gym commands when a user passes a `gym:` argument.
// Latitude / Longitude are included so the disambiguation reply can
// hint at location for similarly-named gyms.
type Gym struct {
	ID        string  `db:"id"`
	Name      string  `db:"name"`
	Latitude  float64 `db:"lat"`
	Longitude float64 `db:"lon"`
}

// MaxGymNameLength caps how many bytes of user input the gym-by-name
// search will pass to the scanner DB. Defence-in-depth: parameterised
// queries make injection irrelevant, but a pathological 1MB LIKE
// pattern is still wasted work.
const MaxGymNameLength = 100

// Scanner queries the scanner database for stop/gym data.
type Scanner interface {
	GetStopData(minLat, minLon, maxLat, maxLon float64) ([]StopData, error)
	GetPokestopName(pokestopID string) (string, error)
	GetGymName(gymID string) (string, error)
	GetStationName(stationID string) (string, error)

	// FindGymByID returns the gym with the given exact ID. ok=false
	// when no such gym exists; err is non-nil only on a DB-level
	// failure. Implementations must use a parameterised query.
	FindGymByID(id string) (gym Gym, ok bool, err error)

	// FindGymsByName returns up to limit gyms whose name matches the
	// given string (case-insensitive substring). Implementations must
	// clamp the input length to MaxGymNameLength, parameterise the
	// query, and apply LIMIT at the SQL level. The empty result is
	// not an error.
	FindGymsByName(name string, limit int) ([]Gym, error)
}
