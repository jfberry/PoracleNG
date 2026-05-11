package state

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestPerHumanMaxDistance_AcrossAllRuleTypes(t *testing.T) {
	data := &db.AllData{
		Monsters: &db.MonsterIndex{
			ByPokemonID: map[int][]*db.MonsterTracking{
				25: {{ID: "u1", Distance: 5000}},
				0:  {{ID: "u1", Distance: 1000}, {ID: "u2", Distance: 2000}},
			},
			PVPSpecific:   make(map[int][]*db.MonsterTracking),
			PVPEverything: make(map[int][]*db.MonsterTracking),
		},
		Raids:      []*db.RaidTracking{{ID: "u1", Distance: 8000}},
		Eggs:       []*db.EggTracking{{ID: "u3", Distance: 500}},
		Invasions:  []*db.InvasionTracking{{ID: "u2", Distance: 12000}},
		Quests:     []*db.QuestTracking{{ID: "u4", Distance: 0}}, // area-only, contributes nothing
		Lures:      []*db.LureTracking{},
		Nests:      []*db.NestTracking{},
		Gyms:       []*db.GymTracking{{ID: "u1", Distance: 100}},
		Forts:      []*db.FortTracking{},
		Maxbattles: []*db.MaxbattleTracking{{ID: "u1", Distance: 6000}},
	}
	got := PerHumanMaxDistance(data)
	if got["u1"] != 8000 { // raid is max
		t.Errorf("u1 max = %d, want 8000", got["u1"])
	}
	if got["u2"] != 12000 { // invasion is max
		t.Errorf("u2 max = %d, want 12000", got["u2"])
	}
	if got["u3"] != 500 {
		t.Errorf("u3 max = %d, want 500", got["u3"])
	}
	if _, ok := got["u4"]; ok {
		t.Errorf("u4 has only distance==0 rules, should be absent")
	}
}
