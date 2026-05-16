package mappers

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestHelpMapperEmpty(t *testing.T) {
	tokens, err := Help(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %v", tokens)
	}
}

func TestHelpMapperWithTopic(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name:  "topic",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: "Track", // mixed case to verify lower-casing
		},
	}
	tokens, err := Help(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "track" {
		t.Errorf("expected [track], got %v", tokens)
	}
}

func TestHelpMapperEmptyTopicString(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name:  "topic",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: "",
		},
	}
	tokens, err := Help(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty for empty-string topic, got %v", tokens)
	}
}

func TestLookupHelp(t *testing.T) {
	if Lookup("help") == nil {
		t.Fatal("nil mapper for /help")
	}
}
