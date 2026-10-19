package matching_test

import (
	"encoding/json"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/stretchr/testify/require"
)

// Exercise reconciliation's permission calculation and delivery's gate
// together: saved rules survive role loss but cannot deliver until restored.
func TestValidateHumans_FeatureRoleLifecycle(t *testing.T) {
	for _, feature := range []string{"pvp", "personalshiny", "personalbackground"} {
		t.Run(feature, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Discord.CommandSecurity = map[string][]string{feature: {"supporter"}}
			h := &db.Human{ID: "u1", Enabled: true, Area: []string{"london"}}
			rules := []*db.MonsterTracking{
				{ID: "u1", UID: 1, PokemonID: 25},
				{ID: "u1", UID: 2, PokemonID: 25, PVPRankingLeague: 1500},
				{ID: "u1", UID: 3, PokemonID: 25, Personal: db.PersonalShiny},
				{ID: "u1", UID: 4, PokemonID: 25, Personal: db.PersonalBackground},
				{ID: "u1", UID: 5, PokemonID: 25, Personal: db.PersonalKnownMask, PVPRankingLeague: 1500},
			}
			for _, hasRole := range []bool{true, false, true} {
				var roles []string
				if hasRole {
					roles = []string{"supporter"}
				}
				h.BlockedAlertsSet = make(map[string]bool)
				if encoded := bot.BlockedAlerts(cfg, h.ID, roles); encoded != nil {
					var blocked []string
					require.NoError(t, json.Unmarshal([]byte(*encoded), &blocked))
					for _, key := range blocked {
						h.BlockedAlertsSet[key] = true
					}
				}
				matches := matching.ValidateHumans(rules, 51.5, -0.1, map[string]bool{"london": true}, false, map[string]*db.Human{"u1": h})
				var ids []int64
				for _, match := range matches {
					ids = append(ids, match.RuleUID)
				}
				want := []int64{1, 2, 3, 4, 5}
				if !hasRole {
					switch feature {
					case "pvp":
						want = []int64{1, 3, 4}
					case "personalshiny":
						want = []int64{1, 2, 4}
					case "personalbackground":
						want = []int64{1, 2, 3}
					}
				}
				require.Equal(t, want, ids, "hasRole=%v", hasRole)
			}
		})
	}
}
