package db

// GetID methods for all tracking structs — used by filterSlice in loader.go
// to remove orphaned tracking rows (no matching human).

func (t *RaidTracking) GetID() string      { return t.ID }
func (t *EggTracking) GetID() string       { return t.ID }
func (t *InvasionTracking) GetID() string  { return t.ID }
func (t *QuestTracking) GetID() string     { return t.ID }
func (t *LureTracking) GetID() string      { return t.ID }
func (t *GymTracking) GetID() string       { return t.ID }
func (t *NestTracking) GetID() string      { return t.ID }
func (t *FortTracking) GetID() string      { return t.ID }
func (t *MaxbattleTracking) GetID() string { return t.ID }

// FilterOrphans removes monster tracking entries whose ID is not in the humans map.
func (idx *MonsterIndex) FilterOrphans(humans map[string]*Human) {
	if idx == nil {
		return
	}
	for pokemonID, entries := range idx.ByPokemonID {
		idx.ByPokemonID[pokemonID] = filterMonsters(entries, humans)
	}
	for league, entries := range idx.PVPSpecific {
		idx.PVPSpecific[league] = filterMonsters(entries, humans)
	}
	for league, entries := range idx.PVPEverything {
		idx.PVPEverything[league] = filterMonsters(entries, humans)
	}
	// Recount total
	total := 0
	for _, entries := range idx.ByPokemonID {
		total += len(entries)
	}
	for _, entries := range idx.PVPSpecific {
		total += len(entries)
	}
	for _, entries := range idx.PVPEverything {
		total += len(entries)
	}
	idx.Total = total
}

func filterMonsters(items []*MonsterTracking, humans map[string]*Human) []*MonsterTracking {
	n := 0
	for _, item := range items {
		if _, ok := humans[item.ID]; ok {
			items[n] = item
			n++
		}
	}
	return items[:n]
}
