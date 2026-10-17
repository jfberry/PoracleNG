package discordbot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordroles"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newTestReconciliation returns a Reconciliation wired to the given store and
// config, with nil session and nil dtsStore.  It is safe to call whenever
// no Discord API calls will actually be issued (e.g. no guilds configured).
func newTestReconciliation(hs store.HumanStore, cfg *config.Config) *Reconciliation {
	return NewReconciliation(nil, hs, cfg, nil, nil)
}

// minimalConfig returns a Config where reconciliation is set up but has no
// guilds and no user_role, so no Discord API calls are ever made.
func minimalConfig() *config.Config {
	return &config.Config{
		// No guilds → ReconcileSingleUser iterates zero guilds, makes no API calls.
		Discord: config.DiscordConfig{
			CheckRole: true,
			Guilds:    nil,
			UserRole:  nil,
		},
		// Area security disabled → reconcileNonAreaSecurity path.
		// reconcileNonAreaSecurity is a no-op when UserRole is empty.
		Area: config.AreaConfig{
			Enabled: false,
		},
	}
}

// ---------------------------------------------------------------------------
// ReconcileUserNow on Bot
// ---------------------------------------------------------------------------

// TestReconcileUserNow_Disabled confirms that the public wrapper returns
// ErrReconciliationDisabled when the Bot was created without reconciliation.
func TestReconcileUserNow_Disabled(t *testing.T) {
	b := &Bot{
		Cfg:            minimalConfig(),
		reconciliation: nil, // explicitly nil
	}

	err := b.ReconcileUserNow("123456789")
	if err != ErrReconciliationDisabled {
		t.Fatalf("expected ErrReconciliationDisabled, got %v", err)
	}
}

// TestReconcileUserNow_Routes confirms that ReconcileUserNow returns nil and
// reaches ReconcileSingleUser when reconciliation is configured.
// We verify indirectly: the mock store records the "Get" call that
// ReconcileSingleUser issues after collecting guild roles (none, no guilds).
func TestReconcileUserNow_Routes(t *testing.T) {
	cfg := minimalConfig()
	ms := store.NewMockHumanStore()

	b := &Bot{
		Cfg:            cfg,
		reconciliation: newTestReconciliation(ms, cfg),
	}

	err := b.ReconcileUserNow("999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ReconcileSingleUser calls humanStore.Get(id) after iterating guilds.
	if len(ms.Calls) == 0 {
		t.Fatal("expected at least one store call (Get), got none")
	}
	found := slices.Contains(ms.Calls, "Get")
	if !found {
		t.Fatalf("expected 'Get' call in store.Calls, got %v", ms.Calls)
	}
}

// ---------------------------------------------------------------------------
// ReconcileSingleUser on Reconciliation
// ---------------------------------------------------------------------------

// TestReconcileSingleUser_UnknownUser calls ReconcileSingleUser for a user
// that is not in any guild (no guilds configured) and not in the DB.
// With no user_role set, reconcileNonAreaSecurity is a no-op — the function
// should complete without panic and without modifying the store.
func TestReconcileSingleUser_UnknownUser(t *testing.T) {
	cfg := minimalConfig()
	ms := store.NewMockHumanStore()
	r := newTestReconciliation(ms, cfg)

	// Must not panic.
	r.ReconcileSingleUser("unknown-user-id", false)

	// Get was called to look up the human record.
	gotGet := false
	for _, c := range ms.Calls {
		if c == "Get" {
			gotGet = true
		}
	}
	if !gotGet {
		t.Fatalf("expected 'Get' store call, got %v", ms.Calls)
	}

	// No create/update/delete calls — user doesn't qualify for any action.
	for _, c := range ms.Calls {
		if c == "Create" || c == "Update" || c == "SetAdminDisable" || c == "Delete" {
			t.Errorf("unexpected mutating store call: %s", c)
		}
	}
}

// TestReconcileSingleUser_KnownUser_StillActive verifies that an enabled user
// with no user_role requirement is neither disabled nor updated (no mutation).
func TestReconcileSingleUser_KnownUser_StillActive(t *testing.T) {
	cfg := minimalConfig()
	ms := store.NewMockHumanStore()
	ms.AddHuman(&store.Human{
		ID:           "user-active",
		Type:         "discord:user",
		Enabled:      true,
		AdminDisable: false,
		Name:         "Test User",
	})
	r := newTestReconciliation(ms, cfg)

	r.ReconcileSingleUser("user-active", false)

	for _, c := range ms.Calls {
		if c == "Create" || c == "Delete" || c == "SetAdminDisable" {
			t.Errorf("unexpected mutating store call: %s", c)
		}
	}
}

// TestReconcileUser_EveryoneCommunityRole covers #261: a community whose
// user_role is the guild ID (@everyone) must keep every guild member, even
// one with no explicit roles. Discord omits @everyone from member.Roles, so
// the role list has to come through discordroles.MemberRoleIDs.
func TestReconcileUser_EveryoneCommunityRole(t *testing.T) {
	const guildID = "1092147901393813588"
	cases := []struct {
		name        string
		roles       []string
		wantDisable bool
	}{
		{"member role list includes @everyone", discordroles.MemberRoleIDs(guildID, nil), false},
		{"raw member.Roles without @everyone", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comm := config.CommunityConfig{Name: "Vila_Velha", AllowedAreas: []string{"Vila Velha"}}
			comm.Discord.UserRole = []string{guildID}
			cfg := minimalConfig()
			cfg.General.RoleCheckMode = "disable-user"
			cfg.Area = config.AreaConfig{Enabled: true, Communities: []config.CommunityConfig{comm}}

			user := &store.Human{
				ID:                  "member-1",
				Type:                "discord:user",
				Enabled:             true,
				Name:                "Member",
				CommunityMembership: []string{"vila_velha"},
			}
			ms := store.NewMockHumanStore()
			ms.AddHuman(user)
			r := newTestReconciliation(ms, cfg)

			r.ReconcileUser(user.ID, user, &DiscordUserInfo{Name: "Member", Roles: tc.roles}, false, true)

			if got := slices.Contains(ms.Calls, "SetAdminDisable"); got != tc.wantDisable {
				t.Errorf("SetAdminDisable called=%v, want %v (calls %v)", got, tc.wantDisable, ms.Calls)
			}
		})
	}
}
