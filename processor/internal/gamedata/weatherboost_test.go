package gamedata

import (
	"slices"
	"testing"
)

func TestGetBoostingWeathers(t *testing.T) {
	gd := loadTestGameData(t)

	tests := []struct {
		name     string
		typeIDs  []int
		wantAny  []int // at least one of these should be present
	}{
		{
			name:    "Fire type (10) boosted by Clear (1)",
			typeIDs: []int{10},
			wantAny: []int{1},
		},
		{
			name:    "Water type (11) boosted by Rain (2)",
			typeIDs: []int{11},
			wantAny: []int{2},
		},
		{
			name:    "Dual Fire/Flying (10,3) boosted by Clear (1) and Windy (5)",
			typeIDs: []int{10, 3},
			wantAny: []int{1, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weathers := gd.GetBoostingWeathers(tt.typeIDs)
			for _, w := range tt.wantAny {
				if !slices.Contains(weathers, w) {
					t.Errorf("expected weather %d in boosting weathers %v", w, weathers)
				}
			}
		})
	}
}

func TestGetAlteringWeathers(t *testing.T) {
	gd := loadTestGameData(t)

	// Fire type, currently boosted (weather 1): altering = non-boosting weathers
	altering := gd.GetAlteringWeathers([]int{10}, 1)
	if slices.Contains(altering, 1) {
		t.Error("Clear (1) should not be in altering weathers when Fire is boosted by Clear")
	}
	if len(altering) == 0 {
		t.Error("should have some altering weathers")
	}

	// Fire type, not boosted: altering = boosting weathers
	altering = gd.GetAlteringWeathers([]int{10}, 0)
	if !slices.Contains(altering, 1) {
		t.Error("Clear (1) should be in altering weathers when Fire is not boosted")
	}
}

func TestIsBoostedByWeather(t *testing.T) {
	gd := loadTestGameData(t)

	// Fire (10) in Clear (1) = boosted
	if !gd.IsBoostedByWeather([]int{10}, 1) {
		t.Error("Fire should be boosted by Clear")
	}

	// Fire (10) in Rain (2) = not boosted
	if gd.IsBoostedByWeather([]int{10}, 2) {
		t.Error("Fire should not be boosted by Rain")
	}

	// Weather 0 = never boosted
	if gd.IsBoostedByWeather([]int{10}, 0) {
		t.Error("no type should be boosted by weather 0")
	}
}

func TestFindIvColor(t *testing.T) {
	colors := []string{"#gray", "#white", "#green", "#blue", "#purple", "#orange"}

	tests := []struct {
		iv   float64
		want string
	}{
		{0, "#gray"},
		{24.9, "#gray"},
		{25, "#white"},
		{49.9, "#white"},
		{50, "#green"},
		{81.9, "#green"},
		{82, "#blue"},
		{89.9, "#blue"},
		{90, "#purple"},
		{99.9, "#purple"},
		{100, "#orange"},
	}

	for _, tt := range tests {
		got := FindIvColor(tt.iv, colors)
		if got != tt.want {
			t.Errorf("FindIvColor(%.1f) = %q, want %q", tt.iv, got, tt.want)
		}
	}

	// Not enough colors returns empty
	got := FindIvColor(50, []string{"a", "b"})
	if got != "" {
		t.Errorf("FindIvColor with too few colors = %q, want empty", got)
	}
}
