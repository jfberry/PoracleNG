package mappers

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestInfoMapperNoSubcommand(t *testing.T) {
	_, err := Info(nil)
	if err == nil {
		t.Fatal("expected error for /info with no sub-command")
	}
}

func TestInfoMapperPokemon(t *testing.T) {
	tokens, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{
			Type: discordgo.ApplicationCommandOptionSubCommand,
			Name: "pokemon",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Value: "pikachu"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "pikachu" {
		t.Errorf("got %v, want [pikachu]", tokens)
	}
}

func TestInfoMapperPokemonMissingName(t *testing.T) {
	_, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "pokemon"},
	})
	if err == nil {
		t.Fatal("expected error for /info pokemon with no name")
	}
}

func TestInfoMapperRarity(t *testing.T) {
	tokens, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "rarity"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "rarity" {
		t.Errorf("got %v, want [rarity]", tokens)
	}
}

func TestInfoMapperShiny(t *testing.T) {
	tokens, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "shiny"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "shiny" {
		t.Errorf("got %v, want [shiny]", tokens)
	}
}

func TestInfoMapperWeatherNoCoords(t *testing.T) {
	tokens, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "weather"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "weather" {
		t.Errorf("got %v, want [weather]", tokens)
	}
}

func TestInfoMapperWeatherWithCoords(t *testing.T) {
	tokens, err := Info([]*discordgo.ApplicationCommandInteractionDataOption{
		{
			Type: discordgo.ApplicationCommandOptionSubCommand,
			Name: "weather",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "coords", Value: "51.5,-0.1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[0] != "weather" || tokens[1] != "51.5,-0.1" {
		t.Errorf("got %v, want [weather 51.5,-0.1]", tokens)
	}
}

func TestLookupInfo(t *testing.T) {
	if Lookup("info") == nil {
		t.Fatal("nil mapper for /info")
	}
}
