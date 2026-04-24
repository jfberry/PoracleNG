package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func newTestArgMatcher() *ArgMatcher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"arg.prefix.d":          "d",
		"arg.prefix.iv":         "iv",
		"arg.prefix.cp":         "cp",
		"arg.prefix.level":      "level",
		"arg.prefix.gen":        "gen",
		"arg.prefix.cap":        "cap",
		"arg.prefix.t":          "t",
		"arg.prefix.miniv":      "miniv",
		"arg.prefix.maxiv":      "maxiv",
		"arg.prefix.maxcp":      "maxcp",
		"arg.prefix.atk":        "atk",
		"arg.prefix.def":        "def",
		"arg.prefix.sta":        "sta",
		"arg.prefix.form":       "form",
		"arg.prefix.template":   "template",
		"arg.prefix.move":       "move",
		"arg.prefix.great":      "great",
		"arg.prefix.greathigh":  "greathigh",
		"arg.prefix.greatcp":    "greatcp",
		"arg.prefix.ultra":      "ultra",
		"arg.prefix.ultrahigh":  "ultrahigh",
		"arg.prefix.ultracp":    "ultracp",
		"arg.prefix.little":     "little",
		"arg.prefix.littlehigh": "littlehigh",
		"arg.prefix.littlecp":   "littlecp",
		"arg.prefix.minspawn":   "minspawn",
		"arg.remove":            "remove",
		"arg.everything":        "everything",
		"arg.clean":             "clean",
		"arg.individually":      "individually",
		"arg.ex":                "ex",
		"arg.shiny":             "shiny",
		"arg.male":              "male",
		"arg.female":            "female",
		"arg.genderless":        "genderless",
		"arg.valor":             "valor",
		"arg.mystic":            "mystic",
		"arg.instinct":          "instinct",
		"arg.harmony":           "harmony",
		"arg.red":               "red",
		"arg.blue":              "blue",
		"arg.yellow":            "yellow",
		"arg.gray":              "gray",
		"arg.normal":            "normal",
		"arg.glacial":           "glacial",
		"arg.mossy":             "mossy",
		"arg.magnetic":          "magnetic",
		"arg.rainy":             "rainy",
		"arg.sparkly":           "sparkly",
		"arg.rsvp":              "rsvp",
		"arg.no_rsvp":           "no rsvp",
		"arg.rsvp_only":         "rsvp only",
		"raid.level.legendary":       "legendary",
		"raid.level.mega":            "mega",
		"raid.level.shadow":          "shadow",
		"raid.level.shadow_legendary": "shadow legendary",
		"raid.level.ultra_beast":      "ultra beast",
		"raid.level.elite":            "elite",
		"raid.level.primal":           "primal",
		"raid.level.mega_legendary":   "mega legendary",
		"poke_type_12": "Grass",
		"poke_type_10": "Fire",
		"poke_type_16": "Dragon",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"arg.prefix.d":        "entfernung",
		"arg.prefix.iv":       "iv",
		"arg.prefix.cp":       "wp",
		"arg.prefix.level":    "level",
		"arg.prefix.template": "vorlage",
		"arg.prefix.form":     "form",
		"arg.prefix.great":    "super",
		"arg.prefix.greatcp":  "superwp",
		"arg.prefix.ultra":    "hyper",
		"arg.prefix.ultracp":  "hyperwp",
		"arg.remove":          "entfernen",
		"arg.everything":      "alle",
		"arg.clean":           "bereinigen",
		"arg.male":            "männlich",
		"arg.female":          "weiblich",
		"arg.genderless":      "geschlechtslos",
		"arg.valor":           "wagemut",
		"arg.mystic":          "weisheit",
		"arg.instinct":        "intuition",
		"arg.harmony":         "unbesetzt",
		"arg.red":             "rot",
		"arg.blue":            "blau",
		"arg.yellow":          "gelb",
		"arg.gray":            "grau",
		"arg.shiny":           "schillernd",
		"raid.level.legendary": "legendär",
		"raid.level.mega":      "mega",
		"raid.level.shadow":    "schatten",
		"poke_type_12":         "Pflanze",
		"poke_type_10":         "Feuer",
		"poke_type_16":         "Drache",
	}))

	// Minimal gamedata with util for raid levels
	gd := &gamedata.GameData{
		Util: &gamedata.UtilData{
			RaidLevels: map[int]struct{}{
				1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {},
				9: {}, 10: {}, 11: {}, 12: {}, 13: {}, 14: {}, 15: {},
			},
		},
		Types: map[int]*gamedata.TypeInfo{
			10: {Emoji: "type-fire", Color: "EE8130"},
			12: {Emoji: "type-grass", Color: "7AC74C"},
			16: {Emoji: "type-dragon", Color: "6F35FC"},
		},
	}

	return NewArgMatcher(bundle, gd, nil, []string{"en", "de"})
}

func TestArgMatchPrefixSingle(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixSingle, Key: "arg.prefix.d"}}
	result := am.Match([]string{"d500"}, params, "en")
	if result.Singles["d"] != 500 {
		t.Errorf("d = %d, want 500", result.Singles["d"])
	}
}

func TestArgMatchPrefixSingleColon(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixSingle, Key: "arg.prefix.d"}}
	result := am.Match([]string{"d:500"}, params, "en")
	if result.Singles["d"] != 500 {
		t.Errorf("d:500 = %d, want 500", result.Singles["d"])
	}
}

func TestArgMatchPrefixSingleGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixSingle, Key: "arg.prefix.d"}}
	result := am.Match([]string{"entfernung500"}, params, "de")
	if result.Singles["d"] != 500 {
		t.Errorf("d = %d, want 500", result.Singles["d"])
	}
}

func TestArgMatchPrefixSingleEnglishFallback(t *testing.T) {
	// German user types English "d500" — should still match via English fallback
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixSingle, Key: "arg.prefix.d"}}
	result := am.Match([]string{"d500"}, params, "de")
	if result.Singles["d"] != 500 {
		t.Errorf("d = %d, want 500 (English fallback)", result.Singles["d"])
	}
}

func TestArgMatchPrefixRange(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixRange, Key: "arg.prefix.iv"}}

	// Single value — only min set, HasMax=false
	result := am.Match([]string{"iv100"}, params, "en")
	if result.Ranges["iv"].Min != 100 || result.Ranges["iv"].HasMax {
		t.Errorf("iv = %+v, want {Min:100, HasMax:false}", result.Ranges["iv"])
	}

	// Colon syntax: iv:55-75
	result = am.Match([]string{"iv:55-75"}, params, "en")
	if result.Ranges["iv"].Min != 55 || result.Ranges["iv"].Max != 75 || !result.Ranges["iv"].HasMax {
		t.Errorf("iv:55-75 = %+v, want {Min:55, Max:75, HasMax:true}", result.Ranges["iv"])
	}

	// Colon syntax single: iv:90
	result = am.Match([]string{"iv:90"}, params, "en")
	if result.Ranges["iv"].Min != 90 || result.Ranges["iv"].HasMax {
		t.Errorf("iv:90 = %+v, want {Min:90, HasMax:false}", result.Ranges["iv"])
	}

	// Range — both min and max set, HasMax=true
	result = am.Match([]string{"iv50-100"}, params, "en")
	if result.Ranges["iv"].Min != 50 || result.Ranges["iv"].Max != 100 || !result.Ranges["iv"].HasMax {
		t.Errorf("iv = %+v, want {Min:50, Max:100, HasMax:true}", result.Ranges["iv"])
	}
}

func TestArgMatchPrefixString(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamPrefixString, Key: "arg.prefix.form"},
		{Type: ParamPrefixString, Key: "arg.prefix.template"},
	}
	result := am.Match([]string{"form:alola", "template:2"}, params, "en")
	if result.Strings["form"] != "alola" {
		t.Errorf("form = %q", result.Strings["form"])
	}
	if result.Strings["template"] != "2" {
		t.Errorf("template = %q", result.Strings["template"])
	}
}

func TestArgMatchPrefixStringGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamPrefixString, Key: "arg.prefix.template"}}
	result := am.Match([]string{"vorlage:2"}, params, "de")
	if result.Strings["template"] != "2" {
		t.Errorf("template = %q, want 2", result.Strings["template"])
	}
}

func TestArgMatchKeyword(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamKeyword, Key: "arg.remove"},
		{Type: ParamKeyword, Key: "arg.clean"},
	}
	result := am.Match([]string{"remove", "clean"}, params, "en")
	if !result.HasKeyword("arg.remove") {
		t.Error("expected arg.remove")
	}
	if !result.HasKeyword("arg.clean") {
		t.Error("expected arg.clean")
	}
}

func TestArgMatchKeywordGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamKeyword, Key: "arg.remove"}}
	result := am.Match([]string{"entfernen"}, params, "de")
	if !result.HasKeyword("arg.remove") {
		t.Error("expected arg.remove for German keyword")
	}
}

func TestArgMatchKeywordEnglishFallbackForGermanUser(t *testing.T) {
	// German user types English "remove" — should match via English fallback
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamKeyword, Key: "arg.remove"}}
	result := am.Match([]string{"remove"}, params, "de")
	if !result.HasKeyword("arg.remove") {
		t.Error("expected arg.remove via English fallback")
	}
}

func TestArgMatchTeam(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamTeam}}

	tests := []struct {
		tok  string
		lang string
		want int
	}{
		{"valor", "en", 2},
		{"red", "en", 2},
		{"mystic", "en", 1},
		{"blue", "en", 1},
		{"instinct", "en", 3},
		{"yellow", "en", 3},
		{"harmony", "en", 0},
		{"gray", "en", 0},
		{"wagemut", "de", 2},    // German valor
		{"rot", "de", 2},        // German red
		{"weisheit", "de", 1},   // German mystic
		{"valor", "de", 2},      // English fallback for German user
	}

	for _, tt := range tests {
		result := am.Match([]string{tt.tok}, params, tt.lang)
		if result.Team != tt.want {
			t.Errorf("team(%q, %s) = %d, want %d", tt.tok, tt.lang, result.Team, tt.want)
		}
	}
}

func TestArgMatchGender(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamGender}}

	result := am.Match([]string{"male"}, params, "en")
	if result.Gender != 1 {
		t.Errorf("gender = %d, want 1", result.Gender)
	}

	result = am.Match([]string{"weiblich"}, params, "de")
	if result.Gender != 2 {
		t.Errorf("gender = %d, want 2 (German female)", result.Gender)
	}
}

func TestArgMatchLureType(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamLureType}}

	result := am.Match([]string{"glacial"}, params, "en")
	if result.LureType != 502 {
		t.Errorf("lure = %d, want 502", result.LureType)
	}

	result = am.Match([]string{"mossy"}, params, "en")
	if result.LureType != 503 {
		t.Errorf("lure = %d, want 503", result.LureType)
	}
}

func TestArgMatchRaidLevelName(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamRaidLevelName}}

	result := am.Match([]string{"legendary"}, params, "en")
	if len(result.RaidLevels) != 1 || result.RaidLevels[0] != 5 {
		t.Errorf("levels = %v, want [5]", result.RaidLevels)
	}

	result = am.Match([]string{"mega"}, params, "en")
	if len(result.RaidLevels) != 1 || result.RaidLevels[0] != 6 {
		t.Errorf("levels = %v, want [6]", result.RaidLevels)
	}
}

func TestArgMatchRaidLevelShadow(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamRaidLevelName}}

	// "shadow" should match all shadow levels (11-15)
	result := am.Match([]string{"shadow"}, params, "en")
	if len(result.RaidLevels) < 2 {
		t.Errorf("shadow levels = %v, want multiple shadow levels", result.RaidLevels)
	}
}

func TestArgMatchRaidLevelGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamRaidLevelName}}

	result := am.Match([]string{"legendär"}, params, "de")
	if len(result.RaidLevels) != 1 || result.RaidLevels[0] != 5 {
		t.Errorf("levels = %v, want [5]", result.RaidLevels)
	}
}

func TestArgMatchTypeName(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamTypeName}}

	result := am.Match([]string{"grass"}, params, "en")
	if len(result.Types) != 1 || result.Types[0] != 12 {
		t.Errorf("types = %v, want [12]", result.Types)
	}
}

func TestArgMatchTypeNameGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamTypeName}}

	result := am.Match([]string{"pflanze"}, params, "de")
	if len(result.Types) != 1 || result.Types[0] != 12 {
		t.Errorf("types = %v, want [12] (Pflanze=Grass)", result.Types)
	}
}

func TestArgMatchPVPGreat(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamPVPLeague, Key: "arg.prefix.great"},
		{Type: ParamPVPLeague, Key: "arg.prefix.greathigh"},
		{Type: ParamPVPLeague, Key: "arg.prefix.greatcp"},
	}

	// great5 → worst=5
	result := am.Match([]string{"great5"}, params, "en")
	if f, ok := result.PVP["great"]; !ok || f.Worst != 5 {
		t.Errorf("great = %+v, want worst=5", result.PVP["great"])
	}

	// great1-10 → best=1, worst=10
	result = am.Match([]string{"great1-10"}, params, "en")
	if f := result.PVP["great"]; f.Best != 1 || f.Worst != 10 {
		t.Errorf("great = %+v, want best=1 worst=10", f)
	}

	// greathigh3 → best=3
	result = am.Match([]string{"greathigh3"}, params, "en")
	if f := result.PVP["great"]; f.Best != 3 {
		t.Errorf("great = %+v, want best=3", f)
	}

	// greatcp1400 → minCP=1400
	result = am.Match([]string{"greatcp1400"}, params, "en")
	if f := result.PVP["great"]; f.MinCP != 1400 {
		t.Errorf("great = %+v, want minCP=1400", f)
	}
}

func TestArgMatchPVPGerman(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamPVPLeague, Key: "arg.prefix.great"},
	}

	// German "super5" for great league
	result := am.Match([]string{"super5"}, params, "de")
	if f, ok := result.PVP["great"]; !ok || f.Worst != 5 {
		t.Errorf("great = %+v, want worst=5 (German super)", result.PVP["great"])
	}
}

func TestArgMatchLatLon(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamLatLon}}

	result := am.Match([]string{"51.28,1.08"}, params, "en")
	if result.Coords == nil {
		t.Fatal("coords nil")
	}
	if result.Coords.Lat != 51.28 || result.Coords.Lon != 1.08 {
		t.Errorf("coords = %+v", result.Coords)
	}
}

func TestArgMatchUnrecognized(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamPrefixSingle, Key: "arg.prefix.d"},
		{Type: ParamKeyword, Key: "arg.clean"},
	}
	result := am.Match([]string{"d500", "clean", "gobbledygook"}, params, "en")
	if len(result.Unrecognized) != 1 || result.Unrecognized[0] != "gobbledygook" {
		t.Errorf("unrecognized = %v", result.Unrecognized)
	}
}

func TestArgMatchCombined(t *testing.T) {
	// Simulate: !track pikachu iv90-100 d500 clean male
	am := newTestArgMatcher()
	params := []ParamDef{
		{Type: ParamPrefixRange, Key: "arg.prefix.iv"},
		{Type: ParamPrefixSingle, Key: "arg.prefix.d"},
		{Type: ParamKeyword, Key: "arg.clean"},
		{Type: ParamGender},
		// No PokemonName param — "pikachu" will be unrecognized (resolver is nil in this test)
	}
	result := am.Match([]string{"pikachu", "iv90-100", "d500", "clean", "male"}, params, "en")
	if result.Ranges["iv"].Min != 90 || result.Ranges["iv"].Max != 100 {
		t.Errorf("iv = %+v", result.Ranges["iv"])
	}
	if result.Singles["d"] != 500 {
		t.Errorf("d = %d", result.Singles["d"])
	}
	if !result.HasKeyword("arg.clean") {
		t.Error("missing clean")
	}
	if result.Gender != 1 {
		t.Errorf("gender = %d", result.Gender)
	}
	// pikachu should be unrecognized (no resolver)
	if len(result.Unrecognized) != 1 || result.Unrecognized[0] != "pikachu" {
		t.Errorf("unrecognized = %v", result.Unrecognized)
	}
}

func TestArgMatchTeamDefaultUnset(t *testing.T) {
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamTeam}}
	result := am.Match([]string{}, params, "en")
	if result.Team != 4 {
		t.Errorf("default team = %d, want 4", result.Team)
	}
}

func TestArgMatchMultiWordKeyword(t *testing.T) {
	// "no rsvp" is a multi-word keyword (from underscore→space conversion)
	am := newTestArgMatcher()
	params := []ParamDef{{Type: ParamKeyword, Key: "arg.no_rsvp"}}
	result := am.Match([]string{"no rsvp"}, params, "en")
	if !result.HasKeyword("arg.no_rsvp") {
		t.Error("expected arg.no_rsvp for multi-word keyword")
	}
}

// newMultiWordTestMatcher builds an ArgMatcher with a minimal set of
// multi-word items / moves / forms / pokemon names, exercising the
// collapseMultiWord pre-pass without dragging in the full game data
// or pokemon resolver setup.
func newMultiWordTestMatcher() *ArgMatcher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"arg.prefix.move":     "move",
		"arg.prefix.form":     "form",
		"arg.prefix.d":        "d",
		"arg.prefix.iv":       "iv",
		"arg.prefix.template": "template",
		"arg.remove":          "remove",
		"arg.everything":      "everything",
		"arg.clean":           "clean",
		// Items — two multi-word entries, one single-word distractor.
		"item_1":   "Pokeball",
		"item_701": "Razz Berry",
		"item_702": "Golden Razz Berry",
		"item_708": "Silver Pinap Berry",
		"item_1201": "Rare Candy",
		// Moves — multi-word.
		"move_14": "Hyper Beam",
		"move_58": "Ice Beam",
		"move_94": "Rock Slide",
		"move_101": "Shadow Ball",
		// Forms — multi-word.
		"form_2463": "Low Key Form",
		"form_2464": "Amped Form",
		"form_147":  "Black Kyurem",
		"form_140":  "Incarnate Forme",
	}))
	// buildMultiWordVocabularies iterates gd.Items / gd.Moves / the form
	// IDs in gd.Monsters, so the test GameData has to enumerate which
	// IDs exist even though the translator holds the names.
	gd := &gamedata.GameData{
		Util:  &gamedata.UtilData{},
		Items: map[int]*gamedata.Item{1: {}, 701: {}, 702: {}, 708: {}, 1201: {}},
		Moves: map[int]*gamedata.Move{14: {}, 58: {}, 94: {}, 101: {}},
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 140}: {}, {ID: 1, Form: 147}: {},
			{ID: 1, Form: 2463}: {}, {ID: 1, Form: 2464}: {},
		},
	}
	am := NewArgMatcher(bundle, gd, nil, []string{"en"})

	// The real setup pulls multi-word pokemon names out of the
	// PokemonResolver. Seed the parser's bare vocabulary directly for
	// tests; mirrors what the resolver registers ("mr. mime" and the
	// de-punctuated "mr mime").
	am.bareMultiWord["mr. mime"] = true
	am.bareMultiWord["mr mime"] = true
	am.bareMultiWord["tapu koko"] = true
	return am
}

// TestCollapseMultiWordItems — bare multi-word item names collapse into
// single tokens so "razz berry" reaches the item matcher as one arg.
func TestCollapseMultiWordItems(t *testing.T) {
	am := newMultiWordTestMatcher()
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"razz", "berry"}, []string{"razz berry"}},
		{[]string{"golden", "razz", "berry"}, []string{"golden razz berry"}},
		{[]string{"silver", "pinap", "berry"}, []string{"silver pinap berry"}},
		{[]string{"pikachu", "rare", "candy", "d:500"}, []string{"pikachu", "rare candy", "d:500"}},
		// Greedy-longest: prefer "golden razz berry" over "razz berry".
		{[]string{"golden", "razz", "berry", "pikachu"}, []string{"golden razz berry", "pikachu"}},
		// Single-word items stay untouched.
		{[]string{"pokeball", "razz", "berry"}, []string{"pokeball", "razz berry"}},
		// No match → tokens unchanged.
		{[]string{"pikachu", "charizard"}, []string{"pikachu", "charizard"}},
	}
	for _, c := range cases {
		got := am.collapseMultiWord(c.in)
		if !equalStringSlices(got, c.want) {
			t.Errorf("collapseMultiWord(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCollapseMultiWordPokemonNames — multi-word pokemon names collapse
// (including the de-punctuated variants the resolver registers).
func TestCollapseMultiWordPokemonNames(t *testing.T) {
	am := newMultiWordTestMatcher()
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"mr", "mime"}, []string{"mr mime"}},
		{[]string{"mr.", "mime"}, []string{"mr. mime"}},
		{[]string{"tapu", "koko"}, []string{"tapu koko"}},
		{[]string{"pikachu", "mr", "mime"}, []string{"pikachu", "mr mime"}},
	}
	for _, c := range cases {
		got := am.collapseMultiWord(c.in)
		if !equalStringSlices(got, c.want) {
			t.Errorf("collapseMultiWord(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCollapseMultiWordPrefixed — move:X and form:X consume following
// tokens when they complete a known multi-word entry.
func TestCollapseMultiWordPrefixed(t *testing.T) {
	am := newMultiWordTestMatcher()
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"move:hyper", "beam"}, []string{"move:hyper beam"}},
		{[]string{"move:ice", "beam"}, []string{"move:ice beam"}},
		{[]string{"move:rock", "slide"}, []string{"move:rock slide"}},
		{[]string{"form:low", "key", "form"}, []string{"form:low key form"}},
		{[]string{"form:black", "kyurem"}, []string{"form:black kyurem"}},
		// Mixed with other args.
		{[]string{"tyranitar", "move:hyper", "beam", "d:1000"}, []string{"tyranitar", "move:hyper beam", "d:1000"}},
		// Already-complete single-word move: stays as-is.
		{[]string{"move:earthquake"}, []string{"move:earthquake"}},
		// Unknown prefix — doesn't trigger prefix-scoped collapse.
		{[]string{"iv:100", "razz", "berry"}, []string{"iv:100", "razz berry"}},
	}
	for _, c := range cases {
		got := am.collapseMultiWord(c.in)
		if !equalStringSlices(got, c.want) {
			t.Errorf("collapseMultiWord(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCollapseMultiWordMultiLanguage — the vocabulary is built from
// every configured language's translations, so a German user can type
// "süßer apfel" just like an English user types "razz berry".
func TestCollapseMultiWordMultiLanguage(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"arg.prefix.move": "move",
		"arg.prefix.form": "form",
		"arg.prefix.iv":   "iv",
		"item_701":        "Razz Berry",
		"move_14":         "Hyper Beam",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"arg.prefix.move": "move",
		"arg.prefix.form": "form",
		"arg.prefix.iv":   "iv",
		"item_701":        "Himmihbeere", // single-word in German
		"item_702":        "Süßer Apfel", // multi-word in German
		"move_14":         "Hyperstrahl", // single-word in German
		"move_25":         "Draco Meteor", // happens to stay English in German
	}))
	gd := &gamedata.GameData{
		Util:  &gamedata.UtilData{},
		Items: map[int]*gamedata.Item{701: {}, 702: {}},
		Moves: map[int]*gamedata.Move{14: {}, 25: {}},
	}
	am := NewArgMatcher(bundle, gd, nil, []string{"en", "de"})

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"english bare item", []string{"razz", "berry"}, []string{"razz berry"}},
		{"german bare item", []string{"süßer", "apfel"}, []string{"süßer apfel"}},
		{"english prefixed move", []string{"move:hyper", "beam"}, []string{"move:hyper beam"}},
		{"german prefixed move (kept english in-game)", []string{"move:draco", "meteor"}, []string{"move:draco meteor"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := am.collapseMultiWord(c.in)
			if !equalStringSlices(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestCollapseMultiWordPassthrough — tokens that aren't multi-word
// prefixes stay exactly as they were.
func TestCollapseMultiWordPassthrough(t *testing.T) {
	am := newMultiWordTestMatcher()
	in := []string{"pikachu", "d:500", "iv:100", "clean"}
	got := am.collapseMultiWord(in)
	if !equalStringSlices(got, in) {
		t.Errorf("tokens unchanged expected, got %v", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
