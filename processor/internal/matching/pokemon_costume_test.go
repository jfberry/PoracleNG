package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/pvp"
)

// pvpZero returns a zero-value pvp.LeagueRank for tests that need to call
// matchMonsters with league=0 (non-PVP matching path).
func pvpZero() pvp.LeagueRank {
	return pvp.LeagueRank{}
}

func TestMatchMonsters_Costume(t *testing.T) {
	m := &PokemonMatcher{}
	// Encountered: true avoids the unrelated "unencountered pokemon skip
	// encounter-only stat filters" path (see matchMonsters), which would
	// otherwise skip these zero-valued MonsterTracking fixtures (MaxCP=0 <
	// defaultMaxCP, etc. reads as "constrained") before the costume check
	// under test ever runs.
	data := &ProcessedPokemon{PokemonID: 25, Form: 598, Costume: 1, Encountered: true}
	mk := func(costume int) []*db.MonsterTracking {
		return []*db.MonsterTracking{{ID: "u1", PokemonID: 25, Form: 0, Costume: costume}}
	}
	if got := m.matchMonsters(data, mk(9000), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 9000 (any) should match")
	}
	if got := m.matchMonsters(data, mk(1), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 1 should match costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(2), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 2 should NOT match costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(0), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 0 (no costume) should NOT match costumed spawn")
	}
}

// TestMatchMonsters_CostumeUnencountered locks the headline use case (design
// D1): costume is present on wild/unencountered sightings, so a costume:N rule
// with otherwise-default stats must still filter an unencountered costumed
// spawn — the costume check sits before the unencountered stat-skip. The
// fixture uses non-constraining stat defaults so that skip is a no-op and only
// the costume filter decides the match.
func TestMatchMonsters_CostumeUnencountered(t *testing.T) {
	m := &PokemonMatcher{}
	data := &ProcessedPokemon{PokemonID: 25, Form: 598, Costume: 1, Encountered: false, IV: -1}
	mk := func(costume int) []*db.MonsterTracking {
		return []*db.MonsterTracking{{
			ID: "u1", PokemonID: 25, Form: 0, Costume: costume,
			MinIV: -1, MaxIV: 100,
			MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: defaultMaxStat, MaxDEF: defaultMaxStat, MaxSTA: defaultMaxStat,
			MaxWeight: defaultMaxWeight,
		}}
	}
	if got := m.matchMonsters(data, mk(9000), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 9000 (any) should match unencountered costumed spawn")
	}
	if got := m.matchMonsters(data, mk(1), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 1 should match unencountered costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(2), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 2 should NOT match unencountered costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(0), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 0 (no costume) should NOT match unencountered costumed spawn")
	}
}
