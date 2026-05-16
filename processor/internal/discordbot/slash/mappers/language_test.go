package mappers

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestLanguageMapperEmpty(t *testing.T) {
	tokens, err := Language(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty, got %v", tokens)
	}
}

func TestLanguageMapperWithCode(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name:  "code",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: "DE", // mixed case to verify lower-casing
		},
	}
	tokens, err := Language(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "de" {
		t.Errorf("expected [de], got %v", tokens)
	}
}

func TestLanguageMapperEmptyCodeString(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name:  "code",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: "",
		},
	}
	tokens, err := Language(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected empty for empty-string code, got %v", tokens)
	}
}

func TestLookupLanguage(t *testing.T) {
	if Lookup("language") == nil {
		t.Fatal("nil mapper for /language")
	}
}

func TestFlattenOptionsCommon(t *testing.T) {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "a", Value: "1"},
		{Name: "b", Value: "2"},
	}
	m := flattenOptions(opts)
	if len(m) != 2 || m["a"] == nil || m["b"] == nil {
		t.Errorf("flattenOptions: %+v", m)
	}
}
