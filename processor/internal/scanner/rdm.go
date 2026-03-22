package scanner

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"
)

// RDMScanner queries stop/gym data from an RDM-schema database.
type RDMScanner struct {
	db *sqlx.DB
}

// NewRDM creates a new RDMScanner with the given DSN.
func NewRDM(dsn string) (*RDMScanner, error) {
	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("scanner: open rdm db: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("scanner: ping rdm db: %w", err)
	}
	return &RDMScanner{db: db}, nil
}

// GetStopData returns all pokestops and gyms within the given bounding box.
func (s *RDMScanner) GetStopData(minLat, minLon, maxLat, maxLon float64) ([]StopData, error) {
	lo := math.Min(minLat, maxLat)
	hi := math.Max(minLat, maxLat)
	loLon := math.Min(minLon, maxLon)
	hiLon := math.Max(minLon, maxLon)

	var result []StopData

	// Pokestops
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

	// Gyms
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
func (s *RDMScanner) GetPokestopName(pokestopID string) (string, error) {
	var name string
	err := s.db.Get(&name, "SELECT name FROM pokestop WHERE id = ?", pokestopID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// GetGymName returns the name of a gym by ID.
func (s *RDMScanner) GetGymName(gymID string) (string, error) {
	var name string
	err := s.db.Get(&name, "SELECT name FROM gym WHERE id = ?", gymID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// Close closes the underlying database connection.
func (s *RDMScanner) Close() error {
	return s.db.Close()
}
