package discordbot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newTestReconciliation returns a Reconciliation wired to the given store and
// config, with nil session and nil dtsStore.  It is safe to call whenever
// no Discord API calls will actually be issued (e.g. no guilds configured).
func newTestReconciliation(hs store.HumanStore, cfg *config.Config) *Reconciliation {
	return NewReconciliation(nil, hs, cfg, nil, nil, nil)
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

// Reconciliation had no state manager and so could not see userSelectable or
// exemptFromAreaSecurity (#243). fences() is its only window onto them.
func TestReconciliation_FencesFromStateManager(t *testing.T) {
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: []geofence.Fence{{Name: "alpha"}}})
	r := NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, mgr)
	if got := r.fences(); len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("fences() = %v, want [alpha]", got)
	}
}

func TestReconciliation_FencesNilSafe(t *testing.T) {
	r := NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, nil)
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with nil manager = %v, want nil", got)
	}
	r = NewReconciliation(nil, store.NewMockHumanStore(), minimalConfig(), nil, nil, state.NewManager())
	if got := r.fences(); got != nil {
		t.Fatalf("fences() with empty manager = %v, want nil", got)
	}
}

func TestReconcileUser_FeatureRoleLossAndRestoration(t *testing.T) {
	for _, registrationRole := range []string{"", "member"} {
		t.Run("registrationRole="+registrationRole, func(t *testing.T) {
			cfg := minimalConfig()
			if registrationRole != "" {
				cfg.Discord.UserRole = []string{registrationRole}
			}
			cfg.Discord.CommandSecurity = map[string][]string{
				"pvp": {"supporter"}, "personal": {"supporter"},
			}
			hs := store.NewMockHumanStore()
			hs.AddHuman(&store.Human{ID: "user1", Type: "discord:user", Enabled: true})
			r := newTestReconciliation(hs, cfg)
			for _, roles := range [][]string{{"member", "supporter"}, nil, {"member", "supporter"}} {
				user, err := hs.Get("user1")
				if err != nil {
					t.Fatal(err)
				}
				r.ReconcileUser(user.ID, user, &DiscordUserInfo{Roles: roles}, false, false)
				got, err := hs.Get(user.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"pvp", "personal"} {
					if slices.Contains(got.BlockedAlerts, key) != (len(roles) == 0) {
						t.Errorf("roles=%v: blocked=%v, feature=%s", roles, got.BlockedAlerts, key)
					}
				}
				if !got.Enabled || got.AdminDisable {
					t.Fatal("feature role loss must not disable ordinary tracking")
				}
			}
		})
	}
}
