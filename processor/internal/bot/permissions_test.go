package bot

import (
	"strings"
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
	if blocked == nil {
		t.Fatal("expected non-nil blocked for user with partial roles")
	}
	blockedStr := *blocked
	if strings.Contains(blockedStr, "raid") {
		t.Error("raid should not be blocked for role_raiders")
	}
	if !strings.Contains(blockedStr, "monster") {
		t.Error("monster should be blocked (no role_trackers)")
	}

	// User with both roles: nothing blocked
	blocked = BlockedAlerts(cfg, "user1", []string{"role_raiders", "role_trackers"})
	if blocked != nil {
		t.Errorf("expected nil blocked, got %v", *blocked)
	}
}

// Admins bypass command_security on every surface. The slash dispatcher
// already did this; text commands and /api/command didn't, so an admin not
// in the allow list was refused the text form of a command they could run
// as a slash command (#287).
func TestCommandAllowed_AdminBypasses(t *testing.T) {
	cfg := testConfig()
	if !CommandAllowed(cfg, "discord", "cmd.raid", "admin1", []string{"role_other"}) {
		t.Error("admin1 should bypass command_security for raid")
	}
}

// Delegation keyed on a category (or a thread's parent channel) must grant
// access to the channels under it (#286).
func TestCalculateChannelPermissions_Ancestors(t *testing.T) {
	cfg := testConfig()
	cfg.Discord.DelegatedAdministration.ChannelTracking["category7"] = []string{"user60"}
	if !CalculateChannelPermissions(cfg, "user60", nil, "textchan", "guild1", "category7") {
		t.Error("user60 should manage a channel under category7")
	}
	if !CalculateChannelPermissions(cfg, "user60", nil, "thread1", "guild1", "parentchan", "category7") {
		t.Error("user60 should manage a thread whose parent channel is under category7")
	}
	if CalculateChannelPermissions(cfg, "user60", nil, "textchan", "guild1", "othercat") {
		t.Error("user60 should not manage channels under another category")
	}
}

// Telegram delegated admins and community admins can manage tracking in
// their groups (PoracleJS canAdminChannel); NG never set the permission
// for Telegram (#286).
func TestCalculateTelegramChannelPermissions(t *testing.T) {
	cfg := testConfig()
	cfg.Telegram.DelegatedAdministration.ChannelTracking = map[string][]string{"-100": {"tg7"}}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "North"}}
	cfg.Area.Communities[0].Telegram.Admins = []string{"tg9"}

	cases := []struct {
		name        string
		user        string
		chatIDs     []string
		communities []string
		want        bool
	}{
		{"delegated on the group", "tg7", []string{"-100"}, nil, true},
		{"delegated group, inside a topic", "tg7", []string{"-100:5", "-100"}, nil, true},
		{"delegated elsewhere", "tg7", []string{"-200"}, nil, false},
		{"community admin of the group's community", "tg9", []string{"-300"}, []string{"north"}, true},
		{"community admin, other community", "tg9", []string{"-300"}, []string{"south"}, false},
		{"nobody", "tg1", []string{"-100"}, []string{"north"}, false},
	}
	for _, c := range cases {
		if got := CalculateTelegramChannelPermissions(cfg, c.user, c.chatIDs, c.communities); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	cfg.Area.Enabled = false
	if CalculateTelegramChannelPermissions(cfg, "tg9", []string{"-300"}, []string{"north"}) {
		t.Error("community admins only apply when area security is enabled")
	}
}

func TestTappableCommandSecurity(t *testing.T) {
	if got := commandSecurityName("cmd.tappable"); got != "tappable" {
		t.Errorf("commandSecurityName(cmd.tappable) = %q, want tappable", got)
	}
	cfg := testConfig()
	cfg.Discord.CommandSecurity = map[string][]string{"tappable": {"role_tappers"}}
	blocked := BlockedAlerts(cfg, "user1", []string{"role_other"})
	if blocked == nil || !strings.Contains(*blocked, `"tappable"`) {
		t.Errorf("tappable should be blocked without role_tappers, got %v", blocked)
	}
	if CommandAllowed(cfg, "discord", "cmd.tappable", "user1", []string{"role_other"}) {
		t.Error("cmd.tappable should be refused without role_tappers")
	}
	if !CommandAllowed(cfg, "discord", "cmd.tappable", "user1", []string{"role_tappers"}) {
		t.Error("cmd.tappable should be allowed with role_tappers")
	}
}
