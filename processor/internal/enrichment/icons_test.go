package enrichment

import (
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// mockWeather implements WeatherProvider for tests.
type mockWeather struct{}

func (m *mockWeather) GetCurrentWeatherInCell(cellID string) int { return 0 }
func (m *mockWeather) GetWeatherForecast(cellID string) tracker.WeatherForecast {
	return tracker.WeatherForecast{}
}

func newTestEnricher() *Enricher {
	return &Enricher{
		ImgUicons:       testUicons(),
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
		DateLayout:      "2006-01-02",
	}
}

// --- Raid ---

func TestRaidIconURLs_HatchedRaid(t *testing.T) {
	e := newTestEnricher()

	raid := &webhook.RaidWebhook{
		PokemonID: 150,
		Form:      0,
		Level:     5,
		Latitude:  52.5,
		Longitude: 13.4,
		End:       time.Now().Add(30 * time.Minute).Unix(),
		Start:     time.Now().Add(-15 * time.Minute).Unix(),
	}
	m, _ := e.Raid(raid, true)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("hatched raid should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/pokemon/") {
		t.Errorf("hatched raid imgUrl = %q, want it to contain /pokemon/", imgUrl)
	}
}

func TestRaidIconURLs_Egg(t *testing.T) {
	e := newTestEnricher()

	raid := &webhook.RaidWebhook{
		PokemonID: 0, // egg (not hatched)
		Level:     5,
		Latitude:  52.5,
		Longitude: 13.4,
		Start:     time.Now().Add(15 * time.Minute).Unix(),
		End:       time.Now().Add(60 * time.Minute).Unix(),
	}
	m, _ := e.Raid(raid, true)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("egg should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/raid/egg/") {
		t.Errorf("egg imgUrl = %q, want it to contain /raid/egg/", imgUrl)
	}
}

func TestRaidIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
	}
	raid := &webhook.RaidWebhook{
		PokemonID: 150,
		Level:     5,
		Latitude:  52.5,
		Longitude: 13.4,
		End:       time.Now().Add(30 * time.Minute).Unix(),
		Start:     time.Now().Add(-15 * time.Minute).Unix(),
	}
	m, _ := e.Raid(raid, true)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

func TestRaidIconURLs_AltAndSticker(t *testing.T) {
	alt := uicons.NewWithIndex("https://alt.test", "png", &uicons.Index{
		Pokemon: map[string]bool{"150.png": true, "0.png": true},
	})
	sticker := uicons.NewWithIndex("https://sticker.test", "webp", &uicons.Index{
		Pokemon: map[string]bool{"150.webp": true, "0.webp": true},
	})
	e := &Enricher{
		ImgUicons:       testUicons(),
		ImgUiconsAlt:    alt,
		StickerUicons:   sticker,
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
	}
	raid := &webhook.RaidWebhook{
		PokemonID: 150,
		Level:     5,
		Latitude:  52.5,
		Longitude: 13.4,
		End:       time.Now().Add(30 * time.Minute).Unix(),
		Start:     time.Now().Add(-15 * time.Minute).Unix(),
	}
	m, _ := e.Raid(raid, true)

	if _, ok := m["imgUrl"].(string); !ok {
		t.Error("expected imgUrl")
	}
	if _, ok := m["imgUrlAlt"].(string); !ok {
		t.Error("expected imgUrlAlt")
	}
	if _, ok := m["stickerUrl"].(string); !ok {
		t.Error("expected stickerUrl")
	}
}

// --- Maxbattle ---

func TestMaxbattleIconURLs(t *testing.T) {
	e := newTestEnricher()

	mb := &webhook.MaxbattleWebhook{
		ID:                "mb1",
		BattlePokemonID:   25,
		BattlePokemonForm: 0,
		BattleEnd:         time.Now().Add(30 * time.Minute).Unix(),
	}
	m, _ := e.Maxbattle(52.5, 13.4, mb.BattleEnd, mb)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("maxbattle should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/pokemon/") {
		t.Errorf("maxbattle imgUrl = %q, want it to contain /pokemon/", imgUrl)
	}
}

func TestMaxbattleIconURLs_NoPokemon(t *testing.T) {
	e := newTestEnricher()

	mb := &webhook.MaxbattleWebhook{
		ID:              "mb1",
		BattlePokemonID: 0,
		BattleEnd:       time.Now().Add(30 * time.Minute).Unix(),
	}
	m, _ := e.Maxbattle(52.5, 13.4, mb.BattleEnd, mb)

	if _, ok := m["imgUrl"]; ok {
		t.Error("maxbattle with no pokemon should not have imgUrl")
	}
}

// --- Gym ---

func TestGymIconURLs(t *testing.T) {
	e := newTestEnricher()

	m, _ := e.Gym(52.5, 13.4, 1, 0, 3, false, false, "gym123")

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("gym should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/gym/") {
		t.Errorf("gym imgUrl = %q, want it to contain /gym/", imgUrl)
	}
}

func TestGymIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
	}
	m, _ := e.Gym(52.5, 13.4, 1, 0, 3, false, false, "gym123")

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

// --- Invasion ---

func TestInvasionIconURLs_RegularGrunt(t *testing.T) {
	e := newTestEnricher()

	m, _ := e.Invasion(52.5, 13.4, time.Now().Add(15*time.Minute).Unix(), "stop1", 41, 0, 0)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("invasion should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/invasion/") {
		t.Errorf("invasion imgUrl = %q, want it to contain /invasion/", imgUrl)
	}
}

func TestInvasionIconURLs_EventInvasion(t *testing.T) {
	e := newTestEnricher()

	// Event invasion: gruntTypeID=0, displayType>=7 uses pokestop icon
	m, _ := e.Invasion(52.5, 13.4, time.Now().Add(15*time.Minute).Unix(), "stop1", 0, 7, 501)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("event invasion should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/pokestop/") {
		t.Errorf("event invasion imgUrl = %q, want it to contain /pokestop/", imgUrl)
	}
}

func TestInvasionIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
	}
	m, _ := e.Invasion(52.5, 13.4, time.Now().Add(15*time.Minute).Unix(), "stop1", 41, 0, 0)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

// --- Lure ---

func TestLureIconURLs(t *testing.T) {
	e := newTestEnricher()

	lure := &webhook.LureWebhook{
		PokestopID:     "stop1",
		Latitude:       52.5,
		Longitude:      13.4,
		LureExpiration: time.Now().Add(30 * time.Minute).Unix(),
		LureID:         501,
	}
	m, _ := e.Lure(lure)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("lure should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/pokestop/") {
		t.Errorf("lure imgUrl = %q, want it to contain /pokestop/", imgUrl)
	}
}

func TestLureIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
	}
	lure := &webhook.LureWebhook{
		PokestopID:     "stop1",
		Latitude:       52.5,
		Longitude:      13.4,
		LureExpiration: time.Now().Add(30 * time.Minute).Unix(),
		LureID:         501,
	}
	m, _ := e.Lure(lure)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

// --- Nest ---

func TestNestIconURLs(t *testing.T) {
	e := newTestEnricher()

	nest := &webhook.NestWebhook{
		PokemonID: 25,
		Form:      0,
		Latitude:  52.5,
		Longitude: 13.4,
		ResetTime: time.Now().Unix(),
	}
	m, _ := e.Nest(nest)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("nest should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/pokemon/") {
		t.Errorf("nest imgUrl = %q, want it to contain /pokemon/", imgUrl)
	}
}

func TestNestIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
		DateLayout:      "2006-01-02",
	}
	nest := &webhook.NestWebhook{
		PokemonID: 25,
		Form:      0,
		Latitude:  52.5,
		Longitude: 13.4,
		ResetTime: time.Now().Unix(),
	}
	m, _ := e.Nest(nest)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

// --- Weather ---

func TestWeatherIconURLs(t *testing.T) {
	e := newTestEnricher()
	e.Translations = newTestBundle()
	e.GameData = newTestGameData()

	base, _ := e.Weather(52.5, 13.4, 3, nil, false)
	m, _ := e.WeatherTranslate(base, 1, 3, nil, "en", false)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("weather should have imgUrl")
	}
	if !strings.Contains(imgUrl, "/weather/") {
		t.Errorf("weather imgUrl = %q, want it to contain /weather/", imgUrl)
	}
}

func TestWeatherIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
		Translations:    newTestBundle(),
		GameData:        newTestGameData(),
	}
	base, _ := e.Weather(52.5, 13.4, 3, nil, false)
	m, _ := e.WeatherTranslate(base, 1, 3, nil, "en", false)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}
