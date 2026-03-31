package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Discord: config.DiscordConfig{
			Admins: []string{"admin1", "admin2"},
			CommandSecurity: map[string][]string{
				"raid":    {"role_raiders", "user99"},
				"monster": {"role_trackers"},
			},
			DelegatedAdministration: config.DelegatedAdminConfig{
				ChannelTracking: map[string][]string{
					"channel123": {"user50", "role_mods"},
					"guild456":   {"role_admins"},
				},
				WebhookTracking: map[string][]string{
					"my-webhook": {"user50"},
				},
			},
		},
		Telegram: config.TelegramConfig{
			Admins: []string{"tg_admin1"},
		},
	}
}

func TestIsAdmin(t *testing.T) {
	cfg := testConfig()

	if !IsAdmin(cfg, "discord", "admin1") {
		t.Error("admin1 should be admin")
	}
	if IsAdmin(cfg, "discord", "nobody") {
		t.Error("nobody should not be admin")
	}
	if !IsAdmin(cfg, "telegram", "tg_admin1") {
		t.Error("tg_admin1 should be telegram admin")
	}
	if IsAdmin(cfg, "telegram", "admin1") {
		t.Error("admin1 is not a telegram admin")
	}
}

func TestCommandAllowed_NoRestriction(t *testing.T) {
	cfg := testConfig()
	// "gym" has no command_security entry → unrestricted
	if !CommandAllowed(cfg, "discord", "cmd.gym", "anyone", nil) {
		t.Error("gym should be unrestricted")
	}
}

func TestCommandAllowed_ByUserID(t *testing.T) {
	cfg := testConfig()
	// user99 is directly in the raid allowed list
	if !CommandAllowed(cfg, "discord", "cmd.raid", "user99", nil) {
		t.Error("user99 should be allowed for raid")
	}
}

func TestCommandAllowed_ByRole(t *testing.T) {
	cfg := testConfig()
	if !CommandAllowed(cfg, "discord", "cmd.raid", "user1", []string{"role_raiders"}) {
		t.Error("role_raiders should allow raid")
	}
}

func TestCommandAllowed_Denied(t *testing.T) {
	cfg := testConfig()
	if CommandAllowed(cfg, "discord", "cmd.raid", "user1", []string{"role_other"}) {
		t.Error("user1 with role_other should be denied raid")
	}
}

func TestCommandAllowed_TelegramAlwaysAllowed(t *testing.T) {
	cfg := testConfig()
	// Telegram has no command security
	if !CommandAllowed(cfg, "telegram", "cmd.raid", "anyone", nil) {
		t.Error("telegram should always allow commands")
	}
}

func TestCommandAllowed_TrackMapsToMonster(t *testing.T) {
	cfg := testConfig()
	// cmd.track maps to "monster" in command_security
	if !CommandAllowed(cfg, "discord", "cmd.track", "user1", []string{"role_trackers"}) {
		t.Error("role_trackers should allow track (maps to monster)")
	}
	if CommandAllowed(cfg, "discord", "cmd.track", "user1", []string{"role_other"}) {
		t.Error("role_other should not allow track")
	}
}

func TestCalculateChannelPermissions(t *testing.T) {
	cfg := testConfig()

	// user50 is directly allowed for channel123
	if !CalculateChannelPermissions(cfg, "user50", nil, "channel123", "", "") {
		t.Error("user50 should have channel123 permission")
	}

	// role_mods is allowed for channel123
	if !CalculateChannelPermissions(cfg, "user1", []string{"role_mods"}, "channel123", "", "") {
		t.Error("role_mods should have channel123 permission")
	}

	// role_admins is allowed for guild456
	if !CalculateChannelPermissions(cfg, "user1", []string{"role_admins"}, "anything", "guild456", "") {
		t.Error("role_admins should have guild456 permission")
	}

	// No permission
	if CalculateChannelPermissions(cfg, "user1", nil, "channel999", "guild999", "") {
		t.Error("user1 should not have permissions for unknown channel/guild")
	}
}

func TestCanAdminWebhook(t *testing.T) {
	cfg := testConfig()

	if !CanAdminWebhook(cfg, "user50", "my-webhook") {
		t.Error("user50 should admin my-webhook")
	}
	if CanAdminWebhook(cfg, "user1", "my-webhook") {
		t.Error("user1 should not admin my-webhook")
	}
	if CanAdminWebhook(cfg, "user50", "other-webhook") {
		t.Error("user50 should not admin other-webhook")
	}
}

func TestBlockedAlerts(t *testing.T) {
	cfg := testConfig()

	// User with role_raiders: raid is allowed, monster is blocked (needs role_trackers)
	blocked := BlockedAlerts(cfg, "user1", []string{"role_raiders"})
	hasRaid := false
	hasPokemon := false
	for _, b := range blocked {
		if b == "raid" {
			hasRaid = true
		}
		if b == "pokemon" {
			hasPokemon = true
		}
	}
	if hasRaid {
		t.Error("raid should not be blocked for role_raiders")
	}
	if !hasPokemon {
		t.Error("pokemon should be blocked (no role_trackers)")
	}

	// User with both roles: nothing blocked
	blocked = BlockedAlerts(cfg, "user1", []string{"role_raiders", "role_trackers"})
	if len(blocked) != 0 {
		t.Errorf("expected no blocked, got %v", blocked)
	}
}
