package scanner

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"
)

// GolbatScanner queries stop/gym data from a Golbat-schema database.
// Currently uses the same table schema as RDM. Kept as a separate type
// so it can later switch to the Golbat HTTP API.
type GolbatScanner struct {
	db *sqlx.DB
}

// NewGolbat creates a new GolbatScanner with the given DSN.
func NewGolbat(dsn string) (*GolbatScanner, error) {
	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("scanner: open golbat db: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("scanner: ping golbat db: %w", err)
	}
	return &GolbatScanner{db: db}, nil
}

// GetStopData returns all pokestops and gyms within the given bounding box.
func (s *GolbatScanner) GetStopData(minLat, minLon, maxLat, maxLon float64) ([]StopData, error) {
	lo := math.Min(minLat, maxLat)
	hi := math.Max(minLat, maxLat)
	loLon := math.Min(minLon, maxLon)
	hiLon := math.Max(minLon, maxLon)

	var result []StopData

	type stopRow struct {
		Lat float64 `db:"lat"`
		Lon float64 `db:"lon"`
	}
	var stops []stopRow
	err := s.db.Select(&stops,
		"SELECT lat, lon FROM pokestop WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ? AND deleted = 0 AND enabled = 1",
		lo, hi, loLon, hiLon)
	if err != nil {
		return nil, fmt.Errorf("scanner: query pokestops: %w", err)
	}
	for _, st := range stops {
		result = append(result, StopData{Latitude: st.Lat, Longitude: st.Lon, Type: "stop"})
	}

	type gymRow struct {
		Lat            float64 `db:"lat"`
		Lon            float64 `db:"lon"`
		TeamID         int     `db:"team_id"`
		AvailableSlots int     `db:"available_slots"`
	}
	var gyms []gymRow
	err = s.db.Select(&gyms,
		"SELECT lat, lon, team_id, available_slots FROM gym WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ? AND deleted = 0 AND enabled = 1",
		lo, hi, loLon, hiLon)
	if err != nil {
		return nil, fmt.Errorf("scanner: query gyms: %w", err)
	}
	for _, g := range gyms {
		result = append(result, StopData{
			Latitude:  g.Lat,
			Longitude: g.Lon,
			Type:      "gym",
			TeamID:    g.TeamID,
			Slots:     g.AvailableSlots,
		})
	}

	return result, nil
}

// GetPokestopName returns the name of a pokestop by ID.
func (s *GolbatScanner) GetPokestopName(pokestopID string) (string, error) {
	var name string
	err := s.db.Get(&name, "SELECT name FROM pokestop WHERE id = ?", pokestopID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// GetGymName returns the name of a gym by ID.
func (s *GolbatScanner) GetGymName(gymID string) (string, error) {
	var name string
	err := s.db.Get(&name, "SELECT name FROM gym WHERE id = ?", gymID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// GetStationName returns the name of a max battle station by ID.
func (s *GolbatScanner) GetStationName(stationID string) (string, error) {
	var name string
	err := s.db.Get(&name, "SELECT name FROM station WHERE id = ?", stationID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// Close closes the underlying database connection.
func (s *GolbatScanner) Close() error {
	return s.db.Close()
}
