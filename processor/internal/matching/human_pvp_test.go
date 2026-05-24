package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// TestValidateHumans_PVPBlockedAlert pins the parity fix with PoracleJS's
// monster.js:102 — when a tracking rule has a PVP filter (league != 0)
// and the user has "pvp" in blocked_alerts, the rule must NOT fire.
//
// blocked_alerts gets the "pvp" entry from reconciliation when the user
// lacks the role configured under [discord.command_security] pvp, so this
// is the delivery-time gate that backstops the command-time
// CheckFeaturePermission denial — if a user authored a PVP rule before
// losing their role, the rule stops delivering.
func TestValidateHumans_PVPBlockedAlert(t *testing.T) {
	// A user with "pvp" blocked, in an area that matches.
	human := &db.Human{
		ID:               "u1",
		Enabled:          true,
		Area:             []string{"london"},
		BlockedAlertsSet: map[string]bool{"pvp": true},
	}
	humans := map[string]*db.Human{"u1": human}
	areas := map[string]bool{"london": true}

	// Two rules: a basic one (no PVP filter) and a PVP-filtered one.
	rules := []*db.MonsterTracking{
		{ID: "u1", UID: 1, PokemonID: 25, ProfileNo: 0}, // basic — should fire
		{ID: "u1", UID: 2, PokemonID: 25, ProfileNo: 0, PVPRankingLeague: 1500}, // PVP — should be dropped
	}

	out := ValidateHumans(rules, 51.5, -0.1, areas, false, humans)

	var sawBasic, sawPVP bool
	for _, m := range out {
		switch m.RuleUID {
		case 1:
			sawBasic = true
		case 2:
			sawPVP = true
		}
	}
	if !sawBasic {
		t.Errorf("basic rule (UID 1) should fire for a pvp-blocked user, got matches: %+v", out)
	}
	if sawPVP {
		t.Errorf("PVP-filtered rule (UID 2) should be dropped when human has pvp in blocked_alerts; got matches: %+v", out)
	}
}

// TestValidateHumans_PVPAllowedWhenNotBlocked verifies the negative
// path: a user WITHOUT "pvp" in blocked_alerts still receives PVP-
// filtered alerts.
func TestValidateHumans_PVPAllowedWhenNotBlocked(t *testing.T) {
	human := &db.Human{
		ID:               "u1",
		Enabled:          true,
		Area:             []string{"london"},
		BlockedAlertsSet: map[string]bool{}, // empty
	}
	humans := map[string]*db.Human{"u1": human}
	areas := map[string]bool{"london": true}

	rules := []*db.MonsterTracking{
		{ID: "u1", UID: 2, PokemonID: 25, ProfileNo: 0, PVPRankingLeague: 1500},
	}
	out := ValidateHumans(rules, 51.5, -0.1, areas, false, humans)
	if len(out) != 1 || out[0].RuleUID != 2 {
		t.Errorf("PVP rule should fire for unblocked user; got %+v", out)
	}
}
