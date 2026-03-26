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

// Scanner queries the scanner database for stop/gym data.
type Scanner interface {
	GetStopData(minLat, minLon, maxLat, maxLon float64) ([]StopData, error)
	GetPokestopName(pokestopID string) (string, error)
	GetGymName(gymID string) (string, error)
	GetStationName(stationID string) (string, error)
}
