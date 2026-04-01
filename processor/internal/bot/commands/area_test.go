package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func areaTestCtx(t *testing.T) (*bot.CommandContext, *store.MockHumanStore) {
	t.Helper()
	ctx, mock := testCtx(t)

	fences := []geofence.Fence{
		{Name: "Downtown", Group: "City", UserSelectable: true},
		{Name: "Uptown", Group: "City", UserSelectable: true},
		{Name: "Park", Group: "Nature", UserSelectable: true},
	}
	ctx.AreaLogic = bot.NewAreaLogic(fences, &config.Config{Area: config.AreaConfig{Enabled: false}})
	ctx.Fences = fences
	ctx.Config = &config.Config{Area: config.AreaConfig{Enabled: false}}

	return ctx, mock
}

func TestAreaCommand_ListAreas(t *testing.T) {
	ctx, _ := areaTestCtx(t)

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, []string{"list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	// Should show available areas with inactive markers
	assertTextContains(t, replies, "Downtown")
	assertTextContains(t, replies, "Park")
}

func TestAreaCommand_AddArea(t *testing.T) {
	ctx, mock := areaTestCtx(t)

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, []string{"add", "downtown"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetArea")

	h, _ := mock.Get("user1")
	if len(h.Area) == 0 {
		t.Error("expected area to be set")
	}
}

func TestAreaCommand_AddInvalidArea(t *testing.T) {
	ctx, _ := areaTestCtx(t)

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, []string{"add", "nonexistent"})

	// Should still succeed but report not-found
	if len(replies) == 0 {
		t.Fatal("expected reply")
	}
}

func TestAreaCommand_RemoveArea(t *testing.T) {
	ctx, mock := areaTestCtx(t)

	// Seed user with areas
	mock.AddHuman(&store.Human{
		ID:   "user1",
		Area: []string{"downtown", "park"},
	})

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, []string{"remove", "downtown"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetArea")

	h, _ := mock.Get("user1")
	if len(h.Area) != 1 {
		t.Errorf("expected 1 area remaining, got %d", len(h.Area))
	}
}

func TestAreaCommand_RemoveMultiple(t *testing.T) {
	ctx, mock := areaTestCtx(t)

	mock.AddHuman(&store.Human{
		ID:   "user1",
		Area: []string{"downtown", "park"},
	})

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, []string{"remove", "downtown", "park"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetArea")

	h, _ := mock.Get("user1")
	if len(h.Area) != 0 {
		t.Errorf("expected 0 areas after removing both, got %d", len(h.Area))
	}
}

func TestAreaCommand_NoArgs_ShowsCurrent(t *testing.T) {
	ctx, mock := areaTestCtx(t)
	mock.AddHuman(&store.Human{
		ID:   "user1",
		Area: []string{"downtown"},
	})

	cmd := &AreaCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) == 0 {
		t.Fatal("expected reply")
	}
	// Should mention the current area (display name is capitalized)
	assertTextContains(t, replies, "Downtown")
}
