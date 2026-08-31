package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// !profile add reported the profile number by searching back for the HIGHEST
// profile whose name matched the one just created. Profile names are not
// unique, and the store assigns the lowest FREE number, so creating "Work"
// while an older "Work" sat at a higher number reported that older profile's
// number instead of the new one. AddProfile now returns what it assigned.
func TestProfileAddReportsTheNumberActuallyAssigned(t *testing.T) {
	ctx, humans := testCtx(t)
	// 1 is the seeded default. Take 3 with a profile sharing the name we are
	// about to create, leaving 2 as the lowest free slot.
	humans.SeedProfile(store.Profile{ID: "user1", ProfileNo: 1, Name: "Default"})
	humans.SeedProfile(store.Profile{ID: "user1", ProfileNo: 3, Name: "Work"})

	c := &ProfileCommand{}
	replies := c.Run(ctx, []string{"add", "Work"})

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d: %+v", len(replies), replies)
	}
	if replies[0].React != "✅" {
		t.Fatalf("expected success reply, got %+v", replies[0])
	}
	// The new profile is 2; 3 is the pre-existing same-named one.
	if !strings.Contains(replies[0].Text, "2") {
		t.Errorf("reply = %q, want it to report the newly assigned profile 2", replies[0].Text)
	}
	if strings.Contains(replies[0].Text, "3") {
		t.Errorf("reply = %q, wrongly reports the pre-existing same-named profile 3", replies[0].Text)
	}
}
