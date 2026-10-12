package db

import (
	"slices"
	"testing"
	"time"
)

func TestGroupPlayerAccounts(t *testing.T) {
	t0 := time.Unix(1000, 0)
	rows := []PlayerAccount{
		{UID: 3, ID: "u1", PlayerID: "b", CreatedAt: t0},
		{UID: 1, ID: "u1", PlayerID: "a", CreatedAt: t0},
		{UID: 2, ID: "u2", PlayerID: "c", CreatedAt: t0},
	}
	got := GroupPlayerAccounts(rows)
	if len(got["u1"]) != 2 || got["u1"][0].PlayerID != "a" || got["u1"][1].PlayerID != "b" {
		t.Fatalf("u1 = %+v (want oldest first, uid tiebreak)", got["u1"])
	}
	if len(got["u2"]) != 1 {
		t.Fatalf("u2 = %+v", got["u2"])
	}
}

func TestPlayerAccountsCascadeOnDelete(t *testing.T) {
	if !slices.Contains(humanOwnedTables, "player_accounts") {
		t.Fatal("player_accounts not deleted with the human")
	}
}

func TestPersonalHelpers(t *testing.T) {
	if PackPersonal(true, true) != 3 || PackPersonal(false, true) != 2 || PackPersonal(false, false) != 0 {
		t.Fatal("PackPersonal")
	}
	if !IsPersonalShiny(3) || IsPersonalShiny(2) || !IsPersonalBackground(2) || IsPersonalBackground(1) {
		t.Fatal("Is* helpers")
	}
}
