package state

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestApplyAccountsToHumans(t *testing.T) {
	humans := map[string]*db.Human{"u1": {ID: "u1"}, "u2": {ID: "u2"}}
	applyAccounts(humans, map[string][]db.PlayerAccount{
		"u1":      {{ID: "u1", PlayerID: "abc"}},
		"missing": {{ID: "missing", PlayerID: "zzz"}},
	})
	if len(humans["u1"].Accounts) != 1 || humans["u2"].Accounts != nil {
		t.Fatalf("u1=%v u2=%v", humans["u1"].Accounts, humans["u2"].Accounts)
	}
}
