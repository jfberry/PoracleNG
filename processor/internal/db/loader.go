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
	Humans    map[string]*Human
	Monsters  *MonsterIndex
	Raids     []*RaidTracking
	Eggs      []*EggTracking
	Profiles  map[ProfileKey]*Profile
	Invasions []*InvasionTracking
	Quests    []*QuestTracking
	Lures     []*LureTracking
	Gyms      []*GymTracking
	Nests     []*NestTracking
	Forts     []*FortTracking
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
	invasions, err := LoadInvasions(db)
	if err != nil {
		return nil, fmt.Errorf("load invasions: %w", err)
	}
	quests, err := LoadQuests(db)
	if err != nil {
		return nil, fmt.Errorf("load quests: %w", err)
	}
	lures, err := LoadLures(db)
	if err != nil {
		return nil, fmt.Errorf("load lures: %w", err)
	}
	gyms, err := LoadGyms(db)
	if err != nil {
		return nil, fmt.Errorf("load gyms: %w", err)
	}
	nests, err := LoadNests(db)
	if err != nil {
		return nil, fmt.Errorf("load nests: %w", err)
	}
	forts, err := LoadForts(db)
	if err != nil {
		return nil, fmt.Errorf("load forts: %w", err)
	}
	return &AllData{
		Humans:    humans,
		Monsters:  monsters,
		Raids:     raids,
		Eggs:      eggs,
		Profiles:  profiles,
		Invasions: invasions,
		Quests:    quests,
		Lures:     lures,
		Gyms:      gyms,
		Nests:     nests,
		Forts:     forts,
	}, nil
}
