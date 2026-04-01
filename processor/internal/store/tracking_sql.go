package store

import (
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// sqlTrackingStore is a generic SQL-backed TrackingStore.
// It delegates to type-specific select/insert functions since each type has
// different columns and COALESCE defaults.
type sqlTrackingStore[T any] struct {
	database  *sqlx.DB
	table     string
	selectFn  func(*sqlx.DB, string, int) ([]T, error)
	insertFn  func(*sqlx.DB, *T) (int64, error)
}

func (s *sqlTrackingStore[T]) SelectByIDProfile(id string, profileNo int) ([]T, error) {
	return s.selectFn(s.database, id, profileNo)
}

func (s *sqlTrackingStore[T]) Insert(row *T) (int64, error) {
	return s.insertFn(s.database, row)
}

func (s *sqlTrackingStore[T]) DeleteByUIDs(id string, uids []int64) error {
	return db.DeleteByUIDs(s.database, s.table, id, uids)
}

func (s *sqlTrackingStore[T]) DeleteByUID(id string, uid int64) error {
	return db.DeleteByUID(s.database, s.table, id, uid)
}

// --- Per-type constructors ---

func NewEggStore(database *sqlx.DB) TrackingStore[db.EggTrackingAPI] {
	return &sqlTrackingStore[db.EggTrackingAPI]{
		database: database,
		table:    "egg",
		selectFn: db.SelectEggsByIDProfile,
		insertFn: db.InsertEgg,
	}
}

func NewRaidStore(database *sqlx.DB) TrackingStore[db.RaidTrackingAPI] {
	return &sqlTrackingStore[db.RaidTrackingAPI]{
		database: database,
		table:    "raid",
		selectFn: db.SelectRaidsByIDProfile,
		insertFn: db.InsertRaid,
	}
}

func NewMonsterStore(database *sqlx.DB) TrackingStore[db.MonsterTrackingAPI] {
	return &sqlTrackingStore[db.MonsterTrackingAPI]{
		database: database,
		table:    "monsters",
		selectFn: db.SelectMonstersByIDProfile,
		insertFn: db.InsertMonster,
	}
}

func NewQuestStore(database *sqlx.DB) TrackingStore[db.QuestTrackingAPI] {
	return &sqlTrackingStore[db.QuestTrackingAPI]{
		database: database,
		table:    "quest",
		selectFn: db.SelectQuestsByIDProfile,
		insertFn: db.InsertQuest,
	}
}

func NewInvasionStore(database *sqlx.DB) TrackingStore[db.InvasionTrackingAPI] {
	return &sqlTrackingStore[db.InvasionTrackingAPI]{
		database: database,
		table:    "invasion",
		selectFn: db.SelectInvasionsByIDProfile,
		insertFn: db.InsertInvasion,
	}
}

func NewLureStore(database *sqlx.DB) TrackingStore[db.LureTrackingAPI] {
	return &sqlTrackingStore[db.LureTrackingAPI]{
		database: database,
		table:    "lures",
		selectFn: db.SelectLuresByIDProfile,
		insertFn: db.InsertLure,
	}
}

func NewNestStore(database *sqlx.DB) TrackingStore[db.NestTrackingAPI] {
	return &sqlTrackingStore[db.NestTrackingAPI]{
		database: database,
		table:    "nests",
		selectFn: db.SelectNestsByIDProfile,
		insertFn: db.InsertNest,
	}
}

func NewGymStore(database *sqlx.DB) TrackingStore[db.GymTrackingAPI] {
	return &sqlTrackingStore[db.GymTrackingAPI]{
		database: database,
		table:    "gym",
		selectFn: db.SelectGymsByIDProfile,
		insertFn: db.InsertGym,
	}
}

func NewFortStore(database *sqlx.DB) TrackingStore[db.FortTrackingAPI] {
	return &sqlTrackingStore[db.FortTrackingAPI]{
		database: database,
		table:    "forts",
		selectFn: db.SelectFortsByIDProfile,
		insertFn: db.InsertFort,
	}
}

func NewMaxbattleStore(database *sqlx.DB) TrackingStore[db.MaxbattleTrackingAPI] {
	return &sqlTrackingStore[db.MaxbattleTrackingAPI]{
		database: database,
		table:    "maxbattle",
		selectFn: db.SelectMaxbattlesByIDProfile,
		insertFn: db.InsertMaxbattle,
	}
}

// TrackingStores holds all 10 tracking store instances.
type TrackingStores struct {
	Monsters   TrackingStore[db.MonsterTrackingAPI]
	Raids      TrackingStore[db.RaidTrackingAPI]
	Eggs       TrackingStore[db.EggTrackingAPI]
	Quests     TrackingStore[db.QuestTrackingAPI]
	Invasions  TrackingStore[db.InvasionTrackingAPI]
	Lures      TrackingStore[db.LureTrackingAPI]
	Nests      TrackingStore[db.NestTrackingAPI]
	Gyms       TrackingStore[db.GymTrackingAPI]
	Forts      TrackingStore[db.FortTrackingAPI]
	Maxbattles TrackingStore[db.MaxbattleTrackingAPI]
}

// NewTrackingStores creates all 10 SQL-backed tracking stores.
func NewTrackingStores(database *sqlx.DB) *TrackingStores {
	return &TrackingStores{
		Monsters:   NewMonsterStore(database),
		Raids:      NewRaidStore(database),
		Eggs:       NewEggStore(database),
		Quests:     NewQuestStore(database),
		Invasions:  NewInvasionStore(database),
		Lures:      NewLureStore(database),
		Nests:      NewNestStore(database),
		Gyms:       NewGymStore(database),
		Forts:      NewFortStore(database),
		Maxbattles: NewMaxbattleStore(database),
	}
}

// UID accessor functions for each tracking type.
// These are needed by DiffAndClassify/ApplyDiff since generics can't access struct fields.

func EggGetUID(e *db.EggTrackingAPI) int64         { return e.UID }
func EggSetUID(e *db.EggTrackingAPI, uid int64)     { e.UID = uid }
func RaidGetUID(r *db.RaidTrackingAPI) int64        { return r.UID }
func RaidSetUID(r *db.RaidTrackingAPI, uid int64)   { r.UID = uid }
func MonsterGetUID(m *db.MonsterTrackingAPI) int64   { return m.UID }
func MonsterSetUID(m *db.MonsterTrackingAPI, uid int64) { m.UID = uid }
func QuestGetUID(q *db.QuestTrackingAPI) int64      { return q.UID }
func QuestSetUID(q *db.QuestTrackingAPI, uid int64)  { q.UID = uid }
func InvasionGetUID(i *db.InvasionTrackingAPI) int64 { return i.UID }
func InvasionSetUID(i *db.InvasionTrackingAPI, uid int64) { i.UID = uid }
func LureGetUID(l *db.LureTrackingAPI) int64        { return l.UID }
func LureSetUID(l *db.LureTrackingAPI, uid int64)   { l.UID = uid }
func NestGetUID(n *db.NestTrackingAPI) int64        { return n.UID }
func NestSetUID(n *db.NestTrackingAPI, uid int64)   { n.UID = uid }
func GymGetUID(g *db.GymTrackingAPI) int64          { return g.UID }
func GymSetUID(g *db.GymTrackingAPI, uid int64)     { g.UID = uid }
func FortGetUID(f *db.FortTrackingAPI) int64        { return f.UID }
func FortSetUID(f *db.FortTrackingAPI, uid int64)   { f.UID = uid }
func MaxbattleGetUID(m *db.MaxbattleTrackingAPI) int64  { return m.UID }
func MaxbattleSetUID(m *db.MaxbattleTrackingAPI, uid int64) { m.UID = uid }

