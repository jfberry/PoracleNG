package db

import (
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// OpenDB opens a MySQL connection using the given DSN.
func OpenDB(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return db, nil
}

// AllData holds all loaded database data.
type AllData struct {
	Humans   map[string]*Human
	Monsters *MonsterIndex
	Raids    []*RaidTracking
	Eggs     []*EggTracking
	Profiles map[ProfileKey]*Profile
}

// LoadAll loads all tracking data from the database.
func LoadAll(db *sqlx.DB) (*AllData, error) {
	humans, err := LoadHumans(db)
	if err != nil {
		return nil, fmt.Errorf("load humans: %w", err)
	}
	monsters, err := LoadMonsters(db)
	if err != nil {
		return nil, fmt.Errorf("load monsters: %w", err)
	}
	raids, err := LoadRaids(db)
	if err != nil {
		return nil, fmt.Errorf("load raids: %w", err)
	}
	eggs, err := LoadEggs(db)
	if err != nil {
		return nil, fmt.Errorf("load eggs: %w", err)
	}
	profiles, err := LoadProfiles(db)
	if err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}
	return &AllData{
		Humans:   humans,
		Monsters: monsters,
		Raids:    raids,
		Eggs:     eggs,
		Profiles: profiles,
	}, nil
}
