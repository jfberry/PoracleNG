package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// subgroup builds a SubCommandGroup option carrying child sub-commands.
// Mirrors the discordgo shape that arrives when a user picks a
// /<command> <group> <sub> in the Discord client.
func subgroup(name string, children ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:    name,
		Type:    discordgo.ApplicationCommandOptionSubCommandGroup,
		Options: children,
	}
}

func TestSummaryMapperNoAlertType(t *testing.T) {
	_, err := Summary(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.summary.no_alert_type" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestSummaryMapperNoAction(t *testing.T) {
	_, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.summary.no_action" {
		t.Errorf("key=%q", me.Key)
	}
}

// /summary <alertType> show → bare alertType token; the text bot's
// SummaryCommand treats a bare alertType (no further args) as a status
// request and renders the current schedule + buffer count.
func TestSummaryMapperShow(t *testing.T) {
	tokens, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("show")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"quest"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestSummaryMapperSettime(t *testing.T) {
	tokens, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("settime", sopt("times", "weekday07:30-18:00"))),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"quest", "settime", "weekday07:30-18:00"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestSummaryMapperSettimeRequiresTimes(t *testing.T) {
	_, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("settime")),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.summary.no_times" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestSummaryMapperSettimeEmptyTimes(t *testing.T) {
	_, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("settime", sopt("times", ""))),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError for empty times, got %v", err)
	}
}

func TestSummaryMapperCleartime(t *testing.T) {
	tokens, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("cleartime")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"quest", "cleartime"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestSummaryMapperNow(t *testing.T) {
	tokens, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("now")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"quest", "now"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestSummaryMapperUnknownAction(t *testing.T) {
	_, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		subgroup("quest", subopt("bogus")),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.summary.unknown_action" {
		t.Errorf("key=%q", me.Key)
	}
}

// A non-SubCommandGroup top-level option (e.g. someone hand-rolling an
// interaction) should be rejected up-front rather than silently
// producing wrong tokens.
func TestSummaryMapperWrongTopLevelType(t *testing.T) {
	_, err := Summary([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("quest", "show"),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestLookupSummary(t *testing.T) {
	if Lookup("summary") == nil {
		t.Fatal("nil mapper for /summary")
	}
}
