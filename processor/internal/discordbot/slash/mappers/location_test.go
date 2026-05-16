package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func TestLocationMapperEmpty(t *testing.T) {
	_, err := Location(nil, nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.empty" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperBlankPlace(t *testing.T) {
	// Whitespace-only place is also "empty".
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "   "),
	}, nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.empty" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperCoordsNoSpace(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "51.28,1.08"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"51.28,1.08"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLocationMapperCoordsWithSpace(t *testing.T) {
	// Pasted from Google Maps with a space after the comma — we re-pack to
	// the canonical "lat,lon" form expected by the text bot's latLonRe.
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "51.28, 1.08"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"51.28,1.08"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLocationMapperNegativeCoords(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "-34.6037,-58.3816"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"-34.6037,-58.3816"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLocationMapperPlaceNameWithoutGeocoder(t *testing.T) {
	// Free-form place name with no deps → no geocoder error.
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "London"),
	}, nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.no_geocoder" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperPlaceNameDepsWithoutGeocoder(t *testing.T) {
	// deps present but Geocoder field is nil → same no_geocoder error.
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("place", "London"),
	}, &bot.BotDeps{})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.no_geocoder" {
		t.Errorf("key=%q", me.Key)
	}
}

// Location is NOT registered in the shared mapper registry — the dispatcher
// special-cases it because the func signature includes BotDeps.
func TestLocationNotInRegistry(t *testing.T) {
	if Lookup("location") != nil {
		t.Fatal("/location must not be in the shared registry; it has a non-standard signature")
	}
}
