package discordbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func newTestSnapshot() *guildSnapshot {
	return &guildSnapshot{
		channels:                  map[string]*discordgo.Channel{},
		categoriesByLowerName:     map[string]string{},
		channelsByParentLowerName: map[string]map[string]string{},
		threads:                   map[string]bool{},
		rolesByLowerName:          map[string]string{},
	}
}

// TestApplyAutocreate_SameChannelNameDifferentCategory is the regression test
// for the reported bug: creating a new category (City 2) whose channels share
// names with an existing category (City 1) must CREATE new channels under City
// 2, never adopt/move City 1's same-named channels. The old code fell back to a
// guild-wide name lookup (findChannelAnyParent) and relocated City 1/100 into
// City 2. Runs in dry-run (no Discord/DB writes) with both categories already
// present and roles: null, so applyAutocreate makes no session calls.
func TestApplyAutocreate_SameChannelNameDifferentCategory(t *testing.T) {
	snap := newTestSnapshot()
	snap.addCategory("cat-city1", "City 1")
	snap.addCategory("cat-city2", "City 2")
	// City 1 already owns a "100" channel.
	snap.addChannel("ch-city1-100", "cat-city1", "100")

	tmpl := &channelTemplate{
		Name: "city 2",
		Definition: channelDefinition{
			Category: &categoryDefinition{CategoryName: "City 2"}, // reused, no roles
			Channels: []channelEntry{
				{ChannelName: "100", ChannelType: "text", ControlType: "bot"},
			},
		},
	}

	b := &Bot{}
	rep := &collectingReporter{}
	result := b.applyAutocreate(
		nil, // session — unused in this dry-run shape (reused category, roles:null, no threads/commands)
		&autocreateActor{UserID: "u1", UserName: "tester"},
		snap,
		tmpl,
		nil, // args
		nil, // rawArgs
		"guild-1",
		rep,
		applyAutocreateOptions{DryRun: true},
	)

	// City 2/100 must be a NEW channel, not City 1's existing one.
	if got := result.ChannelIDs["100"]; got == "ch-city1-100" {
		t.Fatalf("City 2/100 reused City 1's channel (%s) — the cross-category steal has regressed", got)
	}
	if result.ChannelsCreated != 1 {
		t.Errorf("ChannelsCreated = %d, want 1 (City 2/100 created fresh)", result.ChannelsCreated)
	}
	if result.ChannelsReused != 0 {
		t.Errorf("ChannelsReused = %d, want 0", result.ChannelsReused)
	}

	// City 1's "100" is untouched — still under City 1's category.
	if got := snap.findChannel("cat-city1", "100"); got != "ch-city1-100" {
		t.Errorf("City 1's 100 channel = %q, want ch-city1-100 (must not be moved/removed)", got)
	}
	// The new channel is registered under City 2's category.
	if got := snap.findChannel("cat-city2", "100"); got == "" || got == "ch-city1-100" {
		t.Errorf("City 2/100 = %q, want a fresh channel under City 2", got)
	}
}

// TestApplyAutocreate_ReusesChannelInSameCategory confirms the idempotent path
// still works: re-running the same template reuses the channel already under
// that category (rather than creating a duplicate).
func TestApplyAutocreate_ReusesChannelInSameCategory(t *testing.T) {
	snap := newTestSnapshot()
	snap.addCategory("cat-city2", "City 2")
	snap.addChannel("ch-city2-100", "cat-city2", "100") // already exists under City 2

	tmpl := &channelTemplate{
		Name: "city 2",
		Definition: channelDefinition{
			Category: &categoryDefinition{CategoryName: "City 2"},
			Channels: []channelEntry{
				{ChannelName: "100", ChannelType: "text", ControlType: "bot"},
			},
		},
	}

	b := &Bot{}
	rep := &collectingReporter{}
	result := b.applyAutocreate(nil, &autocreateActor{UserID: "u1"}, snap, tmpl, nil, nil, "guild-1", rep,
		applyAutocreateOptions{DryRun: true})

	if result.ChannelsReused != 1 {
		t.Errorf("ChannelsReused = %d, want 1 (existing City 2/100 reused)", result.ChannelsReused)
	}
	if result.ChannelsCreated != 0 {
		t.Errorf("ChannelsCreated = %d, want 0", result.ChannelsCreated)
	}
	if got := result.ChannelIDs["100"]; got != "ch-city2-100" {
		t.Errorf("reused channel ID = %q, want ch-city2-100", got)
	}
}
