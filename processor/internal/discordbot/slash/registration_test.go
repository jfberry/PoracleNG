package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

type fakeSession struct{ called []string }

func (f *fakeSession) ApplicationCommandBulkOverwrite(appID, guildID string, cmds []*discordgo.ApplicationCommand, opts ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
	f.called = append(f.called, guildID)
	return cmds, nil
}

func TestSyncGlobal(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true, Global: true})
	d.appID = "app123"
	fs := &fakeSession{}
	d.commandsAPI = fs

	err := d.SyncCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.called) != 1 || fs.called[0] != "" {
		t.Errorf("expected global call, got %v", fs.called)
	}
}

func TestSyncGuilds(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true, Global: false, Guilds: []string{"g1", "g2"}})
	d.appID = "app123"
	fs := &fakeSession{}
	d.commandsAPI = fs

	err := d.SyncCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.called) != 2 {
		t.Errorf("expected 2 guild calls, got %d", len(fs.called))
	}
}
