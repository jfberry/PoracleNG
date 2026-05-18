package dts

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestView(t *testing.T, opts ...func(*testViewOpts)) *LayeredView {
	t.Helper()
	o := &testViewOpts{
		templateType: "monster",
		platform:     "discord",
	}
	for _, fn := range opts {
		fn(o)
	}
	return NewLayeredView(
		NewViewBuilder(o.emoji, o.dtsDict),
		o.templateType,
		o.base, o.perLang, o.perUser, o.webhook,
		o.platform, o.areas,
	)
}

type testViewOpts struct {
	templateType string
	platform     string
	base         map[string]any
	perLang      map[string]any
	perUser      map[string]any
	webhook      map[string]any
	emoji        *EmojiLookup
	dtsDict      map[string]any
	areas        []webhook.MatchedArea
}

// --- Layer priority tests ---

func TestLayeredView_PerUserWins(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"name": "Base"}
		o.perLang = map[string]any{"name": "PerLang"}
		o.perUser = map[string]any{"name": "PerUser"}
	})
	v, ok := lv.GetField("name")
	require.True(t, ok)
	assert.Equal(t, "PerUser", v)
}

func TestLayeredView_PerLangOverBase(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"name": "Base"}
		o.perLang = map[string]any{"name": "German"}
	})
	v, ok := lv.GetField("name")
	require.True(t, ok)
	assert.Equal(t, "German", v)
}

func TestLayeredView_BaseReturned(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"iv": 100}
	})
	v, ok := lv.GetField("iv")
	require.True(t, ok)
	assert.Equal(t, 100, v)
}

func TestLayeredView_WebhookFallback(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"iv": 100}
		o.webhook = map[string]any{"spawnpoint_id": "abc123"}
	})
	v, ok := lv.GetField("spawnpoint_id")
	require.True(t, ok)
	assert.Equal(t, "abc123", v)

	// Not found at all
	_, ok = lv.GetField("nonexistent")
	assert.False(t, ok)
}

func TestLayeredView_DTSDictionary(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.dtsDict = map[string]any{"prefix": "!"}
	})
	v, ok := lv.GetField("prefix")
	require.True(t, ok)
	assert.Equal(t, "!", v)
}

func TestLayeredView_DTSDictLowestPriority(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"field": "base"}
		o.dtsDict = map[string]any{"field": "dict"}
	})
	v, ok := lv.GetField("field")
	require.True(t, ok)
	assert.Equal(t, "base", v)
}

// --- Computed fields ---

func TestLayeredView_ComputedID(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"pokemon_id": 25}
	})
	v, ok := lv.GetField("id")
	require.True(t, ok)
	assert.Equal(t, 25, v)
}

func TestLayeredView_ComputedTime(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"disappearTime": "14:30:00"}
	})
	v, ok := lv.GetField("time")
	require.True(t, ok)
	assert.Equal(t, "14:30:00", v)
}

func TestLayeredView_ComputedTTH_Struct(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"tth": geo.TTH{Days: 0, Hours: 1, Minutes: 30, Seconds: 45},
		}
	})
	v, _ := lv.GetField("tthh")
	assert.Equal(t, 1, v)
	v, _ = lv.GetField("tthm")
	assert.Equal(t, 30, v)
	v, _ = lv.GetField("tths")
	assert.Equal(t, 45, v)
}

func TestLayeredView_ComputedTTH_Map(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"tth": map[string]any{"hours": 2, "minutes": 15, "seconds": 0},
		}
	})
	v, _ := lv.GetField("tthh")
	assert.Equal(t, 2, v)
	v, _ = lv.GetField("tthm")
	assert.Equal(t, 15, v)
}

func TestLayeredView_ComputedNowISO(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{}
	})
	v, ok := lv.GetField("nowISO")
	require.True(t, ok)
	assert.NotEmpty(t, v)
}

func TestLayeredView_ComputedAreas(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{}
		o.areas = []webhook.MatchedArea{
			{Name: "Berlin", DisplayInMatches: true},
			{Name: "Hidden", DisplayInMatches: false},
			{Name: "Hamburg", DisplayInMatches: true},
		}
	})
	v, ok := lv.GetField("areas")
	require.True(t, ok)
	assert.Equal(t, "Berlin, Hamburg", v)
}

func TestLayeredView_ComputedAreasEmpty(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{}
	})
	v, ok := lv.GetField("areas")
	require.True(t, ok)
	assert.Equal(t, "", v)
}

// TestLayeredView_BearingEmojiFromPerUser verifies that emoji keys living
// in the per-user enrichment layer (such as bearingEmojiKey, populated by
// PokemonPerUser) resolve to their platform-specific glyph. The previous
// resolveEmojiMap only walked base+perLang, leaving {{bearingEmoji}}
// empty for every pokemon alert.
func TestLayeredView_BearingEmojiFromPerUser(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"northeast": "↗️"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.perUser = map[string]any{"bearingEmojiKey": "northeast"}
	})
	v, ok := lv.GetField("bearingEmoji")
	require.True(t, ok, "bearingEmoji should resolve from perUser.bearingEmojiKey")
	assert.Equal(t, "↗️", v)
}

// TestLayeredView_PerUserEmojiOverridesBase proves the resolution order is
// perUser → perLang → base, so a per-user override wins even if the base
// layer also happens to carry the same key.
func TestLayeredView_PerUserEmojiOverridesBase(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"north": "⬆️", "south": "⬇️"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{"bearingEmojiKey": "north"}
		o.perUser = map[string]any{"bearingEmojiKey": "south"}
	})
	v, _ := lv.GetField("bearingEmoji")
	assert.Equal(t, "⬇️", v, "perUser should win")
}

func TestLayeredView_ComputedGenderData(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"male": "♂️"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{"genderEmojiKey": "male"}
		o.perLang = map[string]any{"genderName": "Male"}
	})
	v, ok := lv.GetField("genderData")
	require.True(t, ok)
	gd, ok := v.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Male", gd["name"])
	assert.Equal(t, "♂️", gd["emoji"])
}

func TestLayeredView_ComputedMegaName(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.templateType = "raid"
		o.perLang = map[string]any{"megaName": "Mega Charizard X"}
	})
	v, ok := lv.GetField("megaName")
	require.True(t, ok)
	assert.Equal(t, "Mega Charizard X", v)
}

// --- Aliases ---

func TestLayeredView_CommonAlias(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"googleMapUrl": "https://maps.google.com/?q=1,2"}
	})
	v, ok := lv.GetField("mapurl")
	require.True(t, ok)
	assert.Equal(t, "https://maps.google.com/?q=1,2", v)
}

func TestLayeredView_PokemonAliases(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"formName":      "Alolan",
			"ivColor":       "#A040A0",
			"disappearTime": "05:00:00",
		}
		// atk/def/sta come from enrichment
		o.perUser = map[string]any{"atk": 15, "def": 14, "sta": 13}
	})
	v, _ := lv.GetField("formname")
	assert.Equal(t, "Alolan", v)
	v, _ = lv.GetField("ivcolor")
	assert.Equal(t, "#A040A0", v)
	v, _ = lv.GetField("distime")
	assert.Equal(t, "05:00:00", v)
	v, _ = lv.GetField("individual_attack")
	assert.Equal(t, 15, v)
	v, _ = lv.GetField("individual_defense")
	assert.Equal(t, 14, v)
	v, _ = lv.GetField("individual_stamina")
	assert.Equal(t, 13, v)
}

func TestLayeredView_RaidAliases(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.templateType = "raid"
		o.base = map[string]any{
			"gym_name": "Central Gym",
			"gym_url":  "http://example.com/gym",
		}
	})
	v, _ := lv.GetField("gymName")
	assert.Equal(t, "Central Gym", v)
	v, _ = lv.GetField("gymUrl")
	assert.Equal(t, "http://example.com/gym", v)
}

func TestLayeredView_GymAliases(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.templateType = "gym"
		o.base = map[string]any{"name": "Park Gym"}
	})
	// gym type: gymName → name
	v, _ := lv.GetField("gymName")
	assert.Equal(t, "Park Gym", v)
}

func TestLayeredView_AliasDoesNotOverrideExisting(t *testing.T) {
	// If both "formname" and "formName" exist, the alias lookup
	// should not be reached because "formname" would need to not exist
	// in any layer for the alias to fire.
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"formName": "Alolan",
		}
		// perUser has "formname" set directly — this takes priority over alias
		o.perUser = map[string]any{"formname": "DirectValue"}
	})
	v, ok := lv.GetField("formname")
	require.True(t, ok)
	assert.Equal(t, "DirectValue", v)
}

// --- Emoji resolution ---

func TestLayeredView_EmojiResolution(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"male": "♂️", "grass": "🌿", "fire": "🔥"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{
			"genderEmojiKey":         "male",
			"quickMoveTypeEmojiKey":  "grass",
			"chargeMoveTypeEmojiKey": "fire",
		}
	})
	v, ok := lv.GetField("genderEmoji")
	require.True(t, ok)
	assert.Equal(t, "♂️", v)

	v, _ = lv.GetField("quickMoveEmoji")
	assert.Equal(t, "🌿", v)

	v, _ = lv.GetField("chargeMoveEmoji")
	assert.Equal(t, "🔥", v)
}

func TestLayeredView_EmojiArrayResolution(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"water": "💧", "dragon": "🐉"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{
			"typeEmojiKeys": []string{"water", "dragon"},
		}
	})

	// emojiString is the joined string
	v, ok := lv.GetField("emojiString")
	require.True(t, ok)
	assert.Equal(t, "💧🐉", v)

	// emoji is the []string array (in computed layer)
	v, ok = lv.GetField("emoji")
	require.True(t, ok)
	arr, ok := v.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"💧", "🐉"}, arr)
}

func TestLayeredView_EmojiFromAnySlice(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"water": "💧"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{
			"typeEmojiKeys": []any{"water"},
		}
	})
	v, ok := lv.GetField("emoji")
	require.True(t, ok)
	arr, ok := v.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"💧"}, arr)
}

func TestLayeredView_NilEmojiLookup(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"genderEmojiKey": "male"}
	})
	_, ok := lv.GetField("genderEmoji")
	assert.False(t, ok)
}

// --- Original maps unmodified ---

func TestLayeredView_OriginalMapsUnmodified(t *testing.T) {
	base := map[string]any{"pokemon_id": 25}
	perLang := map[string]any{"name": "Pikachu"}
	webhook := map[string]any{"spawnpoint_id": "abc"}

	vb := NewViewBuilder(nil, nil)
	lv := NewLayeredView(vb, "pokemon", base, perLang, nil, webhook, "discord", nil)

	// Read some fields to trigger resolution
	lv.GetField("id")
	lv.GetField("name")

	// Original maps must not be mutated
	assert.Equal(t, map[string]any{"pokemon_id": 25}, base)
	assert.Equal(t, map[string]any{"name": "Pikachu"}, perLang)
	assert.Equal(t, map[string]any{"spawnpoint_id": "abc"}, webhook)
}

// --- Type-specific alias isolation ---

func TestLayeredView_AliasIsolationByType(t *testing.T) {
	// "gymName" maps to "gym_name" for raids but "name" for gyms
	raidView := newTestView(t, func(o *testViewOpts) {
		o.templateType = "raid"
		o.base = map[string]any{"gym_name": "Raid Gym", "name": "Charizard"}
	})
	v, _ := raidView.GetField("gymName")
	assert.Equal(t, "Raid Gym", v)

	gymView := newTestView(t, func(o *testViewOpts) {
		o.templateType = "gym"
		o.base = map[string]any{"name": "Gym Name"}
	})
	v, _ = gymView.GetField("gymName")
	assert.Equal(t, "Gym Name", v)
}

// --- Webhook layer for custom DTS fields ---

func TestLayeredView_WebhookCustomFields(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{}
		o.webhook = map[string]any{
			"username":     "scanner1",
			"display_form": 42,
		}
	})
	v, ok := lv.GetField("username")
	require.True(t, ok)
	assert.Equal(t, "scanner1", v)

	v, ok = lv.GetField("display_form")
	require.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestLayeredView_BaseOverridesWebhook(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"pokemon_id": 25}
		o.webhook = map[string]any{"pokemon_id": 26}
	})
	v, _ := lv.GetField("pokemon_id")
	assert.Equal(t, 25, v)
}

// --- User content escaping ---

func TestLayeredView_EscapeUserContent(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"pokestop_name": "Bob's \"Great\" Stop\nLine2",
			"pokestop_url":  "https://example.com/stop?name=\"test\"",
		}
	})

	v, ok := lv.GetField("pokestop_name")
	require.True(t, ok)
	assert.Equal(t, "Bob's ''Great'' Stop Line2", v)

	v, ok = lv.GetField("pokestop_url")
	require.True(t, ok)
	assert.Equal(t, "https://example.com/stop?name=''test''", v)
}

func TestLayeredView_EscapeFromWebhook(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{}
		o.webhook = map[string]any{
			"gym_name": "Gym \"With\" Quotes",
		}
	})

	v, ok := lv.GetField("gym_name")
	require.True(t, ok)
	assert.Equal(t, "Gym ''With'' Quotes", v)
}

// TestLayeredView_EscapeAllScannerNameFields locks the full set of
// scanner-derived text fields that produce JSON inside templates and
// must therefore be escaped. Mirrors PoracleJS's escapeJsonString
// coverage so a regression here would re-introduce broken-JSON bugs.
func TestLayeredView_EscapeAllScannerNameFields(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{
			"nest_name":      "Park \"East\"",
			"description":    "Has a \"plaque\" on it",
			"oldName":        "Old \"Stop\"",
			"newName":        "New \"Stop\"",
			"oldDescription": "Old \"plaque\"",
			"newDescription": "New \"plaque\"",
		}
	})

	cases := map[string]string{
		"nest_name":      "Park ''East''",
		"description":    "Has a ''plaque'' on it",
		"oldName":        "Old ''Stop''",
		"newName":        "New ''Stop''",
		"oldDescription": "Old ''plaque''",
		"newDescription": "New ''plaque''",
	}
	for field, want := range cases {
		got, ok := lv.GetField(field)
		require.True(t, ok, "field %s missing", field)
		assert.Equal(t, want, got, "field %s", field)
	}
}

// --- Nil layer handling ---

func TestLayeredView_AllNilLayers(t *testing.T) {
	vb := NewViewBuilder(nil, nil)
	lv := NewLayeredView(vb, "pokemon", nil, nil, nil, nil, "discord", nil)

	_, ok := lv.GetField("anything")
	assert.False(t, ok)

	// Areas should still return empty string (computed)
	v, ok := lv.GetField("areas")
	require.True(t, ok)
	assert.Equal(t, "", v)
}

// TestLayeredView_WeatherChangeAliases verifies the lowercase template
// fields used in legacy / hand-written weather alerts ({{weather}},
// {{oldweather}}, {{weatheremoji}}, {{oldweatheremoji}}) resolve to the
// canonical enrichment outputs.
func TestLayeredView_WeatherChangeAliases(t *testing.T) {
	emoji := &EmojiLookup{
		defaults: map[string]string{
			"weather-rain":  "🌧️",
			"weather-clear": "☀️",
		},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.templateType = "weatherchange"
		o.base = map[string]any{}
		o.perLang = map[string]any{
			"weatherName":        "Rain",
			"oldWeatherName":     "Clear",
			"weatherEmojiKey":    "weather-rain",
			"oldWeatherEmojiKey": "weather-clear",
		}
		o.emoji = emoji
	})

	for _, tc := range []struct {
		alias string
		want  any
	}{
		{"weather", "Rain"},
		{"oldweather", "Clear"},
		{"weathername", "Rain"},
		{"oldweathername", "Clear"},
		{"weatheremoji", "🌧️"},
		{"oldweatheremoji", "☀️"},
	} {
		v, ok := lv.GetField(tc.alias)
		require.Truef(t, ok, "alias %q should resolve", tc.alias)
		assert.Equalf(t, tc.want, v, "alias %q", tc.alias)
	}
}

// TestLayeredView_BoostingWeathersEmoji locks {{boostingWeathersEmoji}}
// — the field every long-running pokemon / raid template references.
// resolveEmojiMap joins the per-platform emoji for every weather that
// boosts the pokemon's types into one string.
func TestLayeredView_BoostingWeathersEmoji(t *testing.T) {
	emoji := &EmojiLookup{
		custom: make(map[string]map[string]string),
		defaults: map[string]string{
			"weather_3": "☀️",
			"weather_5": "💨",
		},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.base = map[string]any{
			"boostingWeatherEmojiKeys": []string{"weather_3", "weather_5"},
		}
	})

	v, ok := lv.GetField("boostingWeathersEmoji")
	require.True(t, ok)
	assert.Equal(t, "☀️💨", v)
}

// TestLayeredView_WeaknessEmojiFlatString locks the {{weaknessEmoji}}
// flat string. Templates that don't iterate the structured weaknessList
// (e.g. NPlumb's dts_all.json) reference {{weaknessEmoji}} directly.
func TestLayeredView_WeaknessEmojiFlatString(t *testing.T) {
	emoji := &EmojiLookup{
		custom: make(map[string]map[string]string),
		defaults: map[string]string{
			"poke_type_water":    "💧",
			"poke_type_electric": "⚡",
			"poke_type_rock":     "🪨",
		},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.perLang = map[string]any{
			"weaknessList": []map[string]any{
				{
					"value": 2.0,
					"types": []map[string]any{
						{"emojiKey": "poke_type_water"},
						{"emojiKey": "poke_type_electric"},
					},
				},
				{
					"value": 4.0,
					"types": []map[string]any{
						{"emojiKey": "poke_type_rock"},
					},
				},
			},
		}
	})
	v, ok := lv.GetField("weaknessEmoji")
	require.True(t, ok, "weaknessEmoji should resolve")
	assert.Equal(t, "2x💧⚡ 4x🪨 ", v)
}

// TestLayeredView_WeaknessEmojiSkipEmpty confirms that categories with
// no resolvable type emoji are excluded from the flat string — matches
// PoracleJS's `if (info.types.length)` guard in raid.js.
func TestLayeredView_WeaknessEmojiSkipEmpty(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"poke_type_fire": "🔥"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.perLang = map[string]any{
			"weaknessList": []map[string]any{
				{
					"value": 0.5,
					"types": []map[string]any{}, // empty — should be skipped
				},
				{
					"value": 2.0,
					"types": []map[string]any{
						{"emojiKey": "poke_type_fire"},
					},
				},
			},
		}
	})
	v, ok := lv.GetField("weaknessEmoji")
	require.True(t, ok)
	assert.Equal(t, "2x🔥 ", v)
}

// TestLayeredView_Original verifies the {{original.X}} sub-resolver: the
// `original` field returns the prior-sighting map verbatim so templates
// can dereference into it (e.g. {{original.fullName}}, {{original.cp}}).
// The map must not be shadowed by any other layer that happens to hold an
// "original" key.
func TestLayeredView_Original(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"name": "Sinistea"}
	})
	lv.original = map[string]any{"name": "Polteageist", "cp": 1500}

	// Direct field outside `original` resolves through the normal cascade.
	v, ok := lv.GetField("name")
	require.True(t, ok)
	assert.Equal(t, "Sinistea", v)

	// `original` returns the nested map verbatim for raymond to recurse into.
	v, ok = lv.GetField("original")
	require.True(t, ok)
	m, ok := v.(map[string]any)
	require.True(t, ok, "original = %T, want map[string]any", v)
	assert.Equal(t, "Polteageist", m["name"])
	assert.Equal(t, 1500, m["cp"])
}

// TestLayeredView_OriginalNilWhenAbsent confirms that an unset `original`
// is reported as not-found so templates can guard with {{#if original}}.
func TestLayeredView_OriginalNilWhenAbsent(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.base = map[string]any{"name": "Sinistea"}
	})

	v, ok := lv.GetField("original")
	assert.False(t, ok, "original should not resolve when unset")
	assert.Nil(t, v)
}

// TestLayeredView_GymUrlAliasFromWebhook covers the gym_details photo URL:
// the gym webhook ships it as `url`, the gym alias table maps `gymUrl` →
// `url`, and the value flows through the raw-webhook fallback layer.
// Regression guard for the GymWebhook.URL field that was missing from
// the struct until the webhook's `url` field was wired up.
func TestLayeredView_GymUrlAliasFromWebhook(t *testing.T) {
	lv := newTestView(t, func(o *testViewOpts) {
		o.templateType = "gym"
		o.base = map[string]any{}
		o.webhook = map[string]any{
			"url": "https://lh3.googleusercontent.com/abc123",
		}
	})
	v, ok := lv.GetField("gymUrl")
	require.True(t, ok)
	assert.Equal(t, "https://lh3.googleusercontent.com/abc123", v)
}

// TestResolveWeaknessEmojis_DoesNotMutateInput verifies that
// resolveWeaknessEmojis leaves the original perLang weaknessList maps
// untouched. This locks the fix for the "concurrent map writes" panic that
// occurred when two render workers processed different RenderJobs sharing
// the same *Enrichment (e.g. raid-rsvp's fullUsers + rsvpUsers partition):
// the old in-place writes raced fatally at layered_view.go:312.
func TestResolveWeaknessEmojis_DoesNotMutateInput(t *testing.T) {
	emojiLookup := &EmojiLookup{
		custom: make(map[string]map[string]string),
		defaults: map[string]string{
			"poke_type_water":    "💧",
			"poke_type_electric": "⚡",
		},
	}

	// Build the shared weakness list exactly as enrichment produces it —
	// category maps with nested type-entry maps but no "emoji"/"typeEmoji".
	sharedWeakness := []map[string]any{
		{
			"value": 2.0,
			"types": []map[string]any{
				{"emojiKey": "poke_type_water", "name": "Water"},
				{"emojiKey": "poke_type_electric", "name": "Electric"},
			},
		},
	}
	perLang := map[string]any{
		"weaknessList": sharedWeakness,
	}

	// Snapshot the original state of every map we want to protect.
	origCat := map[string]any{
		"value": 2.0,
		"types": []map[string]any{
			{"emojiKey": "poke_type_water", "name": "Water"},
			{"emojiKey": "poke_type_electric", "name": "Electric"},
		},
	}
	origEntry0 := map[string]any{"emojiKey": "poke_type_water", "name": "Water"}
	origEntry1 := map[string]any{"emojiKey": "poke_type_electric", "name": "Electric"}

	computed := make(map[string]any)
	localOverrides := make(map[string]any)
	resolveWeaknessEmojis(emojiLookup, perLang, "discord", computed, localOverrides)

	// The shared perLang map must not have gained any new keys.
	assert.Equal(t, map[string]any{"weaknessList": sharedWeakness}, perLang,
		"perLang must not be mutated")

	// The shared category map must remain unchanged.
	cat := sharedWeakness[0]
	assert.Equal(t, origCat["value"], cat["value"])
	_, hasTypeEmoji := cat["typeEmoji"]
	assert.False(t, hasTypeEmoji, "shared category map must not have typeEmoji added")

	// The shared type-entry maps must not have gained "emoji".
	types := sharedWeakness[0]["types"].([]map[string]any)
	assert.Equal(t, origEntry0, types[0], "shared type entry 0 must not be mutated")
	assert.Equal(t, origEntry1, types[1], "shared type entry 1 must not be mutated")

	// The resolved list must land in localOverrides, not perLang.
	resolvedRaw, ok := localOverrides["weaknessList"]
	require.True(t, ok, "localOverrides must contain resolved weaknessList")
	resolved, ok := resolvedRaw.([]map[string]any)
	require.True(t, ok)
	require.Len(t, resolved, 1)

	// Resolved category has typeEmoji.
	resolvedCat := resolved[0]
	assert.Equal(t, "💧⚡", resolvedCat["typeEmoji"])

	// Resolved type entries have emoji.
	resolvedTypes := resolvedCat["types"].([]map[string]any)
	assert.Equal(t, "💧", resolvedTypes[0]["emoji"])
	assert.Equal(t, "⚡", resolvedTypes[1]["emoji"])

	// flat weaknessEmoji lands in computed.
	assert.Equal(t, "2x💧⚡ ", computed["weaknessEmoji"])
}

// TestLayeredView_WeaknessList_HasEmojiFields verifies that the localOverrides
// layer (checked before perLang in GetField) supplies the resolved weaknessList
// — the one with "emoji" on type entries and "typeEmoji" on categories. This
// is the template-author-visible contract: {{#each weaknessList}}...{{typeEmoji}}
// must return the platform-resolved string, not the empty string from the shared
// un-resolved perLang copy.
func TestLayeredView_WeaknessList_HasEmojiFields(t *testing.T) {
	emoji := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: map[string]string{"poke_type_grass": "🌿"},
	}
	lv := newTestView(t, func(o *testViewOpts) {
		o.emoji = emoji
		o.perLang = map[string]any{
			"weaknessList": []map[string]any{
				{
					"value": 2.0,
					"types": []map[string]any{
						{"emojiKey": "poke_type_grass", "name": "Grass"},
					},
				},
			},
		}
	})

	v, ok := lv.GetField("weaknessList")
	require.True(t, ok, "weaknessList must resolve")
	list, ok := v.([]map[string]any)
	require.True(t, ok, "weaknessList must be []map[string]any")
	require.Len(t, list, 1)

	cat := list[0]
	assert.Equal(t, "🌿", cat["typeEmoji"], "category typeEmoji must be resolved")

	types, ok := cat["types"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, types, 1)
	assert.Equal(t, "🌿", types[0]["emoji"], "type entry emoji must be resolved")
}
