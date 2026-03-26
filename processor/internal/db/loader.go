package db

import (
	"fmt"
	"time"

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
	db.SetConnMaxLifetime(5 * time.Minute)
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
	Forts       []*FortTracking
	Maxbattles  []*MaxbattleTracking
}

// LoadAll loads all tracking data from the database.
// Tracking rows without a matching human are filtered out in-memory
// (no FK constraints, so orphaned rows may exist).
func LoadAll(db *sqlx.DB) (*AllData, error) {
	humans, err := LoadHumans(db)
	if err != nil {
		return nil, fmt.Errorf("load humans: %w", err)
	}
	monsters, err := LoadMonsters(db)
	if err != nil {
		return nil, fmt.Errorf("load monsters: %w", err)
	}
	monsters.FilterOrphans(humans)
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
	maxbattles, err := LoadMaxbattles(db)
	if err != nil {
		return nil, fmt.Errorf("load maxbattles: %w", err)
	}

	// Filter orphaned tracking/profile rows (no matching human)
	for key := range profiles {
		if _, ok := humans[key.ID]; !ok {
			delete(profiles, key)
		}
	}
	raids = filterSlice(raids, humans)
	eggs = filterSlice(eggs, humans)
	invasions = filterSlice(invasions, humans)
	quests = filterSlice(quests, humans)
	lures = filterSlice(lures, humans)
	gyms = filterSlice(gyms, humans)
	nests = filterSlice(nests, humans)
	forts = filterSlice(forts, humans)
	maxbattles = filterSlice(maxbattles, humans)

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
		Forts:      forts,
		Maxbattles: maxbattles,
	}, nil
}

// idGetter is implemented by all tracking structs (they all have an ID field).
type idGetter interface {
	GetID() string
}

// filterSlice removes tracking entries whose ID is not in the humans map.
func filterSlice[T idGetter](items []T, humans map[string]*Human) []T {
	n := 0
	for _, item := range items {
		if _, ok := humans[item.GetID()]; ok {
			items[n] = item
			n++
		}
	}
	return items[:n]
}
