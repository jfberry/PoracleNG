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
	_, reply := parseOverride(ctx, map[string]string{"location": "Home"}, nil, 0)
	if reply == nil || !strings.Contains(reply.Text, "needs a `d:`") {
		t.Fatalf("expected requires-distance error, got %+v", reply)
	}
}

func TestParseOverride_AreaAndDistanceRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, nil, []string{"london"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected a+d rejection, got %+v", reply)
	}
}

func TestParseOverride_LocationAndAreaRejected(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, map[string]string{"location": "Home"}, []string{"london"}, 500)
	if reply == nil || !strings.Contains(reply.Text, "mutually exclusive") {
		t.Fatalf("expected location+area rejection, got %+v", reply)
	}
}

func TestParseOverride_UnknownLocation(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	_, reply := parseOverride(ctx, map[string]string{"location": "Nope"}, nil, 500)
	if reply == nil || !strings.Contains(reply.Text, "No saved location") {
		t.Fatalf("expected unknown-location error, got %+v", reply)
	}
}

func TestParseOverride_AreaNotPermitted(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	// "berlin" is not in the fences seeded by newTestLocationCtx (only london, paris)
	_, reply := parseOverride(ctx, nil, []string{"berlin"}, 0)
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
	got, reply := parseOverride(ctx, map[string]string{"location": "home"}, nil, 500)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if got.LocationLabel != "Home" {
		t.Fatalf("label not normalised to stored case: %+v", got)
	}
}

func TestParseOverride_ValidAreas(t *testing.T) {
	ctx, _ := newTestLocationCtx(t)
	got, reply := parseOverride(ctx, nil, []string{"london"}, 0)
	if reply != nil {
		t.Fatalf("valid case rejected: %+v", reply)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "london" {
		t.Fatalf("areas not stored: %+v", got)
	}
}
