package nlp

import (
	"strings"
	"testing"
)

func TestParseDistance(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1km", "d1000"},
		{"2.5km", "d2500"},
		{"500m", "d500"},
		{"1.5km", "d1500"},
		{"0.5km", "d500"},
		{"3m", "d3"},
		{"notadistance", ""},
		{"km", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDistance(tt.input)
			if got != tt.want {
				t.Errorf("parseDistance(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchFiltersSynonyms(t *testing.T) {
	tests := []struct {
		name    string
		tokens  []string
		intent  string
		wantAny string // at least one filter should contain this
	}{
		{"hundo track", []string{"hundo"}, "track", "iv100"},
		{"perfect track", []string{"perfect"}, "track", "iv100"},
		{"100% track", []string{"100%"}, "track", "iv100"},
		{"nundo track", []string{"nundo"}, "track", "iv0"},
		{"good ivs track", []string{"good", "ivs"}, "track", "iv80"},
		{"shadow track", []string{"shadow"}, "track", "form:shadow"},
		{"shadow raid", []string{"shadow"}, "raid", "level15"},
		{"legendary raid", []string{"legendary"}, "raid", "level5"},
		{"mega raid", []string{"mega"}, "raid", "level6"},
		{"nearby any", []string{"nearby"}, "track", "d1000"},
		{"xxl track", []string{"xxl"}, "track", "size:xxl"},
		{"shiny quest", []string{"shiny"}, "quest", "shiny"},
		{"stardust quest", []string{"stardust"}, "quest", "stardust"},
		{"energy quest", []string{"energy"}, "quest", "energy"},
		{"candy quest", []string{"candy"}, "quest", "candy"},
		{"new fort", []string{"new"}, "fort", "new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := matchFilters(tt.tokens, tt.intent)
			found := false
			for _, f := range r.Filters {
				if f == tt.wantAny {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("matchFilters(%v, %q) filters=%v, want to contain %q", tt.tokens, tt.intent, r.Filters, tt.wantAny)
			}
		})
	}
}

func TestMatchFiltersEverything(t *testing.T) {
	r := matchFilters([]string{"everything"}, "track")
	if !r.Everything {
		t.Error("expected Everything=true for 'everything'")
	}
	r = matchFilters([]string{"all"}, "track")
	if !r.Everything {
		t.Error("expected Everything=true for 'all'")
	}
}

func TestMatchFiltersTeams(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"mystic", "mystic"},
		{"blue", "mystic"},
		{"valor", "valor"},
		{"red", "valor"},
		{"instinct", "instinct"},
		{"yellow", "instinct"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			r := matchFilters([]string{tt.input}, "gym")
			if len(r.Filters) == 0 || r.Filters[0] != tt.want {
				t.Errorf("matchFilters([%q], gym) = %v, want [%q]", tt.input, r.Filters, tt.want)
			}
		})
	}
}

func TestMatchFiltersPVP(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{"pvp alone", []string{"pvp"}, "great5"},
		{"good pvp", []string{"good", "pvp"}, "great5"},
		{"great league", []string{"great", "league"}, "great5"},
		{"ultra league rank 1", []string{"ultra", "league", "rank", "1"}, "ultra1"},
		{"rank 1 great league", []string{"rank", "1", "great", "league"}, "great1"},
		{"rank 10", []string{"rank", "10"}, "great10"},
		{"top 5", []string{"top", "5"}, "great5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := matchFilters(tt.tokens, "track")
			found := false
			for _, f := range r.Filters {
				if f == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("matchFilters(%v, track) = %v, want to contain %q", tt.tokens, r.Filters, tt.want)
			}
		})
	}
}

func TestMatchFiltersBetween(t *testing.T) {
	r := matchFilters([]string{"between", "95", "99", "iv"}, "track")
	found := false
	for _, f := range r.Filters {
		if f == "iv95-99" {
			found = true
		}
	}
	if !found {
		t.Errorf("matchFilters(between 95 99 iv) = %v, want iv95-99", r.Filters)
	}
}

func TestMatchFiltersPassthrough(t *testing.T) {
	passthroughs := []string{"iv100", "cp2500", "level35", "d500", "gen1", "great1", "ultra5", "clean", "template:2"}
	for _, pt := range passthroughs {
		t.Run(pt, func(t *testing.T) {
			r := matchFilters([]string{pt}, "track")
			if len(r.Filters) == 0 || !strings.Contains(strings.Join(r.Filters, " "), pt) {
				t.Errorf("expected passthrough for %q, got %v", pt, r.Filters)
			}
		})
	}
}

func TestMatchFiltersShinyIgnoredInTrack(t *testing.T) {
	r := matchFilters([]string{"shiny"}, "track")
	// "shiny" in track context should be consumed but produce no filter
	if !r.Consumed[0] {
		t.Error("shiny should be consumed in track context")
	}
	for _, f := range r.Filters {
		if f == "shiny" {
			t.Error("shiny should be ignored in track context but was added to filters")
		}
	}
}
