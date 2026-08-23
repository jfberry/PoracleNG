package api

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// TestV2Pokemon_CostumeWriteMapper locks the translateV2Pokemon write-side
// contract for the global costume sentinel: nil (omitted) -> stored 9000
// ("any costume"), ptr(0) -> stored 0 ("no costume"), ptr(5) -> stored 5
// (that specific costume). Mirrors the Form round-trip coverage in
// v2_pokemon_test.go / tracking_test.go's Costume idempotency regression.
func TestV2Pokemon_CostumeWriteMapper(t *testing.T) {
	cases := []struct {
		name    string
		costume *int
		want    int
	}{
		{"omitted -> any (9000)", nil, 9000},
		{"zero -> no costume (0)", ptr(0), 0},
		{"specific costume", ptr(5), 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &v2PokemonRule{PokemonID: 25, Costume: c.costume}
			row, err := translateV2Pokemon(&TrackingDeps{}, "u1", 1, overrideContext{}, req)
			if err != nil {
				t.Fatalf("translateV2Pokemon returned error: %v", err)
			}
			if row.Costume != c.want {
				t.Errorf("Costume = %d, want %d", row.Costume, c.want)
			}
		})
	}
}

// TestV2Pokemon_CostumeReadMapper locks the pokemonRowToRule read-side
// contract: stored 9000 ("any costume") projects to nil (hidden wildcard),
// stored 0 ("no costume") and any specific costume project to a non-nil
// pointer carrying the exact value.
func TestV2Pokemon_CostumeReadMapper(t *testing.T) {
	cases := []struct {
		name    string
		stored  int
		wantNil bool
		want    int
	}{
		{"any (9000) -> nil", 9000, true, 0},
		{"no costume (0) -> ptr(0)", 0, false, 0},
		{"specific costume -> ptr(5)", 5, false, 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := &db.MonsterTrackingAPI{PokemonID: 25, Costume: c.stored}
			rule := pokemonRowToRule(row)
			if c.wantNil {
				if rule.Costume != nil {
					t.Errorf("Costume = %v, want nil", *rule.Costume)
				}
				return
			}
			if rule.Costume == nil {
				t.Fatalf("Costume = nil, want %d", c.want)
			}
			if *rule.Costume != c.want {
				t.Errorf("Costume = %d, want %d", *rule.Costume, c.want)
			}
		})
	}
}
