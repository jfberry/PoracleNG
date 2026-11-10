package discordbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

type fakeChannels map[string]*discordgo.Channel

func (f fakeChannels) stateChannel(id string) (*discordgo.Channel, error) {
	if ch, ok := f[id]; ok {
		return ch, nil
	}
	return nil, discordgo.ErrStateNotFound
}

func (f fakeChannels) apiChannel(id string) (*discordgo.Channel, error) {
	return nil, discordgo.ErrStateNotFound
}

func TestChannelAncestors(t *testing.T) {
	chans := fakeChannels{
		"cat":    {ID: "cat"},
		"text":   {ID: "text", ParentID: "cat"},
		"thread": {ID: "thread", ParentID: "text"},
		"loose":  {ID: "loose"},
	}
	cases := map[string][]string{
		"text":    {"cat"},
		"thread":  {"text", "cat"},
		"loose":   nil,
		"unknown": nil,
	}
	for id, want := range cases {
		got := ancestorsVia(chans, id)
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", id, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", id, got, want)
			}
		}
	}
}
