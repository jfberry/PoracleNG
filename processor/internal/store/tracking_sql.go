package store

import (
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// sqlTrackingStore is a generic SQL-backed TrackingStore.
// It delegates to type-specific select/insert functions since each type has
// different columns and COALESCE defaults.
type sqlTrackingStore[T any] struct {
	database *sqlx.DB
	table    string
	selectFn func(*sqlx.DB, string, int) ([]T, error)
	insertFn func(*sqlx.DB, *T) (int64, error)
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

func (s *sqlTrackingStore[T]) ReassignUID(id string, from, to int64) error {
	return db.ReassignUID(s.database, s.table, id, from, to)
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

func NewTappableStore(database *sqlx.DB) TrackingStore[db.TappableTrackingAPI] {
	return &sqlTrackingStore[db.TappableTrackingAPI]{
		database: database,
		table:    "tappable",
		selectFn: db.SelectTappablesByIDProfile,
		insertFn: db.InsertTappable,
	}
}

// TrackingStores holds all 11 tracking store instances.
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
	Tappables  TrackingStore[db.TappableTrackingAPI]
}

// NewTrackingStores creates all 11 SQL-backed tracking stores.
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
		Tappables:  NewTappableStore(database),
	}
}

// UID accessor functions for each tracking type.
// These are needed by DiffAndClassify/ApplyDiff since generics can't access struct fields.

func EggGetUID(e *db.EggTrackingAPI) int64                  { return e.UID }
func EggSetUID(e *db.EggTrackingAPI, uid int64)             { e.UID = uid }
func RaidGetUID(r *db.RaidTrackingAPI) int64                { return r.UID }
func RaidSetUID(r *db.RaidTrackingAPI, uid int64)           { r.UID = uid }
func MonsterGetUID(m *db.MonsterTrackingAPI) int64          { return m.UID }
func MonsterSetUID(m *db.MonsterTrackingAPI, uid int64)     { m.UID = uid }
func QuestGetUID(q *db.QuestTrackingAPI) int64              { return q.UID }
func QuestSetUID(q *db.QuestTrackingAPI, uid int64)         { q.UID = uid }
func InvasionGetUID(i *db.InvasionTrackingAPI) int64        { return i.UID }
func InvasionSetUID(i *db.InvasionTrackingAPI, uid int64)   { i.UID = uid }
func LureGetUID(l *db.LureTrackingAPI) int64                { return l.UID }
func LureSetUID(l *db.LureTrackingAPI, uid int64)           { l.UID = uid }
func NestGetUID(n *db.NestTrackingAPI) int64                { return n.UID }
func NestSetUID(n *db.NestTrackingAPI, uid int64)           { n.UID = uid }
func GymGetUID(g *db.GymTrackingAPI) int64                  { return g.UID }
func GymSetUID(g *db.GymTrackingAPI, uid int64)             { g.UID = uid }
func FortGetUID(f *db.FortTrackingAPI) int64                { return f.UID }
func FortSetUID(f *db.FortTrackingAPI, uid int64)           { f.UID = uid }
func MaxbattleGetUID(m *db.MaxbattleTrackingAPI) int64      { return m.UID }
func MaxbattleSetUID(m *db.MaxbattleTrackingAPI, uid int64) { m.UID = uid }
func TappableGetUID(t *db.TappableTrackingAPI) int64        { return t.UID }
func TappableSetUID(t *db.TappableTrackingAPI, uid int64)   { t.UID = uid }

// TablesWithUID returns the canonical rule-table keys (the mute.CanonicalRuleTable
// vocabulary) of every table holding a rule with this uid for the human's
// profile. uids are per-table, so more than one table can match. A store that
// is nil or errors is skipped.
func (ts *TrackingStores) TablesWithUID(id string, profileNo int, uid int64) []string {
	if ts == nil {
		return nil
	}
	var out []string
	check := func(table string, has func() bool) {
		if has() {
			out = append(out, table)
		}
	}
	hasUID := func(n int, uidAt func(i int) int64) bool {
		for i := 0; i < n; i++ {
			if uidAt(i) == uid {
				return true
			}
		}
		return false
	}
	check("pokemon", func() bool {
		if ts.Monsters == nil {
			return false
		}
		r, err := ts.Monsters.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("raid", func() bool {
		if ts.Raids == nil {
			return false
		}
		r, err := ts.Raids.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("egg", func() bool {
		if ts.Eggs == nil {
			return false
		}
		r, err := ts.Eggs.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("quest", func() bool {
		if ts.Quests == nil {
			return false
		}
		r, err := ts.Quests.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("invasion", func() bool {
		if ts.Invasions == nil {
			return false
		}
		r, err := ts.Invasions.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("lure", func() bool {
		if ts.Lures == nil {
			return false
		}
		r, err := ts.Lures.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("nest", func() bool {
		if ts.Nests == nil {
			return false
		}
		r, err := ts.Nests.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("gym", func() bool {
		if ts.Gyms == nil {
			return false
		}
		r, err := ts.Gyms.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("fort", func() bool {
		if ts.Forts == nil {
			return false
		}
		r, err := ts.Forts.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("maxbattle", func() bool {
		if ts.Maxbattles == nil {
			return false
		}
		r, err := ts.Maxbattles.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	check("tappable", func() bool {
		if ts.Tappables == nil {
			return false
		}
		r, err := ts.Tappables.SelectByIDProfile(id, profileNo)
		return err == nil && hasUID(len(r), func(i int) int64 { return r[i].UID })
	})
	return out
}
