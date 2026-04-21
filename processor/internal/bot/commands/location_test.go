package commands

import (
	"strings"
	"testing"
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
