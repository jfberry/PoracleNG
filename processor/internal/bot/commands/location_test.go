package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// TestLocationCommandBareShowsCurrent proves that `!location` with no args
// first shows the user's current location (if set) before the usage help,
// instead of only showing the help.
func TestLocationCommandBareShowsCurrent(t *testing.T) {
	ctx, humans := testCtx(t)
	ctx.HasLocation = true

	// Seed the user's location via the store API.
	if err := humans.SetLocation("user1", 1, 51.5074, -0.1278); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) < 2 {
		t.Fatalf("expected at least 2 replies (current + usage), got %d", len(replies))
	}

	// First reply is the current location with a map link.
	if !strings.Contains(replies[0].Text, "51.507400") || !strings.Contains(replies[0].Text, "-0.127800") {
		t.Errorf("first reply should mention current lat/lon, got %q", replies[0].Text)
	}
	if !strings.Contains(replies[0].Text, "maps.google.com") {
		t.Errorf("first reply should include a map link, got %q", replies[0].Text)
	}

	// Last reply is the usage help (unchanged behaviour).
	if !strings.Contains(replies[len(replies)-1].Text, "location") {
		t.Errorf("last reply should be the usage help, got %q", replies[len(replies)-1].Text)
	}
}

// TestLocationCommandBareNoLocationShowsUsageOnly proves that `!location`
// with no args and no stored location falls through to just the usage
// reply (i.e. only one reply is returned, matching the previous
// behaviour for users who haven't set a location yet).
func TestLocationCommandBareNoLocationShowsUsageOnly(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.HasLocation = false

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply (usage only), got %d: %+v", len(replies), replies)
	}
	if !strings.Contains(replies[0].Text, "location") {
		t.Errorf("expected usage reply, got %q", replies[0].Text)
	}
}

// TestLocationCommandBareSetButZeroZero proves that a user flagged as
// HasLocation=true but whose stored coords are 0,0 doesn't produce a
// spurious "current location is 0,0" reply — only the usage reply fires.
func TestLocationCommandBareSetButZeroZero(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.HasLocation = true
	// Seeded user is 0,0 by default (humans.AddHuman in testCtx doesn't set
	// lat/lon), which is what we want to test.

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, nil)

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply (usage only), got %d: %+v", len(replies), replies)
	}
	if strings.Contains(replies[0].Text, "0.000000") {
		t.Errorf("should not render 0,0 as a current location, got %q", replies[0].Text)
	}
}

// --- add subcommand ---

// TestLocation_AddSavesNamedLocation verifies that `!location add Home 51.5,-0.1`
// persists the named location and returns a confirmation.
func TestLocation_AddSavesNamedLocation(t *testing.T) {
	ctx, mock := testCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"add", "Home", "51.5,-0.1"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if replies[0].React != "✅" {
		t.Fatalf("expected ✅ react, got %q (text: %q)", replies[0].React, replies[0].Text)
	}
	if !strings.Contains(replies[0].Text, "Home") {
		t.Errorf("expected confirmation to mention the location name, got %q", replies[0].Text)
	}

	got, err := mock.GetLocation("user1", "Home")
	if err != nil {
		t.Fatalf("GetLocation: %v", err)
	}
	if got == nil {
		t.Fatal("expected location to be persisted, got nil")
	}
	if got.Latitude != 51.5 {
		t.Errorf("expected latitude 51.5, got %v", got.Latitude)
	}
}

// TestLocation_AddRejectsMissingArgs verifies that `!location add` (no name/coords)
// returns a usage error.
func TestLocation_AddRejectsMissingArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"add"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if replies[0].React != "🙅" {
		t.Errorf("expected 🙅 react for missing args, got %q", replies[0].React)
	}
}

// TestLocation_AddRejectsMissingCoords verifies that `!location add Home` (name only,
// no coords and no geocoder) returns an error.
func TestLocation_AddRejectsMissingCoords(t *testing.T) {
	ctx, _ := testCtx(t)
	cmd := &LocationCommand{}
	// Geocoder is nil in testCtx — the forward-geocode fallback will fail.
	replies := cmd.Run(ctx, []string{"add", "Home"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if replies[0].React != "🙅" {
		t.Errorf("expected 🙅 react, got %q (text: %q)", replies[0].React, replies[0].Text)
	}
}

// TestLocation_AddRejectsDuplicate verifies that adding a second location with
// the same name returns a distinct "already have" error.
func TestLocation_AddRejectsDuplicate(t *testing.T) {
	ctx, mock := testCtx(t)
	// Seed an existing location.
	if _, err := mock.AddLocation(store.UserLocation{
		ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"add", "Home", "0,0"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if replies[0].React != "🙅" {
		t.Errorf("expected 🙅 react, got %q", replies[0].React)
	}
	// The duplicate message should mention the name and hint at remove.
	if !strings.Contains(replies[0].Text, "Home") {
		t.Errorf("expected duplicate error to mention the name, got %q", replies[0].Text)
	}
	if !strings.Contains(strings.ToLower(replies[0].Text), "already") {
		t.Errorf("expected duplicate error to say 'already', got %q", replies[0].Text)
	}
}

// --- list subcommand ---

// TestLocation_ListShowsNamedLocations verifies that `!location list` shows
// saved named locations.
func TestLocation_ListShowsNamedLocations(t *testing.T) {
	ctx, mock := testCtx(t)
	if _, err := mock.AddLocation(store.UserLocation{
		ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if !strings.Contains(replies[0].Text, "Home") {
		t.Errorf("expected list to mention 'Home', got %q", replies[0].Text)
	}
}

// TestLocation_ListShowsDefaultAndNamed verifies that when a default location
// AND named locations are set, both appear in the list output.
func TestLocation_ListShowsDefaultAndNamed(t *testing.T) {
	ctx, mock := testCtx(t)
	// Set default location.
	if err := mock.SetLocation("user1", 1, 10.0, 20.0); err != nil {
		t.Fatalf("seed default location: %v", err)
	}
	// Add named location.
	if _, err := mock.AddLocation(store.UserLocation{
		ID: "user1", Label: "Work", Latitude: 51.5, Longitude: -0.1,
	}); err != nil {
		t.Fatalf("seed named location: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	body := replies[0].Text
	if !strings.Contains(body, "10") {
		t.Errorf("expected default location (10.0) in list, got %q", body)
	}
	if !strings.Contains(body, "Work") {
		t.Errorf("expected named location 'Work' in list, got %q", body)
	}
}

// TestLocation_ListEmptyShowsHint verifies that when the user has no saved
// locations and no default, the list command returns an "add one" hint.
func TestLocation_ListEmptyShowsHint(t *testing.T) {
	ctx, _ := testCtx(t)
	// No default, no named locations.

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	body := strings.ToLower(replies[0].Text)
	if !strings.Contains(body, "location") {
		t.Errorf("expected empty list hint to mention 'location', got %q", replies[0].Text)
	}
}

// --- show subcommand ---

func TestLocation_ShowCaseInsensitive(t *testing.T) {
	ctx, mock := testCtx(t)
	if _, err := mock.AddLocation(store.UserLocation{ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "home"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if !strings.Contains(replies[0].Text, "51.5") {
		t.Fatalf("show should be case-insensitive, got %s", replies[0].Text)
	}
}

func TestLocation_ShowNotFound(t *testing.T) {
	ctx, _ := testCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "Nope"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if !strings.Contains(replies[0].Text, "No saved location") {
		t.Fatalf("expected not-found message, got %s", replies[0].Text)
	}
}

// --- remove subcommand ---

func TestLocation_RemoveRefusesWhenReferenced(t *testing.T) {
	ctx, mock := testCtx(t)
	if _, err := mock.AddLocation(store.UserLocation{ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mock.LocationRefs = map[string][]store.ReferencingRule{
		"user1|home": {
			{Type: "pokemon", UID: 42},
			{Type: "raid", UID: 17},
		},
	}
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "Home"})
	if !strings.Contains(replies[0].Text, "Cannot remove") || !strings.Contains(replies[0].Text, "2") {
		t.Fatalf("expected refuse-with-count, got %s", replies[0].Text)
	}
	if loc, _ := mock.GetLocation("user1", "Home"); loc == nil {
		t.Fatalf("location should NOT have been deleted")
	}
}

func TestLocation_RemoveNamedSucceeds(t *testing.T) {
	ctx, mock := testCtx(t)
	if _, err := mock.AddLocation(store.UserLocation{ID: "user1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "Home"})
	if !strings.Contains(replies[0].Text, "Removed") {
		t.Fatalf("expected success, got %s", replies[0].Text)
	}
	if loc, _ := mock.GetLocation("user1", "Home"); loc != nil {
		t.Fatalf("location should be deleted")
	}
}

func TestLocation_RemoveDefault(t *testing.T) {
	ctx, mock := testCtx(t)
	if err := mock.SetLocation("user1", 1, 51.5, -0.1); err != nil {
		t.Fatalf("seed default location: %v", err)
	}
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove", "default"})
	if !strings.Contains(replies[0].Text, "Cleared") {
		t.Fatalf("expected default-cleared, got %s", replies[0].Text)
	}
}

func TestLocation_RemoveBareErrors(t *testing.T) {
	ctx, _ := testCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"remove"})
	if !strings.Contains(replies[0].Text, "Valid commands are") {
		t.Fatalf("bare remove should show usage, got %s", replies[0].Text)
	}
}

// --- UX fixes: uniform list rows, show default, addresses ---

// TestLocation_ListUsesUniformRowFormat verifies that both the default and
// named entries in `!location list` use backtick-quoted labels.
func TestLocation_ListUsesUniformRowFormat(t *testing.T) {
	ctx, mock := testCtx(t)
	if err := mock.SetLocation("user1", 1, 51.5, -0.1); err != nil {
		t.Fatalf("seed default location: %v", err)
	}
	if _, err := mock.AddLocation(store.UserLocation{
		ID: "user1", Label: "Home", Latitude: 51.6, Longitude: -0.2,
	}); err != nil {
		t.Fatalf("seed named location: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"list"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	body := replies[0].Text
	if !strings.Contains(body, "`default`") {
		t.Errorf("expected backtick-quoted `default` row: %s", body)
	}
	if !strings.Contains(body, "`Home`") {
		t.Errorf("expected backtick-quoted `Home` row: %s", body)
	}
}

// TestLocation_ShowDefault verifies that `!location show default` returns the
// user's default location coordinates.
func TestLocation_ShowDefault(t *testing.T) {
	ctx, mock := testCtx(t)
	if err := mock.SetLocation("user1", 1, 51.5, -0.1); err != nil {
		t.Fatalf("seed default location: %v", err)
	}

	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "default"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if !strings.Contains(replies[0].Text, "51.5") {
		t.Fatalf("expected coords in show-default reply, got %s", replies[0].Text)
	}
}

// TestLocation_ShowDefault_NoneSet verifies that `!location show default` when
// no default location is set returns a friendly error.
func TestLocation_ShowDefault_NoneSet(t *testing.T) {
	ctx, _ := testCtx(t)
	cmd := &LocationCommand{}
	replies := cmd.Run(ctx, []string{"show", "default"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	if !strings.Contains(replies[0].Text, "No default") {
		t.Fatalf("expected no-default error, got %s", replies[0].Text)
	}
}
