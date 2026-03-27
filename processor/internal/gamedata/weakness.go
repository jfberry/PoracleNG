package gamedata

// WeaknessCategory represents a group of types at a specific damage multiplier.
type WeaknessCategory struct {
	Multiplier float64 // 4, 2, 0.5, 0.25, 0.125
	TypeIDs    []int   // type IDs at this multiplier
}

// CalculateWeaknesses computes the defensive weakness/resistance profile for a
// pokemon with the given defending type IDs. The result is categorized by effective
// multiplier across all defending types combined (dual type multiplication).
//
// Multiplier categories:
//   - 4x    (extraWeak)    — weak to both types
//   - 2x    (weak)         — weak to one type
//   - 0.5x  (resist)       — resisted by one type
//   - 0.25x (immune/doubleResist) — resisted by both, or immune to one
//   - 0.125x (extraImmune) — immune + resist
//
// This matches the alerter's weakness calculation in monster.js / raid.js.
func CalculateWeaknesses(defenseTypeIDs []int, types map[int]*TypeInfo) []WeaknessCategory {
	if len(defenseTypeIDs) == 0 {
		return nil
	}

	// Build multiplier map for each attacking type ID
	multipliers := make(map[int]float64)
	for _, defTypeID := range defenseTypeIDs {
		ti, ok := types[defTypeID]
		if !ok {
			continue
		}
		// Types that are super effective against this defending type: 2x
		for _, wID := range ti.Weaknesses {
			if _, exists := multipliers[wID]; !exists {
				multipliers[wID] = 1
			}
			multipliers[wID] *= 2
		}
		// Types this defending type resists: 0.5x
		for _, rID := range ti.Resistances {
			if _, exists := multipliers[rID]; !exists {
				multipliers[rID] = 1
			}
			multipliers[rID] *= 0.5
		}
		// Types this defending type is immune to: 0.25x (PoGo double resist)
		for _, iID := range ti.Immunes {
			if _, exists := multipliers[iID]; !exists {
				multipliers[iID] = 1
			}
			multipliers[iID] *= 0.25
		}
	}

	// Categorize into buckets
	buckets := map[float64][]int{}
	for typeID, mult := range multipliers {
		if mult == 1 {
			continue // neutral, skip
		}
		buckets[mult] = append(buckets[mult], typeID)
	}

	// Build result in order from most vulnerable to most resistant
	order := []float64{4, 2, 0.5, 0.25, 0.125}
	var result []WeaknessCategory
	for _, mult := range order {
		if ids, ok := buckets[mult]; ok && len(ids) > 0 {
			result = append(result, WeaknessCategory{
				Multiplier: mult,
				TypeIDs:    ids,
			})
		}
	}

	return result
}
