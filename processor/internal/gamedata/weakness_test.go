package gamedata

import (
	"slices"
	"testing"
)

func TestCalculateWeaknesses(t *testing.T) {
	gd := loadTestGameData(t)

	tests := []struct {
		name           string
		pokemonID      int
		form           int
		wantExtraWeak  []int // 4x type IDs
		wantWeak       []int // 2x type IDs
		wantResist     []int // 0.5x type IDs
	}{
		{
			name:      "Charizard (Fire/Flying)",
			pokemonID: 6,
			form:      0,
			// Fire(10)/Flying(3):
			// 4x weak to Rock(6)
			// 2x weak to Water(11), Electric(13)
			// 0.5x resist: Fire(10), Fighting(2), Steel(9), Fairy(18)
			// 0.25x resist: Grass(12), Bug(7)
			// 0.25x immune: Ground(5) (Flying immunity in PoGo = 0.25x)
			wantExtraWeak: []int{6}, // Rock
			wantWeak:      []int{11, 13}, // Water, Electric
		},
		{
			name:      "Pikachu (Electric)",
			pokemonID: 25,
			form:      0,
			// Electric(13):
			// 2x weak to Ground(5)
			// 0.5x resist: Electric(13), Flying(3), Steel(9)
			wantWeak: []int{5}, // Ground
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := gd.GetMonster(tt.pokemonID, tt.form)
			if m == nil {
				t.Fatalf("Monster %d form %d not found", tt.pokemonID, tt.form)
			}

			categories := CalculateWeaknesses(m.Types, gd.Types)

			// Check 4x weaknesses
			if len(tt.wantExtraWeak) > 0 {
				found := false
				for _, cat := range categories {
					if cat.Multiplier == 4 {
						found = true
						for _, wantID := range tt.wantExtraWeak {
							if !slices.Contains(cat.TypeIDs, wantID) {
								t.Errorf("4x weakness: type %d not found in %v", wantID, cat.TypeIDs)
							}
						}
					}
				}
				if !found {
					t.Errorf("expected 4x weakness category but not found")
				}
			}

			// Check 2x weaknesses
			if len(tt.wantWeak) > 0 {
				found := false
				for _, cat := range categories {
					if cat.Multiplier == 2 {
						found = true
						for _, wantID := range tt.wantWeak {
							if !slices.Contains(cat.TypeIDs, wantID) {
								t.Errorf("2x weakness: type %d not found in %v", wantID, cat.TypeIDs)
							}
						}
					}
				}
				if !found {
					t.Errorf("expected 2x weakness category but not found")
				}
			}
		})
	}
}

func TestCalculateWeaknessesEmpty(t *testing.T) {
	gd := loadTestGameData(t)

	result := CalculateWeaknesses(nil, gd.Types)
	if result != nil {
		t.Errorf("expected nil for empty types, got %v", result)
	}

	result = CalculateWeaknesses([]int{}, gd.Types)
	if result != nil {
		t.Errorf("expected nil for zero-length types, got %v", result)
	}
}
