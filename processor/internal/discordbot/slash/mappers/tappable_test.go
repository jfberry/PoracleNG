package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestTappableMapperTokens(t *testing.T) {
	type opts = []*discordgo.ApplicationCommandInteractionDataOption
	cases := []struct {
		name string
		in   opts
		want []string
	}{
		{"candy any", opts{sopt("reward", "candy")}, []string{"candy"}},
		{"candy pokemon", opts{sopt("reward", "candy"), sopt("pokemon", "zubat")}, []string{"candy:zubat"}},
		{"xl candy", opts{sopt("reward", "xl_candy"), sopt("pokemon", "pikachu")}, []string{"xlcandy:pikachu"}},
		{"mega energy any", opts{sopt("reward", "mega_energy")}, []string{"energy"}},
		{"mega energy amount", opts{sopt("reward", "mega_energy"), sopt("pokemon", "charizard"), iopt("amount", 20)}, []string{"energy:charizard", "amount:20"}},
		{"item named", opts{sopt("reward", "item"), sopt("item", "Rare Candy")}, []string{"rare candy"}},
		{"item any", opts{sopt("reward", "item"), iopt("amount", 2)}, []string{"all items", "amount:2"}},
		{"stardust bare", opts{sopt("reward", "stardust")}, []string{"stardust"}},
		{"stardust min", opts{sopt("reward", "stardust"), iopt("amount", 500)}, []string{"stardust:500"}},
		{"pokecoins min", opts{sopt("reward", "pokecoins"), iopt("amount", 10)}, []string{"pokecoins:10"}},
		{"everything", opts{sopt("reward", "everything"), iopt("amount", 3)}, []string{"everything", "amount:3"}},
		{"tail", opts{sopt("reward", "candy"), iopt("distance", 500), bopt("clean", true), sopt("template", "fancy"), sopt("location", "home"), sopt("areas", "a,b")},
			[]string{"candy", "d500", "clean", "template:fancy", "location:home", "area:a,b"}},
	}
	for _, c := range cases {
		got, err := Tappable(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: tokens=%v want %v", c.name, got, c.want)
		}
	}
}

func TestTappableMapperErrors(t *testing.T) {
	type opts = []*discordgo.ApplicationCommandInteractionDataOption
	cases := []struct {
		in  opts
		key string
	}{
		{nil, "error.slash.tappable.no_reward"},
		{opts{sopt("reward", "stardust"), sopt("pokemon", "zubat")}, "error.slash.tappable.pokemon_not_applicable"},
		{opts{sopt("reward", "item"), sopt("pokemon", "zubat")}, "error.slash.tappable.pokemon_not_applicable"},
		{opts{sopt("reward", "candy"), sopt("item", "rare candy")}, "error.slash.tappable.item_not_applicable"},
	}
	for _, c := range cases {
		_, err := Tappable(c.in)
		me, ok := err.(*MapperError)
		if !ok || me.Key != c.key {
			t.Errorf("%v: err=%v want %s", c.in, err, c.key)
		}
	}
}

func TestLookupTappable(t *testing.T) {
	if Lookup("tappable") == nil {
		t.Fatal("nil mapper for /tappable")
	}
}
