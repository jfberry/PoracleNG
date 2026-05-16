package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// dispatcherWithFakeHumans returns a Dispatcher whose deps.Humans is seeded
// with the given map of typed-ID → HumanLite records. The MockHumanStore's
// GetLite returns a parsed lite from the underlying Human row, so we mirror
// each lite into a full Human with the same fields the lite carries.
func dispatcherWithFakeHumans(t *testing.T, humans map[string]*store.HumanLite) *Dispatcher {
	t.Helper()
	mock := store.NewMockHumanStore()
	for id, lite := range humans {
		if lite == nil {
			continue
		}
		h := &store.Human{
			ID:               id,
			Type:             lite.Type,
			Name:             lite.Name,
			Enabled:          lite.Enabled,
			AdminDisable:     lite.AdminDisable,
			Language:         lite.Language,
			CurrentProfileNo: lite.CurrentProfileNo,
		}
		mock.AddHuman(h)
	}

	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = &bot.BotDeps{Humans: mock}
	return d
}

// buildInteraction constructs a minimal InteractionCreate for a DM-style
// invocation (User populated, Member nil). Caller can mutate ic.Locale or
// promote to a guild interaction by replacing User with Member.
func buildInteraction(userID, channelID string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:      discordgo.InteractionApplicationCommand,
		ChannelID: channelID,
		User:      &discordgo.User{ID: userID, Username: userID},
	}}
}

// dispatcherWithFakeHuman seeds the mock store with one full *store.Human so
// tests can exercise fields (Area, Latitude, Longitude) that HumanLite skips.
func dispatcherWithFakeHuman(t *testing.T, h *store.Human) *Dispatcher {
	t.Helper()
	mock := store.NewMockHumanStore()
	if h != nil {
		mock.AddHuman(h)
	}
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = &bot.BotDeps{Humans: mock}
	return d
}

func TestBuildContextLanguageFromHuman(t *testing.T) {
	d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
		"42": {ID: "42", Language: "de"},
	})
	ic := buildInteraction("42", "")

	ctx, err := d.buildContext(ic, "cmd.tracked")
	if err != nil {
		t.Fatalf("buildContext err: %v", err)
	}
	if ctx.Language != "de" {
		t.Errorf("language=%q, want de", ctx.Language)
	}
}

func TestBuildContextLanguageFallbackToLocale(t *testing.T) {
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.General.Locale = "fr"

	ic := buildInteraction("99", "")
	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.Language != "fr" {
		t.Errorf("language=%q, want fr", ctx.Language)
	}
}

func TestBuildContextLanguageFromDiscordLocale(t *testing.T) {
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.General.Locale = "en"

	ic := buildInteraction("99", "")
	ic.Locale = discordgo.German

	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.Language != "de" {
		t.Errorf("language=%q, want de (mapped from discordgo.German)", ctx.Language)
	}
}

func TestBuildContextTargetAlwaysSender(t *testing.T) {
	d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
		"42": {ID: "42", CurrentProfileNo: 1},
	})

	ic := buildInteraction("42", "")
	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.TargetID != "42" {
		t.Errorf("target=%q, want 42", ctx.TargetID)
	}
}

func TestBuildContextAdminFromDiscordAdmins(t *testing.T) {
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.Discord.Admins = []string{"42"}

	ic := buildInteraction("42", "")
	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if !ctx.IsAdmin {
		t.Error("IsAdmin=false, want true for admin user")
	}
}

func TestBuildContextNonAdminUser(t *testing.T) {
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.Discord.Admins = []string{"42"}

	ic := buildInteraction("999", "")
	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.IsAdmin {
		t.Error("IsAdmin=true, want false for non-admin user")
	}
}

func TestBuildContextAdminInDMSafe(t *testing.T) {
	// Regression: ic.Member is nil in DMs. Admin lookup must not panic.
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.Discord.Admins = []string{"42"}

	ic := buildInteraction("42", "dm-chan")
	// Confirm Member is unset (DM-style).
	if ic.Member != nil {
		t.Fatal("test setup: expected Member to be nil for DM interaction")
	}

	ctx, err := d.buildContext(ic, "cmd.tracked")
	if err != nil {
		t.Fatalf("buildContext err: %v", err)
	}
	if !ctx.IsAdmin {
		t.Error("IsAdmin=false, want true even in DM where Member is nil")
	}
}

func TestBuildContextProfileFromHuman(t *testing.T) {
	d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
		"42": {ID: "42", CurrentProfileNo: 3},
	})
	ic := buildInteraction("42", "")

	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.ProfileNo != 3 {
		t.Errorf("ProfileNo=%d, want 3", ctx.ProfileNo)
	}
}

func TestBuildContextLanguageHumanBeatsDiscordLocale(t *testing.T) {
	// Chain priority: explicit human.Language wins over Discord locale.
	d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
		"42": {ID: "42", Language: "de"},
	})
	d.cfgRoot.General.Locale = "en"

	ic := buildInteraction("42", "")
	ic.Locale = discordgo.French // would map to "fr" if human had no language

	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.Language != "de" {
		t.Errorf("language=%q, want de (human wins over discord locale)", ctx.Language)
	}
}

func TestBuildContextLanguageUnmappedDiscordLocaleFallsThrough(t *testing.T) {
	// Discord locale not in our mapping → fall through to cfg.General.Locale.
	d := dispatcherWithFakeHumans(t, nil)
	d.cfgRoot.General.Locale = "fr"

	ic := buildInteraction("99", "")
	ic.Locale = discordgo.Bulgarian // not in discordLocaleToPoracle

	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.Language != "fr" {
		t.Errorf("language=%q, want fr (unmapped locale falls through)", ctx.Language)
	}
}

// Regression: /tracked emits an "⚠️ no areas set" warning when ctx.HasArea is
// false, even when the underlying Human row has areas. buildContext must pull
// the full record so HasArea / HasLocation reflect the persisted state.
func TestBuildContextPopulatesHasAreaAndHasLocation(t *testing.T) {
	d := dispatcherWithFakeHuman(t, &store.Human{
		ID:        "42",
		Area:      []string{"london", "paris"},
		Latitude:  51.5,
		Longitude: -0.1,
	})
	ic := buildInteraction("42", "")

	ctx, err := d.buildContext(ic, "cmd.tracked")
	if err != nil {
		t.Fatalf("buildContext err: %v", err)
	}
	if !ctx.HasArea {
		t.Error("HasArea=false, want true (human row has areas)")
	}
	if !ctx.HasLocation {
		t.Error("HasLocation=false, want true (human row has lat/lon)")
	}
}

func TestBuildContextNoAreaNoLocationWhenUnset(t *testing.T) {
	d := dispatcherWithFakeHuman(t, &store.Human{ID: "42"})
	ic := buildInteraction("42", "")

	ctx, _ := d.buildContext(ic, "cmd.tracked")
	if ctx.HasArea {
		t.Error("HasArea=true on a human with no areas")
	}
	if ctx.HasLocation {
		t.Error("HasLocation=true on a human with no lat/lon")
	}
}
