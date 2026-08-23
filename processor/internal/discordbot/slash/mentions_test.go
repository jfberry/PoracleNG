package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// Discord addresses a subcommand by its full path but always with the
// TOP-LEVEL command's id, and a command that only holds subcommands is not
// itself invocable. FlattenCommands has to encode both rules.
func TestFlattenCommands(t *testing.T) {
	cmds := []*discordgo.ApplicationCommand{
		{ID: "111", Name: "help"},
		{ID: "222", Name: "untrack", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "raid"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "pokemon"},
		}},
		{ID: "333", Name: "summary", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommandGroup, Name: "quest", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "settime"},
				{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "now"},
			}},
		}},
		// Plain options (not subcommands) must NOT split the command.
		{ID: "444", Name: "track", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "pokemon"},
		}},
	}

	got := FlattenCommands(cmds)

	want := []CommandMention{
		{Path: "help", ID: "111"},
		{Path: "summary quest now", ID: "333"},
		{Path: "summary quest settime", ID: "333"},
		{Path: "track", ID: "444"},
		{Path: "untrack pokemon", ID: "222"},
		{Path: "untrack raid", ID: "222"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mentions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A parent that holds subcommands is not invocable on its own, so it must not
// appear as a bare path alongside its children.
func TestFlattenCommandsOmitsNonInvocableParent(t *testing.T) {
	got := FlattenCommands([]*discordgo.ApplicationCommand{
		{ID: "222", Name: "untrack", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "raid"},
		}},
	})
	for _, m := range got {
		if m.Path == "untrack" {
			t.Fatalf("bare parent %q must not be listed: %+v", m.Path, got)
		}
	}
}

func TestFlattenCommandsEmpty(t *testing.T) {
	if got := FlattenCommands(nil); len(got) != 0 {
		t.Fatalf("expected no mentions, got %+v", got)
	}
}

// Mention renders Discord's clickable command syntax.
func TestCommandMentionMention(t *testing.T) {
	m := CommandMention{Path: "untrack raid", ID: "222"}
	if got := m.Mention(); got != "</untrack raid:222>" {
		t.Errorf("Mention() = %q, want %q", got, "</untrack raid:222>")
	}
}

// ListRegistered reads live from Discord, because SyncCommands discards the
// ids and its fingerprint cache usually skips the push entirely.
func TestListRegisteredFetchesFromDiscord(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true, Global: true})
	d.appID = "app123"
	d.commandsAPI = &fakeSession{registered: map[string][]*discordgo.ApplicationCommand{
		"": {{ID: "111", Name: "help"}},
	}}

	got, err := d.ListRegistered("")
	if err != nil {
		t.Fatalf("ListRegistered: %v", err)
	}
	if len(got) != 1 || got[0].Mention() != "</help:111>" {
		t.Fatalf("got %+v, want one </help:111>", got)
	}
}

func TestListRegisteredNotConfigured(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true})
	if _, err := d.ListRegistered(""); err == nil {
		t.Fatal("expected an error when appID is unset")
	}
}
