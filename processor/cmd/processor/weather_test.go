package main

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// weatherAlertCleanUntil must prefer max(ActivePokemons.DisappearTime)
// so weather alerts auto-delete with the pokemon they mention, not
// with the user's cell-wide care window (which is the max across every
// pokemon ever registered and never shrinks). Falls back to CaresUntil
// when no active-pokemon data is available (show_altered_pokemon off).
func TestWeatherAlertCleanUntil(t *testing.T) {
	cases := []struct {
		name string
		user webhook.MatchedUser
		want int64
	}{
		{
			name: "no active pokemon → falls back to CaresUntil",
			user: webhook.MatchedUser{CaresUntil: 1700000000},
			want: 1700000000,
		},
		{
			name: "single active pokemon → uses its DisappearTime",
			user: webhook.MatchedUser{
				CaresUntil:     2000000000, // longer-lived pokemon registered earlier, already despawned
				ActivePokemons: []webhook.ActivePokemonEntry{{DisappearTime: 1700000000}},
			},
			want: 1700000000,
		},
		{
			name: "multiple active pokemon → max(DisappearTime)",
			user: webhook.MatchedUser{
				CaresUntil: 3000000000, // stale, ignored when ActivePokemons present
				ActivePokemons: []webhook.ActivePokemonEntry{
					{DisappearTime: 1700000000},
					{DisappearTime: 1700001800}, // 30min later
					{DisappearTime: 1700000900},
				},
			},
			want: 1700001800,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := weatherAlertCleanUntil(c.user)
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}
