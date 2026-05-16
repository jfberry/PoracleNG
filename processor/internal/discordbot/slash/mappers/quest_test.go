package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestQuestMapperNoReward(t *testing.T) {
	_, err := Quest(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T", err)
	}
	if me.Key != "error.slash.quest.no_reward" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestQuestMapperEmptyStringsAreNoReward(t *testing.T) {
	// Empty strings count as zero values — must not satisfy hasNonZeroValue.
	_, err := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", ""),
		sopt("item", ""),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestQuestMapperMultipleRewards(t *testing.T) {
	_, err := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("stardust", 1000),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.quest.exactly_one_reward" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestQuestMapperPokemon(t *testing.T) {
	tokens, err := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "Pikachu"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestQuestMapperItem(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("item", "Golden Razz Berry"),
	})
	want := []string{"item:Golden Razz Berry"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestQuestMapperStardust(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("stardust", 1000),
	})
	if !reflect.DeepEqual(tokens, []string{"stardust:1000"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestQuestMapperCandy(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("candy", "pikachu"),
	})
	if !reflect.DeepEqual(tokens, []string{"candy:pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestQuestMapperMegaEnergy(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("mega_energy", "charizard"),
	})
	if !reflect.DeepEqual(tokens, []string{"energy:charizard"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestQuestMapperXLCandy(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("xl_candy", "pikachu"),
	})
	if !reflect.DeepEqual(tokens, []string{"xlcandy:pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestQuestMapperMinAmount(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("candy", "pikachu"),
		iopt("min_amount", 5),
	})
	want := []string{"candy:pikachu", "amount:5"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestQuestMapperAllExtras(t *testing.T) {
	tokens, err := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("min_amount", 3),
		iopt("distance", 750),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pikachu", "amount:3", "d750", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestQuestMapperMinAmountZeroOmitted(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("min_amount", 0),
	})
	if !reflect.DeepEqual(tokens, []string{"pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLookupQuest(t *testing.T) {
	if Lookup("quest") == nil {
		t.Fatal("nil mapper for /quest")
	}
}
