package store

import (
	"slices"
	"testing"
)

func TestPlayerAccountsCascadeOnDelete(t *testing.T) {
	if !slices.Contains(humanOwnedTables, "player_accounts") {
		t.Fatal("player_accounts not deleted with the human")
	}
	if slices.Contains(trackingTables, "player_accounts") {
		t.Fatal("player_accounts has no profile_no; must not be a tracking table")
	}
}
