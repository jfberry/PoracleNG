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
	// Bare name (lowercased), no "item:" prefix — matchItemName resolves
	// translated item names from Unrecognized args and lowercases before
	// comparison, so passing it lowercased keeps the matcher happy
	// without forcing it to do the casing.
	want := []string{"golden razz berry"}
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

// xl_candy was removed from the /quest option list because the bot has
// no reward-type matcher for it. A flag-of-incompletion test confirms the
// reward-set guard rejects an XL-candy-like value if anyone reintroduces
// it without wiring the matcher first.
func TestQuestMapperRejectsRemovedXLCandy(t *testing.T) {
	// Set xl_candy via an unknown option — flattenOptions accepts any
	// name, but the mutual-exclusion guard's allow-list no longer
	// contains it, so an "xl_candy"-only invocation should report
	// no-reward.
	_, err := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("xl_candy", "pikachu"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError for xl_candy-only invocation, got %T", err)
	}
	if me.Key != "error.slash.quest.no_reward" {
		t.Errorf("key=%q, want error.slash.quest.no_reward", me.Key)
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

// /quest summary token mirrors the text bot's arg.summary keyword: when
// set, matching quests are buffered for scheduled batch delivery instead
// of alerting immediately. emitFlag accepts either a non-empty String
// value (the single-choice "Yes" path) or a Boolean true (legacy/parity
// fixtures), so both shapes route to the same `summary` token here.
func TestQuestMapperSummaryString(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		sopt("summary", "yes"),
	})
	want := []string{"pikachu", "summary"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestQuestMapperSummaryBool(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		bopt("summary", true),
	})
	want := []string{"pikachu", "summary"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestQuestMapperSummaryEmpty(t *testing.T) {
	tokens, _ := Quest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		sopt("summary", ""),
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
