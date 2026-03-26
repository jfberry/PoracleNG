package uicons

import (
	"strings"
	"testing"
)

func setFrom(items ...string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// newTestUicons creates a Uicons with a pre-loaded index (no HTTP fetch).
func newTestUicons(url, imageType string, idx *Index) *Uicons {
	url = strings.TrimRight(url, "/")
	u := &Uicons{
		url:       url,
		imageType: imageType,
	}
	if idx != nil {
		u.index.Store(idx)
	}
	return u
}

func TestResolvePokemonIconBasic(t *testing.T) {
	idx := &Index{
		Pokemon: setFrom("1.png", "1_f163.png", "6_f169_g1.png", "25.png", "25_s.png", "0.png"),
	}
	u := newTestUicons("https://example.com/icons", "png", idx)

	tests := []struct {
		name      string
		pokemonID int
		form      int
		evolution int
		gender    int
		costume   int
		alignment int
		shiny     bool
		want      string
	}{
		{"basic lookup", 1, 0, 0, 0, 0, 0, false, "https://example.com/icons/pokemon/1.png"},
		{"form lookup", 1, 163, 0, 0, 0, 0, false, "https://example.com/icons/pokemon/1_f163.png"},
		{"form fallback to base", 1, 999, 0, 0, 0, 0, false, "https://example.com/icons/pokemon/1.png"},
		{"shiny found", 25, 0, 0, 0, 0, 0, true, "https://example.com/icons/pokemon/25_s.png"},
		{"shiny fallback to non-shiny", 1, 0, 0, 0, 0, 0, true, "https://example.com/icons/pokemon/1.png"},
		{"gender match with form", 6, 169, 0, 1, 0, 0, false, "https://example.com/icons/pokemon/6_f169_g1.png"},
		{"unknown pokemon fallback to 0", 9999, 0, 0, 0, 0, 0, false, "https://example.com/icons/pokemon/0.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := u.PokemonIcon(tt.pokemonID, tt.form, tt.evolution, tt.gender, tt.costume, tt.alignment, tt.shiny)
			if got != tt.want {
				t.Errorf("PokemonIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePokemonIconFallbackPath(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", nil) // no index

	tests := []struct {
		name      string
		pokemonID int
		form      int
		evolution int
		want      string
	}{
		{"basic fallback", 1, 0, 0, "https://example.com/icons/pokemon_icon_001_00.png"},
		{"fallback with form", 25, 163, 0, "https://example.com/icons/pokemon_icon_025_163.png"},
		{"fallback with evolution", 3, 0, 1, "https://example.com/icons/pokemon_icon_003_00_1.png"},
		{"fallback with form and evolution", 6, 12, 2, "https://example.com/icons/pokemon_icon_006_12_2.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := u.PokemonIcon(tt.pokemonID, tt.form, tt.evolution, 0, 0, 0, false)
			if got != tt.want {
				t.Errorf("PokemonIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEggIcon(t *testing.T) {
	idx := &Index{
		Egg: setFrom("5.png", "5_h.png", "5_ex.png", "5_h_ex.png", "0.png"),
	}
	u := newTestUicons("https://example.com/icons", "png", idx)

	tests := []struct {
		name    string
		level   int
		hatched bool
		ex      bool
		want    string
	}{
		{"basic", 5, false, false, "https://example.com/icons/raid/egg/5.png"},
		{"hatched", 5, true, false, "https://example.com/icons/raid/egg/5_h.png"},
		{"ex", 5, false, true, "https://example.com/icons/raid/egg/5_ex.png"},
		{"hatched+ex", 5, true, true, "https://example.com/icons/raid/egg/5_h_ex.png"},
		{"unknown level", 99, false, false, "https://example.com/icons/raid/egg/0.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := u.EggIcon(tt.level, tt.hatched, tt.ex)
			if got != tt.want {
				t.Errorf("EggIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveGymIcon(t *testing.T) {
	idx := &Index{
		Gym: setFrom("1.png", "1_t3.png", "1_t3_b.png", "0.png"),
	}
	u := newTestUicons("https://example.com/icons", "png", idx)

	tests := []struct {
		name         string
		teamID       int
		trainerCount int
		inBattle     bool
		ex           bool
		want         string
	}{
		{"basic", 1, 0, false, false, "https://example.com/icons/gym/1.png"},
		{"trainers", 1, 3, false, false, "https://example.com/icons/gym/1_t3.png"},
		{"in battle", 1, 3, true, false, "https://example.com/icons/gym/1_t3_b.png"},
		{"unknown team", 99, 0, false, false, "https://example.com/icons/gym/0.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := u.GymIcon(tt.teamID, tt.trainerCount, tt.inBattle, tt.ex)
			if got != tt.want {
				t.Errorf("GymIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveWeatherIcon(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", &Index{
		Weather: setFrom("1.png", "3.png", "0.png"),
	})

	if got := u.WeatherIcon(3); got != "https://example.com/icons/weather/3.png" {
		t.Errorf("WeatherIcon(3) = %q", got)
	}
	if got := u.WeatherIcon(99); got != "https://example.com/icons/weather/0.png" {
		t.Errorf("WeatherIcon(99) = %q, want fallback", got)
	}
}

func TestResolveInvasionIcon(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", &Index{
		Invasion: setFrom("41.png", "0.png"),
	})

	if got := u.InvasionIcon(41); got != "https://example.com/icons/invasion/41.png" {
		t.Errorf("InvasionIcon(41) = %q", got)
	}
}

func TestResolvePokestopIcon(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", &Index{
		Pokestop: setFrom("0.png", "501.png", "0_i.png", "0_q.png"),
	})

	if got := u.PokestopIcon(0, false, 0, false); got != "https://example.com/icons/pokestop/0.png" {
		t.Errorf("PokestopIcon(basic) = %q", got)
	}
	if got := u.PokestopIcon(501, false, 0, false); got != "https://example.com/icons/pokestop/501.png" {
		t.Errorf("PokestopIcon(lure) = %q", got)
	}
	if got := u.PokestopIcon(0, true, 0, false); got != "https://example.com/icons/pokestop/0_i.png" {
		t.Errorf("PokestopIcon(invasion) = %q", got)
	}
	if got := u.PokestopIcon(0, false, 0, true); got != "https://example.com/icons/pokestop/0_q.png" {
		t.Errorf("PokestopIcon(quest) = %q", got)
	}
}

func TestResolveItemIcon(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", &Index{
		RewardItem: setFrom("1.png", "1_a5.png", "0.png"),
	})

	if got := u.RewardItemIcon(1, 0); got != "https://example.com/icons/reward/item/1.png" {
		t.Errorf("RewardItemIcon(1,0) = %q", got)
	}
	if got := u.RewardItemIcon(1, 5); got != "https://example.com/icons/reward/item/1_a5.png" {
		t.Errorf("RewardItemIcon(1,5) = %q", got)
	}
}

func TestPokemonIconComplexFallback(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", &Index{
		Pokemon: setFrom("150.png", "150_f1.png", "0.png"),
	})

	// form=1, gender=2 -> should fall back to form=1 (no gender match)
	if got := u.PokemonIcon(150, 1, 0, 2, 0, 0, false); got != "https://example.com/icons/pokemon/150_f1.png" {
		t.Errorf("got %q, want form fallback", got)
	}

	// form=999, gender=2 -> should fall back to base
	if got := u.PokemonIcon(150, 999, 0, 2, 0, 0, false); got != "https://example.com/icons/pokemon/150.png" {
		t.Errorf("got %q, want base fallback", got)
	}
}

func TestNoIndexReturnsEmpty(t *testing.T) {
	u := newTestUicons("https://example.com/icons", "png", nil)

	if got := u.GymIcon(1, 0, false, false); got != "" {
		t.Errorf("GymIcon without index = %q, want empty", got)
	}
	if got := u.WeatherIcon(1); got != "" {
		t.Errorf("WeatherIcon without index = %q, want empty", got)
	}
	if got := u.InvasionIcon(1); got != "" {
		t.Errorf("InvasionIcon without index = %q, want empty", got)
	}
}

func TestUrlTrailingSlashTrimmed(t *testing.T) {
	u := newTestUicons("https://example.com/icons/", "png", &Index{
		Pokemon: setFrom("1.png"),
	})

	got := u.PokemonIcon(1, 0, 0, 0, 0, 0, false)
	if got != "https://example.com/icons/pokemon/1.png" {
		t.Errorf("URL should have trailing slash trimmed, got %q", got)
	}
}

func TestWebpImageType(t *testing.T) {
	u := newTestUicons("https://example.com/stickers", "webp", &Index{
		Pokemon: setFrom("25.webp", "0.webp"),
	})

	got := u.PokemonIcon(25, 0, 0, 0, 0, 0, false)
	if got != "https://example.com/stickers/pokemon/25.webp" {
		t.Errorf("got %q, want webp extension", got)
	}
}

func TestLiveUiconsIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live test in short mode")
	}

	u := New("https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS", "png")
	// Give the initial fetch a moment
	idx := u.index.Load()
	if idx == nil {
		t.Fatal("failed to fetch live uicons index")
	}

	t.Logf("Live index: %d pokemon, %d gym, %d egg, %d weather icons",
		len(idx.Pokemon), len(idx.Gym), len(idx.Egg), len(idx.Weather))

	// Bulbasaur should exist
	got := u.PokemonIcon(1, 0, 0, 0, 0, 0, false)
	if got == "" || got == "https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS/pokemon/0.png" {
		t.Errorf("Bulbasaur icon resolved to fallback: %q", got)
	}
	t.Logf("Bulbasaur: %s", got)

	// Pikachu
	got = u.PokemonIcon(25, 0, 0, 0, 0, 0, false)
	t.Logf("Pikachu: %s", got)

	// Charizard with mega
	got = u.PokemonIcon(6, 0, 1, 0, 0, 0, false)
	t.Logf("Mega Charizard: %s", got)

	// Egg level 5
	got = u.EggIcon(5, false, false)
	t.Logf("Egg L5: %s", got)

	// Weather sunny
	got = u.WeatherIcon(1)
	t.Logf("Weather sunny: %s", got)
}
