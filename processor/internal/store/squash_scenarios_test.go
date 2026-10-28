package store

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// scenarioType adapts one tracking struct to the shared scenario table. knob
// builds a rule whose single updatable field "knob" (min_iv for pokemon,
// clean for the others) and distance are set.
type scenarioType[T any] struct {
	name string
	knob func(knob, dist int) T
	get  func(*T) int64
	set  func(*T, int64)
	val  func(*T) int // reads the knob back
}

func monsterScenario() scenarioType[db.MonsterTrackingAPI] {
	return scenarioType[db.MonsterTrackingAPI]{
		name: "monster",
		knob: func(k, d int) db.MonsterTrackingAPI {
			return db.MonsterTrackingAPI{ID: "u", PokemonID: 25, MinIV: k, Distance: d}
		},
		get: MonsterGetUID, set: MonsterSetUID,
		val: func(r *db.MonsterTrackingAPI) int { return r.MinIV },
	}
}

func raidScenario() scenarioType[db.RaidTrackingAPI] {
	return scenarioType[db.RaidTrackingAPI]{
		name: "raid",
		knob: func(k, d int) db.RaidTrackingAPI {
			return db.RaidTrackingAPI{ID: "u", PokemonID: 150, Level: 5, Clean: k, Distance: d}
		},
		get: RaidGetUID, set: RaidSetUID,
		val: func(r *db.RaidTrackingAPI) int { return r.Clean },
	}
}

func maxbattleScenario() scenarioType[db.MaxbattleTrackingAPI] {
	return scenarioType[db.MaxbattleTrackingAPI]{
		name: "maxbattle",
		knob: func(k, d int) db.MaxbattleTrackingAPI {
			return db.MaxbattleTrackingAPI{ID: "u", PokemonID: 6, Level: 5, Clean: k, Distance: d}
		},
		get: MaxbattleGetUID, set: MaxbattleSetUID,
		val: func(r *db.MaxbattleTrackingAPI) int { return r.Clean },
	}
}

func tappableScenario() scenarioType[db.TappableTrackingAPI] {
	return scenarioType[db.TappableTrackingAPI]{
		name: "tappable",
		knob: func(k, d int) db.TappableTrackingAPI {
			return db.TappableTrackingAPI{ID: "u", RewardType: 4, Reward: 41, Amount: 3, Clean: k, Distance: d}
		},
		get: TappableGetUID, set: TappableSetUID,
		val: func(r *db.TappableTrackingAPI) int { return r.Clean },
	}
}

func TestSquashScenarios(t *testing.T) {
	t.Run("monster", func(t *testing.T) { runScenarios(t, monsterScenario()) })
	t.Run("raid", func(t *testing.T) { runScenarios(t, raidScenario()) })
	t.Run("maxbattle", func(t *testing.T) { runScenarios(t, maxbattleScenario()) })
	t.Run("tappable", func(t *testing.T) { runScenarios(t, tappableScenario()) })
}

func runScenarios[T any](t *testing.T, s scenarioType[T]) {
	policy := RowDiffPolicy[T]{GetUID: s.get, SetUID: s.set}
	row := func(uid int64, knob, dist int) T {
		r := s.knob(knob, dist)
		s.set(&r, uid)
		return r
	}

	t.Run("one updatable field differs updates the existing uid", func(t *testing.T) {
		existing := []T{row(1, 90, 0)}
		res := policy.Classify(existing, []T{s.knob(80, 0)})
		if len(res.Updates) != 1 || len(res.Inserts) != 0 || len(res.AlreadyPresent) != 0 {
			t.Fatalf("got %+v", res)
		}
		if s.get(&res.Updates[0]) != 1 {
			t.Fatalf("update uid = %d", s.get(&res.Updates[0]))
		}
		if len(res.Replaced) != 1 || s.get(&res.Replaced[0]) != 1 || s.val(&res.Replaced[0]) != 90 {
			t.Fatalf("replaced = %+v", res.Replaced)
		}
		if s.val(&res.Updates[0]) != 80 {
			t.Fatalf("update knob = %d", s.val(&res.Updates[0]))
		}
	})

	t.Run("identical rule is already present", func(t *testing.T) {
		res := policy.Classify([]T{row(1, 90, 0)}, []T{s.knob(90, 0)})
		if len(res.AlreadyPresent) != 1 || len(res.Updates) != 0 || len(res.Inserts) != 0 {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("two differing fields insert (recorded behaviour, spec 3.2 row 6)", func(t *testing.T) {
		res := policy.Classify([]T{row(1, 90, 1000)}, []T{s.knob(95, 500)})
		if len(res.Inserts) != 1 || len(res.Updates) != 0 || len(res.AlreadyPresent) != 0 {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("batch: an existing row is updated by at most one candidate", func(t *testing.T) {
		existing := []T{row(1, 90, 0), row(2, 70, 0)}
		res := policy.Classify(existing, []T{s.knob(85, 0), s.knob(80, 0)})
		if len(res.Updates) != 2 || len(res.Inserts) != 0 {
			t.Fatalf("got %+v", res)
		}
		seen := map[int64]bool{}
		for i := range res.Updates {
			u := s.get(&res.Updates[i])
			if seen[u] {
				t.Fatalf("uid %d updated twice: %+v", u, res.Updates)
			}
			seen[u] = true
			if s.get(&res.Replaced[i]) != u {
				t.Fatalf("Replaced[%d] uid %d != Updates uid %d", i, s.get(&res.Replaced[i]), u)
			}
		}
		if !seen[1] || !seen[2] {
			t.Fatalf("expected updates of 1 and 2, got %v", seen)
		}
	})

	t.Run("batch: identical candidates stay duplicates", func(t *testing.T) {
		res := policy.Classify([]T{row(1, 90, 0)}, []T{s.knob(90, 0), s.knob(90, 0)})
		if len(res.AlreadyPresent) != 2 || len(res.Inserts) != 0 {
			t.Fatalf("got %+v", res)
		}
	})

	for _, order := range []struct {
		name  string
		knobs []int
	}{{"update first", []int{80, 90}}, {"duplicate first", []int{90, 80}}} {
		t.Run("batch: duplicate pins its row ("+order.name+")", func(t *testing.T) {
			mock := NewMockTrackingStore(s.get, s.set)
			a := s.knob(90, 0)
			aUID, _ := mock.Insert(&a)
			existing, _ := mock.SelectByIDProfile("u", 0)
			var cands []T
			for _, k := range order.knobs {
				cands = append(cands, s.knob(k, 0))
			}
			res, err := ApplyDiff[T](mock, "u", existing, cands, s.get, s.set)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.AlreadyPresent) != 1 || s.val(&res.AlreadyPresent[0]) != 90 {
				t.Fatalf("AlreadyPresent = %+v", res.AlreadyPresent)
			}
			if len(res.Updates) != 0 || len(res.Inserts) != 1 || s.val(&res.Inserts[0]) != 80 {
				t.Fatalf("got %+v", res)
			}
			var found bool
			for _, r := range mock.AllRows() {
				if s.get(&r) == aUID && s.val(&r) == 90 {
					found = true
				}
			}
			if !found {
				t.Fatalf("row %d with knob 90 was lost: %+v", aUID, mock.AllRows())
			}
			if len(mock.AllRows()) != 2 {
				t.Fatalf("rows = %+v", mock.AllRows())
			}
		})
	}

	t.Run("exact duplicate of a later row is already present, not an update of an earlier row", func(t *testing.T) {
		mock := NewMockTrackingStore(s.get, s.set)
		a := s.knob(80, 0)
		aUID, _ := mock.Insert(&a)
		b := s.knob(90, 0)
		bUID, _ := mock.Insert(&b)
		existing, _ := mock.SelectByIDProfile("u", 0)
		res, err := ApplyDiff[T](mock, "u", existing, []T{s.knob(90, 0)}, s.get, s.set)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Updates) != 0 || len(res.Inserts) != 0 || len(res.AlreadyPresent) != 1 {
			t.Fatalf("got %+v", res)
		}
		if got := s.get(&res.AlreadyPresent[0]); got != bUID {
			t.Fatalf("AlreadyPresent uid = %d, want %d", got, bUID)
		}
		rows := mock.AllRows()
		if len(rows) != 2 {
			t.Fatalf("rows = %+v", rows)
		}
		for _, r := range rows {
			want := map[int64]int{aUID: 80, bUID: 90}[s.get(&r)]
			if s.val(&r) != want {
				t.Fatalf("row %d knob = %d, want %d: %+v", s.get(&r), s.val(&r), want, rows)
			}
		}
	})

	t.Run("conflicts: exact duplicate of another rule", func(t *testing.T) {
		existing := []T{row(1, 90, 0), row(2, 70, 0)}
		edited := s.knob(70, 0)
		if uid, ok := policy.Conflicts(existing, &edited, 1); !ok || uid != 2 {
			t.Fatalf("got (%d,%v)", uid, ok)
		}
	})

	t.Run("conflicts: one updatable field away from another rule", func(t *testing.T) {
		existing := []T{row(1, 90, 0), row(2, 70, 0)}
		edited := s.knob(60, 0)
		if uid, ok := policy.Conflicts(existing, &edited, 1); !ok || uid != 2 {
			t.Fatalf("got (%d,%v)", uid, ok)
		}
	})

	t.Run("conflicts: only itself", func(t *testing.T) {
		existing := []T{row(1, 90, 0)}
		edited := s.knob(90, 0)
		if uid, ok := policy.Conflicts(existing, &edited, 1); ok || uid != 0 {
			t.Fatalf("got (%d,%v)", uid, ok)
		}
	})
}

func TestSquashMaxbattleDistanceUpdates(t *testing.T) {
	existing := []db.MaxbattleTrackingAPI{{UID: 1, ID: "u", PokemonID: 6, Level: 5, Distance: 1000}}
	res := DiffAndClassify(existing, []db.MaxbattleTrackingAPI{{ID: "u", PokemonID: 6, Level: 5, Distance: 500}},
		MaxbattleGetUID, MaxbattleSetUID)
	if len(res.Updates) != 1 || len(res.Inserts) != 0 {
		t.Fatalf("got %+v", res)
	}
	if res.Updates[0].UID != 1 || res.Updates[0].Distance != 500 {
		t.Fatalf("update = %+v", res.Updates[0])
	}
}

// Tappable mirrors quest: reward_type and reward are match keys (a different
// reward is a separate rule), amount is a regular field (a different minimum
// is a separate rule), and distance is updatable.
func TestSquashTappableKeys(t *testing.T) {
	existing := []db.TappableTrackingAPI{{UID: 1, ID: "u", RewardType: 4, Reward: 41, Amount: 3, Distance: 1000}}
	classify := func(c db.TappableTrackingAPI) DiffResult[db.TappableTrackingAPI] {
		return DiffAndClassify(existing, []db.TappableTrackingAPI{c}, TappableGetUID, TappableSetUID)
	}

	res := classify(db.TappableTrackingAPI{ID: "u", RewardType: 4, Reward: 41, Amount: 3, Distance: 500})
	if len(res.Updates) != 1 || len(res.Inserts) != 0 || res.Updates[0].UID != 1 || res.Updates[0].Distance != 500 {
		t.Fatalf("distance change: got %+v", res)
	}
	for name, c := range map[string]db.TappableTrackingAPI{
		"different reward":      {ID: "u", RewardType: 4, Reward: 25, Amount: 3, Distance: 1000},
		"different reward type": {ID: "u", RewardType: 9, Reward: 41, Amount: 3, Distance: 1000},
		"different amount":      {ID: "u", RewardType: 4, Reward: 41, Amount: 5, Distance: 1000},
	} {
		res := classify(c)
		if len(res.Inserts) != 1 || len(res.Updates) != 0 || len(res.AlreadyPresent) != 0 {
			t.Errorf("%s: got %+v", name, res)
		}
	}
}
