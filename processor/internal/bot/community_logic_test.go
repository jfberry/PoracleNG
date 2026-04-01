package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func communityConfig() *config.Config {
	cc1 := config.CommunityConfig{
		Name:          "TeamCity",
		AllowedAreas:  []string{"Downtown", "Uptown"},
		LocationFence: []string{"CityZone"},
	}
	cc1.Discord.Channels = []string{"discord-ch-1"}
	cc1.Telegram.Channels = []string{"telegram-ch-1"}

	cc2 := config.CommunityConfig{
		Name:          "TeamNature",
		AllowedAreas:  []string{"Park", "Harbor"},
		LocationFence: []string{"NatureZone", "CoastZone"},
	}
	cc2.Discord.Channels = []string{"discord-ch-2"}
	cc2.Telegram.Channels = []string{"telegram-ch-2"}

	return &config.Config{
		Area: config.AreaConfig{
			Enabled:     true,
			Communities: []config.CommunityConfig{cc1, cc2},
		},
		Discord:  config.DiscordConfig{Channels: []string{"discord-reg"}},
		Telegram: config.TelegramConfig{Channels: []string{"telegram-reg"}},
	}
}

// --- AddCommunity ---

func TestAddCommunity_New(t *testing.T) {
	cfg := communityConfig()
	result := AddCommunity(cfg, nil, "teamcity")
	if len(result) != 1 || result[0] != "teamcity" {
		t.Errorf("expected [teamcity], got %v", result)
	}
}

func TestAddCommunity_AlreadyPresent(t *testing.T) {
	cfg := communityConfig()
	result := AddCommunity(cfg, []string{"teamcity"}, "teamcity")
	if len(result) != 1 {
		t.Errorf("expected 1 community (no duplicate), got %d", len(result))
	}
}

func TestAddCommunity_Invalid(t *testing.T) {
	cfg := communityConfig()
	result := AddCommunity(cfg, nil, "nonexistent")
	if len(result) != 0 {
		t.Errorf("expected empty (invalid community filtered out), got %v", result)
	}
}

func TestAddCommunity_Multiple(t *testing.T) {
	cfg := communityConfig()
	result := AddCommunity(cfg, []string{"teamcity"}, "teamnature")
	if len(result) != 2 {
		t.Fatalf("expected 2 communities, got %d", len(result))
	}
	// Should be sorted
	if result[0] != "teamcity" || result[1] != "teamnature" {
		t.Errorf("expected sorted [teamcity, teamnature], got %v", result)
	}
}

// --- RemoveCommunity ---

func TestRemoveCommunity_Exists(t *testing.T) {
	cfg := communityConfig()
	result := RemoveCommunity(cfg, []string{"teamcity", "teamnature"}, "teamcity")
	if len(result) != 1 || result[0] != "teamnature" {
		t.Errorf("expected [teamnature], got %v", result)
	}
}

func TestRemoveCommunity_NotPresent(t *testing.T) {
	cfg := communityConfig()
	result := RemoveCommunity(cfg, []string{"teamcity"}, "teamnature")
	if len(result) != 1 || result[0] != "teamcity" {
		t.Errorf("expected [teamcity], got %v", result)
	}
}

func TestRemoveCommunity_LastOne(t *testing.T) {
	cfg := communityConfig()
	result := RemoveCommunity(cfg, []string{"teamcity"}, "teamcity")
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

// --- CalculateLocationRestrictions ---

func TestCalculateLocationRestrictions_Single(t *testing.T) {
	cfg := communityConfig()
	result := CalculateLocationRestrictions(cfg, []string{"teamcity"})
	if len(result) != 1 || result[0] != "cityzone" {
		t.Errorf("expected [cityzone], got %v", result)
	}
}

func TestCalculateLocationRestrictions_Multiple(t *testing.T) {
	cfg := communityConfig()
	result := CalculateLocationRestrictions(cfg, []string{"teamcity", "teamnature"})
	if len(result) != 3 {
		t.Fatalf("expected 3 restrictions, got %d: %v", len(result), result)
	}
}

func TestCalculateLocationRestrictions_NoCommunities(t *testing.T) {
	cfg := communityConfig()
	result := CalculateLocationRestrictions(cfg, nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

// --- FindCommunityForChannel ---

func TestFindCommunityForChannel_Discord(t *testing.T) {
	cfg := communityConfig()
	result := FindCommunityForChannel(cfg, "discord", "discord-ch-1")
	if result != "TeamCity" {
		t.Errorf("expected TeamCity, got %s", result)
	}
}

func TestFindCommunityForChannel_Telegram(t *testing.T) {
	cfg := communityConfig()
	result := FindCommunityForChannel(cfg, "telegram", "telegram-ch-2")
	if result != "TeamNature" {
		t.Errorf("expected TeamNature, got %s", result)
	}
}

func TestFindCommunityForChannel_NotFound(t *testing.T) {
	cfg := communityConfig()
	result := FindCommunityForChannel(cfg, "discord", "unknown-channel")
	if result != "" {
		t.Errorf("expected empty, got %s", result)
	}
}

// --- IsRegistrationChannel ---

func TestIsRegistrationChannel_AreaSecurity_Valid(t *testing.T) {
	cfg := communityConfig()
	if !IsRegistrationChannel(cfg, "discord", "discord-ch-1") {
		t.Error("expected true for community channel")
	}
}

func TestIsRegistrationChannel_AreaSecurity_Invalid(t *testing.T) {
	cfg := communityConfig()
	if IsRegistrationChannel(cfg, "discord", "random-channel") {
		t.Error("expected false for non-community channel")
	}
}

func TestIsRegistrationChannel_NoAreaSecurity(t *testing.T) {
	cfg := communityConfig()
	cfg.Area.Enabled = false

	if !IsRegistrationChannel(cfg, "discord", "discord-reg") {
		t.Error("expected true for configured registration channel")
	}
	if IsRegistrationChannel(cfg, "discord", "random-channel") {
		t.Error("expected false for non-registration channel")
	}
}

// --- CommunityNames ---

func TestCommunityNames(t *testing.T) {
	cfg := communityConfig()
	names := CommunityNames(cfg)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	// Should be sorted
	if names[0] != "TeamCity" || names[1] != "TeamNature" {
		t.Errorf("expected sorted [TeamCity, TeamNature], got %v", names)
	}
}

// --- ParseCommunityMembership ---

func TestParseCommunityMembership_Valid(t *testing.T) {
	result := ParseCommunityMembership(`["teamcity","teamnature"]`)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestParseCommunityMembership_Empty(t *testing.T) {
	if result := ParseCommunityMembership(""); result != nil {
		t.Errorf("expected nil, got %v", result)
	}
	if result := ParseCommunityMembership("[]"); result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestParseCommunityMembership_Invalid(t *testing.T) {
	if result := ParseCommunityMembership("not json"); result != nil {
		t.Errorf("expected nil for invalid JSON, got %v", result)
	}
}
