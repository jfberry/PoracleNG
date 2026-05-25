package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
)

// testRowTextGenerator builds a minimal Generator with embedded English translations.
func testRowTextGenerator(t *testing.T) *rowtext.Generator {
	t.Helper()
	bundle := i18n.Load("")
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0, Types: []int{13}},
		},
		Moves: map[int]*gamedata.Move{},
		Items: map[int]*gamedata.Item{},
		Util:  &gamedata.UtilData{},
	}
	return &rowtext.Generator{
		GD:                  gd,
		Translations:        bundle,
		DefaultTemplateName: "1",
	}
}

// TestRowText_MonsterShowsOverridesViaConverter ensures that when a MonsterTrackingAPI
// with override fields set is passed through monsterAPIToTracking, the resulting
// MonsterTracking carries those fields through to rowtext output.
// This test would have caught the bug where monsterAPIToTracking dropped override fields.
func TestRowText_MonsterShowsOverridesViaConverter(t *testing.T) {
	g := testRowTextGenerator(t)
	tr := g.Translations.For("en")

	api := &db.MonsterTrackingAPI{
		ID:                    "discord:user:1",
		ProfileNo:             1,
		PokemonID:             25,
		MinIV:                 -1,
		MaxIV:                 100,
		MinCP:                 0,
		MaxCP:                 9000,
		MinLevel:              0,
		MaxLevel:              55,
		MaxATK:                15,
		MaxDEF:                15,
		MaxSTA:                15,
		MaxSize:               5,
		MaxRarity:             6,
		Distance:              500,
		Template:              "1",
		OverrideLocationLabel: "Home",
	}

	tracking := monsterAPIToTracking(api)

	if tracking.OverrideLocationLabel != "Home" {
		t.Fatalf("monsterAPIToTracking dropped OverrideLocationLabel: got %q, want %q",
			tracking.OverrideLocationLabel, "Home")
	}

	got := g.MonsterRowText(tr, tracking)
	if !strings.Contains(got, "@ Home") {
		t.Fatalf("rowtext missing '@ Home' after converter; got: %s", got)
	}
}

// TestConverters_AllTypesPreserveOverrides checks that every *APIToTracking converter
// copies OverrideLocationLabel and OverrideAreas to the output struct.
func TestConverters_AllTypesPreserveOverrides(t *testing.T) {
	label := "Work"
	areas := []string{"london", "berlin"}

	t.Run("monster", func(t *testing.T) {
		api := &db.MonsterTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := monsterAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("monster: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("egg", func(t *testing.T) {
		api := &db.EggTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := eggAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("egg: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("raid", func(t *testing.T) {
		api := &db.RaidTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := raidAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("raid: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("quest", func(t *testing.T) {
		api := &db.QuestTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := questAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("quest: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("invasion", func(t *testing.T) {
		api := &db.InvasionTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := invasionAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("invasion: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("lure", func(t *testing.T) {
		api := &db.LureTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := lureAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("lure: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("gym", func(t *testing.T) {
		api := &db.GymTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := gymAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("gym: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("nest", func(t *testing.T) {
		api := &db.NestTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := nestAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("nest: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("fort", func(t *testing.T) {
		api := &db.FortTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := fortAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("fort: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
	t.Run("maxbattle", func(t *testing.T) {
		api := &db.MaxbattleTrackingAPI{OverrideLocationLabel: label, OverrideAreas: areas}
		out := maxbattleAPIToTracking(api)
		if out.OverrideLocationLabel != label || !slicesEqual(out.OverrideAreas, areas) {
			t.Errorf("maxbattle: overrides not preserved; label=%q areas=%v", out.OverrideLocationLabel, out.OverrideAreas)
		}
	})
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
