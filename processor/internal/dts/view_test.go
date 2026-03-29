package dts

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeLayers(t *testing.T) {
	base := map[string]any{
		"pokemon_id": 25,
		"name":       "Pikachu",
		"iv":         100,
	}
	perLang := map[string]any{
		"name":     "Pikachu-DE",
		"formName": "Normal",
	}
	perUser := map[string]any{
		"distance": 500,
		"name":     "Pikachu-User",
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(base, perLang, perUser, "discord", nil)

	// Later layers win on conflict
	assert.Equal(t, "Pikachu-User", view["name"])
	// Base value preserved when not overridden
	assert.Equal(t, 25, view["pokemon_id"])
	assert.Equal(t, 100, view["iv"])
	// Per-lang value preserved when not overridden by per-user
	assert.Equal(t, "Normal", view["formName"])
	// Per-user value added
	assert.Equal(t, 500, view["distance"])
}

func TestEmojiResolution(t *testing.T) {
	defaults := map[string]string{
		"male":   "\u2642\ufe0f",
		"grass":  "\U0001F33F",
		"fire":   "\U0001F525",
		"sunny":  "\u2600\ufe0f",
		"north":  "\u2b06\ufe0f",
		"shiny":  "\u2728",
		"water":  "\U0001F4A7",
		"dragon": "\U0001F409",
	}
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: defaults,
	}

	base := map[string]any{
		"genderEmojiKey":         "male",
		"quickMoveTypeEmojiKey":  "grass",
		"chargeMoveTypeEmojiKey": "fire",
		"boostWeatherEmojiKey":   "sunny",
		"gameWeatherEmojiKey":    "sunny",
		"bearingEmojiKey":        "north",
		"shinyPossibleEmojiKey":  "shiny",
		"typeEmojiKeys":          []string{"water", "dragon"},
	}

	vb := NewViewBuilder(emoji, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	assert.Equal(t, "\u2642\ufe0f", view["genderEmoji"])
	assert.Equal(t, "\U0001F33F", view["quickMoveEmoji"])
	assert.Equal(t, "\U0001F525", view["chargeMoveEmoji"])
	assert.Equal(t, "\u2600\ufe0f", view["boostWeatherEmoji"])
	assert.Equal(t, "\u2600\ufe0f", view["gameWeatherEmoji"])
	assert.Equal(t, "\u2b06\ufe0f", view["bearingEmoji"])
	assert.Equal(t, "\u2728", view["shinyPossibleEmoji"])

	emojiList, ok := view["emoji"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"\U0001F4A7", "\U0001F409"}, emojiList)
	assert.Equal(t, "\U0001F4A7\U0001F409", view["emojiString"])
}

func TestEmojiResolutionWithAnySlice(t *testing.T) {
	defaults := map[string]string{
		"water": "\U0001F4A7",
	}
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: defaults,
	}

	// typeEmojiKeys arrives as []any from JSON unmarshalling
	base := map[string]any{
		"typeEmojiKeys": []any{"water"},
	}

	vb := NewViewBuilder(emoji, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	emojiList, ok := view["emoji"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"\U0001F4A7"}, emojiList)
}

func TestAliases(t *testing.T) {
	base := map[string]any{
		"formName":      "Alolan",
		"googleMapUrl":  "https://maps.google.com/?q=1,2",
		"appleMapUrl":   "https://maps.apple.com/?q=1,2",
		"ivColor":       "#A040A0",
		"disappearTime": "05:00:00",
		"atk":           15,
		"def":           14,
		"sta":           13,
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	assert.Equal(t, "Alolan", view["formname"])
	assert.Equal(t, "https://maps.google.com/?q=1,2", view["mapurl"])
	assert.Equal(t, "https://maps.apple.com/?q=1,2", view["applemap"])
	assert.Equal(t, "#A040A0", view["ivcolor"])
	assert.Equal(t, "05:00:00", view["distime"])
	assert.Equal(t, 15, view["individual_attack"])
	assert.Equal(t, 14, view["individual_defense"])
	assert.Equal(t, 13, view["individual_stamina"])
}

func TestComputedFields(t *testing.T) {
	base := map[string]any{
		"pokemon_id":    25,
		"disappearTime": "05:00:00",
		"tth": map[string]any{
			"hours":   1,
			"minutes": 30,
			"seconds": 45,
		},
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	assert.Equal(t, 25, view["id"])
	assert.Equal(t, "05:00:00", view["time"])
	assert.Equal(t, 1, view["tthh"])
	assert.Equal(t, 30, view["tthm"])
	assert.Equal(t, 45, view["tths"])
	assert.NotEmpty(t, view["now"])
	assert.NotEmpty(t, view["nowISO"])
}

func TestAreasFiltering(t *testing.T) {
	areas := []webhook.MatchedArea{
		{Name: "Berlin", DisplayInMatches: true},
		{Name: "HiddenArea", DisplayInMatches: false},
		{Name: "Hamburg", DisplayInMatches: true},
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(nil, nil, nil, "discord", areas)

	assert.Equal(t, "Berlin, Hamburg", view["areas"])
}

func TestAreasEmpty(t *testing.T) {
	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(nil, nil, nil, "discord", nil)

	assert.Equal(t, "", view["areas"])
}

func TestDTSDictionary(t *testing.T) {
	dict := map[string]any{
		"customField": "customValue",
		"pokemon_id":  999, // dictionary can override enrichment
	}

	base := map[string]any{
		"pokemon_id": 25,
	}

	vb := NewViewBuilder(nil, dict)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	assert.Equal(t, "customValue", view["customField"])
	assert.Equal(t, 999, view["pokemon_id"])
}

func TestEscapeUserContent(t *testing.T) {
	base := map[string]any{
		"pokestop_name": "Bob's \"Great\" Stop\nLine2",
		"pokestop_url":  "https://example.com/stop?name=\"test\"",
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	assert.Equal(t, "Bob's ''Great'' Stop Line2", view["pokestop_name"])
	assert.Equal(t, "https://example.com/stop?name=''test''", view["pokestop_url"])
}

func TestEscapeJSONString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`hello`, `hello`},
		{`say "hi"`, `say ''hi''`},
		{"line1\nline2", "line1 line2"},
		{"line1\r\nline2", "line1 line2"},
		{`path\to\file`, `path?to?file`},
		{`"quote" and\back\n`, `''quote'' and?back?n`}, // literal \n in source, not newline
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, escapeJSONString(tt.input), "input: %q", tt.input)
	}
}

func TestNilEmojiLookup(t *testing.T) {
	base := map[string]any{
		"genderEmojiKey": "male",
	}

	vb := NewViewBuilder(nil, nil)
	view := vb.BuildPokemonView(base, nil, nil, "discord", nil)

	// No emoji resolved, key just stays
	_, hasResolved := view["genderEmoji"]
	assert.False(t, hasResolved)
}

func TestOriginalMapsUnmodified(t *testing.T) {
	base := map[string]any{"pokemon_id": 25}
	perLang := map[string]any{"name": "Pikachu"}

	vb := NewViewBuilder(nil, nil)
	_ = vb.BuildPokemonView(base, perLang, nil, "discord", nil)

	// Original maps should not be mutated
	assert.Equal(t, map[string]any{"pokemon_id": 25}, base)
	assert.Equal(t, map[string]any{"name": "Pikachu"}, perLang)
}
