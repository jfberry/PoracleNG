package state

import "github.com/pokemon/poracleng/processor/internal/db"

// PerHumanMaxDistance walks every rule across every tracking type and
// returns the maximum non-zero Distance per human ID. Humans with only
// distance==0 (area-based) rules are absent from the map; the geo index
// builder then leaves them out of the distance r-tree.
//
// Distance is per-RULE in the database, but the geo index needs a per-
// HUMAN circle. We take the max so the index circle covers every rule
// the human owns — a strict superset of what each rule would individually
// match. The matcher's existing ValidateHumans* step still applies the
// exact per-rule distance check, so false positives here are harmless.
func PerHumanMaxDistance(data *db.AllData) map[string]int {
	out := map[string]int{}
	record := func(id string, d int) {
		if d <= 0 || id == "" {
			return
		}
		if prev, ok := out[id]; !ok || d > prev {
			out[id] = d
		}
	}
	if data == nil {
		return out
	}
	if data.Monsters != nil {
		for _, slice := range data.Monsters.ByPokemonID {
			for _, m := range slice {
				record(m.ID, m.Distance)
			}
		}
		for _, slice := range data.Monsters.PVPSpecific {
			for _, m := range slice {
				record(m.ID, m.Distance)
			}
		}
		for _, slice := range data.Monsters.PVPEverything {
			for _, m := range slice {
				record(m.ID, m.Distance)
			}
		}
	}
	for _, r := range data.Raids {
		record(r.ID, r.Distance)
	}
	for _, e := range data.Eggs {
		record(e.ID, e.Distance)
	}
	for _, i := range data.Invasions {
		record(i.ID, i.Distance)
	}
	for _, q := range data.Quests {
		record(q.ID, q.Distance)
	}
	for _, l := range data.Lures {
		record(l.ID, l.Distance)
	}
	for _, n := range data.Nests {
		record(n.ID, n.Distance)
	}
	for _, g := range data.Gyms {
		record(g.ID, g.Distance)
	}
	for _, f := range data.Forts {
		record(f.ID, f.Distance)
	}
	for _, mb := range data.Maxbattles {
		record(mb.ID, mb.Distance)
	}
	return out
}
