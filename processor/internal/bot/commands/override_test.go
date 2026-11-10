package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newTestLocationCtx builds a CommandContext with AreaLogic pre-configured
// to allow only "london" as a permitted area (for deterministic override tests).
func newTestLocationCtx(t *testing.T) (*bot.CommandContext, *store.MockHumanStore) {
	t.Helper()
	ctx, mock := testCtx(t)

	fences := []geofence.Fence{
		{Name: "london", Group: "UK", UserSelectable: true},
		{Name: "paris", Group: "FR", UserSelectable: true},
	}
	ctx.AreaLogic = bot.NewAreaLogic(fences, &config.Config{Area: config.AreaConfig{Enabled: false}})
	ctx.Config = &config.Config{Area: config.AreaConfig{Enabled: false}}
	return ctx, mock
}

func TestParseOverride_LocationRequiresDistance(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, "Home", nil, 0)
	if reply == nil || !strings.Contains(reply.Text, "needs a `d:`") {
		t.Fatalf("expected requires-distance error, got %+v", reply)
	}
}

func TestParseOverride_AreaAndDistanceRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, "", []string{"london"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected a+d rejection, got %+v", reply)
	}
}

func TestParseOverride_LocationAndAreaRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, "Home", []string{"london"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected location+area rejection, got %+v", reply)
	}
}

func TestParseOverride_UnknownLocation(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, "Nope", nil, 500)
	if reply == nil || !strings.Contains(reply.Text, "No saved location") {
		t.Fatalf("expected unknown-location error, got %+v", reply)
	}
}

func TestParseOverride_AreaNotPermitted(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	// "berlin" is not in the fences seeded by newTestLocationCtx (only london, paris)
	_, reply := parseOverride(ctx, "", []string{"berlin"}, 0)
	if reply == nil || !strings.Contains(reply.Text, "not in your allowed") {
		t.Fatalf("expected permission error, got %+v", reply)
	}
}

func TestParseOverride_ValidLocation(t *testing.T) {
	ctx, mock := newTestLocationCtx(t)
	if _, err := mock.AddLocation(store.UserLocation{
		ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, reply := parseOverride(ctx, "home", nil, 500)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if got.LocationLabel != "Home" {
		t.Fatalf("label not normalised to stored case: %+v", got)
	}
}

func TestParseOverride_ValidAreas(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	got, reply := parseOverride(ctx, "", []string{"london"}, 0)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "london" {
		t.Fatalf("areas not stored: %+v", got)
	}
}

// TestOverrideCommand_StoresCanonicalName: `!track pikachu area:gent_centrum`
// reaches the command as "gent centrum" (the parser turns unquoted underscores
// into spaces), must be permitted against fence Gent_Centrum, and is stored as
// the fence's own spelling.
func TestOverrideCommand_StoresCanonicalName(t *testing.T) {
	ctx, _ := newTestTrackCtx(t)
	ctx.AreaLogic = bot.NewAreaLogic([]geofence.Fence{
		{Name: "Gent_Centrum", Group: "BE", UserSelectable: true},
		{Name: "london", Group: "UK", UserSelectable: true},
	}, ctx.Config)

	replies := (&TrackCommand{}).Run(ctx, []string{"25", "iv100", "area:gent centrum"})
	if anyReact(replies, "🙅") {
		t.Fatalf("spaced spelling of fence Gent_Centrum refused: %+v", replies)
	}
	rules, _ := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if len(rules) != 1 || len(rules[0].OverrideAreas) != 1 || rules[0].OverrideAreas[0] != "gent_centrum" {
		t.Fatalf("stored override_areas = %+v, want [gent_centrum]", rules)
	}
}

func TestParseOverride_DedupesBySpellingKey(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	ctx.AreaLogic = bot.NewAreaLogic([]geofence.Fence{
		{Name: "Gent_Centrum", UserSelectable: true},
		{Name: "london", UserSelectable: true},
	}, ctx.Config)
	got, reply := parseOverride(ctx, "", []string{"gent centrum", "London", "GENT_CENTRUM"}, 0)
	if reply != nil {
		t.Fatalf("rejected: %+v", reply)
	}
	if len(got.Areas) != 2 || got.Areas[0] != "gent_centrum" || got.Areas[1] != "london" {
		t.Fatalf("areas = %v, want [gent_centrum london]", got.Areas)
	}
}

func TestEnforceDistance_MaxDistance(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	ctx.Config.Tracking.MaxDistance = 1000
	if got := enforceDistance(ctx, 5000, false); got != 1000 {
		t.Errorf("non-admin: got %d, want 1000", got)
	}
	ctx.TargetType = bot.TypeDiscordChannel
	if got := enforceDistance(ctx, 5000, false); got != 5000 {
		t.Errorf("channel target: got %d, want 5000 (exempt)", got)
	}
	ctx.TargetType = bot.TypeDiscordUser
	ctx.IsAdmin = true
	if got := enforceDistance(ctx, 5000, false); got != 5000 {
		t.Errorf("admin: got %d, want 5000 (exempt)", got)
	}
}
