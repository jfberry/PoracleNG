package enrichment

import (
	"reflect"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// forecastWeather is a WeatherProvider returning fixed cell weather and
// forecast values.
type forecastWeather struct {
	current  int
	forecast tracker.WeatherForecast
}

func (f *forecastWeather) GetCurrentWeatherInCell(string) int { return f.current }
func (f *forecastWeather) GetWeatherForecast(string) tracker.WeatherForecast {
	return f.forecast
}

// weatherTestGameData: pokemon 1 is grass (boosted by clear=1),
// util weather 1/2 carry emoji keys.
func weatherTestGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}: {PokemonID: 1, FormID: 0, Types: []int{12}},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			GenData: map[int]gamedata.GenInfo{1: {Roman: "I"}},
			Weather: map[int]gamedata.WeatherInfo{
				1: {Emoji: "weather-sunny"},
				2: {Emoji: "weather-rainy"},
			},
		},
	}
}

// #314: pokemon must produce boostingWeatherEmojiKeys so the documented
// {{boostingWeathersEmoji}} resolves (raid/maxbattle already did).
func TestPokemonGameData_BoostingWeatherEmojiKeys(t *testing.T) {
	e := &Enricher{WeatherProvider: &mockWeather{}, TimeLayout: "15:04:05", GameData: weatherTestGameData()}
	m := map[string]any{}
	e.enrichPokemonGameData(m, &webhook.PokemonWebhook{PokemonID: 1}, false)
	got, _ := m["boostingWeatherEmojiKeys"].([]string)
	if !reflect.DeepEqual(got, []string{"weather-sunny"}) {
		t.Errorf("boostingWeatherEmojiKeys = %v, want [weather-sunny]", m["boostingWeatherEmojiKeys"])
	}
}

func raidForecastEnricher(current int, fc tracker.WeatherForecast) *Enricher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"poke_1": "Bulbasaur"}))
	bundle.LinkFallbacks()
	return &Enricher{
		Translations:    bundle,
		DefaultLocale:   "en",
		WeatherProvider: &forecastWeather{current: current, forecast: fc},
		TimeLayout:      "15:04:05",
		DateLayout:      "2006-01-02",
		GameData:        weatherTestGameData(),
	}
}

// #314: a hatched raid whose boss's boost flips with the next-hour forecast
// gets weatherCurrent/weatherNext/weatherChangeTime, like pokemon.
func TestRaid_WeatherForecastChangeSetsFields(t *testing.T) {
	end := time.Now().Add(2 * time.Hour).Unix()
	raid := &webhook.RaidWebhook{PokemonID: 1, Level: 5, End: end, Start: end - 2700}

	// Currently clear (boosts grass) → rain next: boss loses its boost.
	e := raidForecastEnricher(1, tracker.WeatherForecast{Current: 1, Next: 2})
	m, _ := e.Raid(raid, true, 0)
	if m["weatherCurrent"] != 1 || m["weatherNext"] != 2 {
		t.Errorf("weatherCurrent/Next = %v/%v, want 1/2", m["weatherCurrent"], m["weatherNext"])
	}
	ts, _ := m["weatherChangeTime"].(string)
	if len(ts) != 5 { // HH:MM — seconds stripped
		t.Errorf("weatherChangeTime = %q, want HH:MM", ts)
	}

	// Not boosted now (rain), clear next: boss gains the boost.
	e = raidForecastEnricher(2, tracker.WeatherForecast{Current: 2, Next: 1})
	m, _ = e.Raid(raid, true, 0)
	if m["weatherCurrent"] != 2 || m["weatherNext"] != 1 {
		t.Errorf("gain boost: weatherCurrent/Next = %v/%v, want 2/1", m["weatherCurrent"], m["weatherNext"])
	}

	// Per-language enrichment now builds the names from weatherNext.
	pl := e.RaidTranslate(m, raid, "en")
	if pl["weatherNextEmojiKey"] != "weather-sunny" || pl["weatherCurrentEmojiKey"] != "weather-rainy" {
		t.Errorf("per-language forecast emoji keys = %v/%v", pl["weatherCurrentEmojiKey"], pl["weatherNextEmojiKey"])
	}
}

func TestRaid_WeatherForecastNoBoostChangeUnset(t *testing.T) {
	end := time.Now().Add(2 * time.Hour).Unix()
	raid := &webhook.RaidWebhook{PokemonID: 1, Level: 5, End: end, Start: end - 2700}
	// Clear → clear: no change.
	e := raidForecastEnricher(1, tracker.WeatherForecast{Current: 1, Next: 1})
	m, _ := e.Raid(raid, true, 0)
	for _, k := range []string{"weatherCurrent", "weatherNext", "weatherChangeTime"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s set to %v with no boost change", k, m[k])
		}
	}
	// Rain → windy: changes weather but the grass boss stays unboosted.
	e = raidForecastEnricher(2, tracker.WeatherForecast{Current: 2, Next: 5})
	m, _ = e.Raid(raid, true, 0)
	if _, ok := m["weatherNext"]; ok {
		t.Errorf("weatherNext set although boost doesn't change: %v", m["weatherNext"])
	}
}

func TestRaid_EggHasNoWeatherForecast(t *testing.T) {
	end := time.Now().Add(2 * time.Hour).Unix()
	egg := &webhook.RaidWebhook{PokemonID: 0, Level: 5, End: end, Start: end - 2700}
	e := raidForecastEnricher(1, tracker.WeatherForecast{Current: 1, Next: 2})
	m, _ := e.Raid(egg, true, 0)
	for _, k := range []string{"weatherCurrent", "weatherNext", "weatherChangeTime"} {
		if _, ok := m[k]; ok {
			t.Errorf("egg: %s set to %v", k, m[k])
		}
	}
}
