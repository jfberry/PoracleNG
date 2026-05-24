package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func TestPoracleToDiscord(t *testing.T) {
	cases := map[string]discordgo.Locale{
		"de":    discordgo.German,
		"fr":    discordgo.French,
		"es":    discordgo.SpanishES,
		"it":    discordgo.Italian,
		"ja":    discordgo.Japanese,
		"zh-cn": discordgo.ChineseCN,
	}
	for poracle, want := range cases {
		got, ok := poracleToDiscord[poracle]
		if !ok {
			t.Errorf("missing mapping for poracle locale %q", poracle)
			continue
		}
		if got != want {
			t.Errorf("poracleToDiscord[%q]=%v, want %v", poracle, got, want)
		}
	}
}

// TestLocalizationsForKeyOnlyLoadedLanguages verifies that the helper emits
// entries only for languages actually present in the bundle and for which we
// have a Discord-locale mapping. Crucially, languages with no Discord
// mapping (nb-no) and languages with empty/missing translations must not
// contaminate the output.
func TestLocalizationsForKeyOnlyLoadedLanguages(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"slash.cmd.track": "track"}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"slash.cmd.track": "verfolge"}))
	bundle.LinkFallbacks()

	loc := localizationsForKey(bundle, "slash.cmd.track", true)
	if loc == nil {
		t.Fatal("expected non-nil localizations when at least one non-English value present")
	}
	if (*loc)[discordgo.German] != "verfolge" {
		t.Errorf("German entry = %q, want \"verfolge\"", (*loc)[discordgo.German])
	}
	if _, ok := (*loc)[discordgo.French]; ok {
		t.Error("French should be absent when fr is not in the bundle")
	}
	if _, ok := (*loc)["en-US"]; ok {
		t.Error("English should never appear in localizations map")
	}
}

// TestLocalizationsForKeyReturnsNilWhenOnlyEnglish covers the common operator
// setup where en.json is the only loaded language: we must return nil so
// discordgo omits name_localizations from the wire payload entirely.
func TestLocalizationsForKeyReturnsNilWhenOnlyEnglish(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"slash.cmd.track": "track"}))

	if loc := localizationsForKey(bundle, "slash.cmd.track", true); loc != nil {
		t.Errorf("expected nil for English-only bundle, got %v", *loc)
	}
}

// TestLocalizationsForKeySkipsMissingKey: a translator that doesn't have the
// key (Translator.T returns the key itself) must be skipped, not echo the
// raw key into Discord's UI.
func TestLocalizationsForKeySkipsMissingKey(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"slash.cmd.track": "track"}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{})) // no entry for slash.cmd.track
	bundle.LinkFallbacks()

	if loc := localizationsForKey(bundle, "slash.cmd.track", true); loc != nil {
		// With LinkFallbacks the German translator falls back to English, but
		// we explicitly skip the fallback path: a localization that's
		// identical to the English primary adds no value.
		t.Errorf("expected nil when only translator falls back to English, got %v", *loc)
	}
}

// TestLocalizationsForKeyValidateNameDropsInvalid verifies the validate-name
// branch drops entries that don't match Discord's slash-name regex (spaces,
// punctuation), but accepts other-script identifiers (unicode letters).
func TestLocalizationsForKeyValidateNameDropsInvalid(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"slash.cmd.track": "track"}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"slash.cmd.track": "name with spaces"}))
	bundle.AddTranslator(i18n.NewTranslator("ja", map[string]string{"slash.cmd.track": "追跡"}))
	bundle.LinkFallbacks()

	loc := localizationsForKey(bundle, "slash.cmd.track", true)
	if loc == nil {
		t.Fatal("expected non-nil localizations when valid Japanese entry present")
	}
	if _, ok := (*loc)[discordgo.German]; ok {
		t.Error("German with spaces should be filtered out under validateName=true")
	}
	if (*loc)[discordgo.Japanese] != "追跡" {
		t.Errorf("Japanese entry = %q, want \"追跡\"", (*loc)[discordgo.Japanese])
	}
}

// TestLocalizationsForKeyAllowsLongDescriptions verifies validateName=false
// keeps strings with spaces/punctuation — only the slash-name flow should
// invoke the regex check.
func TestLocalizationsForKeyAllowsLongDescriptions(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"slash.desc.track": "Track a Pokemon"}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"slash.desc.track": "Verfolge ein Pokemon"}))
	bundle.LinkFallbacks()

	loc := localizationsForKey(bundle, "slash.desc.track", false)
	if loc == nil {
		t.Fatal("expected non-nil localizations for description with spaces")
	}
	if (*loc)[discordgo.German] != "Verfolge ein Pokemon" {
		t.Errorf("German description = %q", (*loc)[discordgo.German])
	}
}

func TestValidSlashName(t *testing.T) {
	cases := map[string]bool{
		"track":                                  true,
		"verfolge":                               true,
		"追跡":                                     true,
		"poracle-version":                        true,
		"poracle_version":                        true,
		"":                                       false,
		"with spaces":                            false,
		"too.long":                               false, // dot not allowed
		"very_long_name_with_more_than_32_chars": false,
	}
	for input, want := range cases {
		got := validSlashName(input)
		if got != want {
			t.Errorf("validSlashName(%q)=%v, want %v", input, got, want)
		}
	}
}

func TestValidSlashNameLengthBoundary(t *testing.T) {
	// 32-char name should pass; 33-char should fail.
	just32 := "abcdefghijklmnopqrstuvwxyz123456" // 32
	if !validSlashName(just32) {
		t.Errorf("32-char name rejected: %q", just32)
	}
	tooLong := just32 + "x"
	if validSlashName(tooLong) {
		t.Errorf("33-char name accepted: %q", tooLong)
	}
}

// TestLookupEnglishOrFallback covers the canonical-English source for the
// option/choice helpers: present → returned, absent → fallback, raw-key
// (Translator.T return-the-key behaviour) → fallback.
func TestLookupEnglishOrFallback(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"slash.opt.track.pokemon":      "pokemon",
		"slash.opt.track.pokemon.desc": "Pokemon to track",
	}))
	if got := lookupEnglishOrFallback(bundle, "slash.opt.track.pokemon", "fallback"); got != "pokemon" {
		t.Errorf("present key returned %q, want \"pokemon\"", got)
	}
	if got := lookupEnglishOrFallback(bundle, "slash.opt.track.absent", "fallback"); got != "fallback" {
		t.Errorf("absent key returned %q, want \"fallback\"", got)
	}
	if got := lookupEnglishOrFallback(nil, "any.key", "fallback"); got != "fallback" {
		t.Errorf("nil bundle returned %q, want \"fallback\"", got)
	}
}

// TestOptNameFallsBackOnInvalid covers the validateName branch: when the
// English translation contains a space (or other regex-rejected character),
// optName falls back to the canonical English and returns nil localizations.
// This prevents an admin typo (e.g. "track pokemon" with a space) from
// poisoning every translator's view of the option.
func TestOptNameFallsBackOnInvalid(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"slash.opt.track.pokemon": "with spaces",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"slash.opt.track.pokemon": "pokemon", // valid, but English failed first
	}))
	bundle.LinkFallbacks()
	name, loc := optName(bundle, "track.pokemon", "pokemon")
	if name != "pokemon" {
		t.Errorf("got name %q, want canonical fallback \"pokemon\"", name)
	}
	if loc != nil {
		t.Errorf("expected nil localizations when English fell back, got %v", loc)
	}
}

// TestOptNameUsesEnglishWhenValid: the English bundle value is honoured when
// it's a valid slash name and the localization map for other languages flows
// through.
func TestOptNameUsesEnglishWhenValid(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"slash.opt.track.pokemon": "pokemon",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"slash.opt.track.pokemon": "pokémon",
	}))
	bundle.LinkFallbacks()
	name, loc := optName(bundle, "track.pokemon", "fallback")
	if name != "pokemon" {
		t.Errorf("got name %q, want \"pokemon\"", name)
	}
	if loc == nil {
		t.Fatal("expected non-nil localizations when German entry valid")
	}
	if loc[discordgo.German] != "pokémon" {
		t.Errorf("German localization = %q", loc[discordgo.German])
	}
}

// TestOptDescGermanLocalization verifies descriptions flow through with no
// regex validation — spaces and punctuation are preserved.
func TestOptDescGermanLocalization(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"slash.opt.track.pokemon.desc": "Pokemon to track",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"slash.opt.track.pokemon.desc": "Pokémon zum Verfolgen",
	}))
	bundle.LinkFallbacks()
	desc, loc := optDesc(bundle, "track.pokemon", "fallback")
	if desc != "Pokemon to track" {
		t.Errorf("desc = %q", desc)
	}
	if loc == nil || loc[discordgo.German] != "Pokémon zum Verfolgen" {
		t.Errorf("German desc localization missing: %v", loc)
	}
}

// TestChoiceNameGermanLocalization verifies choice labels (which permit
// spaces and capitals — they aren't validated by the slash-name regex)
// translate cleanly.
func TestChoiceNameGermanLocalization(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"slash.choice.raid.level.5": "Tier 5",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"slash.choice.raid.level.5": "Stufe 5",
	}))
	bundle.LinkFallbacks()
	name, loc := choiceName(bundle, "raid.level.5", "fallback")
	if name != "Tier 5" {
		t.Errorf("English name = %q", name)
	}
	if loc == nil || loc[discordgo.German] != "Stufe 5" {
		t.Errorf("German choice localization missing: %v", loc)
	}
}

// TestChoiceNameFallback: missing English key → canonical English fallback
// and no localizations (a translator that filled in only their own
// language without the English seed is still ignored so the message
// doesn't drift away from the operator's seeded set).
func TestChoiceNameFallback(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"slash.choice.raid.level.5": "Stufe 5",
	}))
	bundle.LinkFallbacks()
	name, loc := choiceName(bundle, "raid.level.5", "Tier 5")
	if name != "Tier 5" {
		t.Errorf("expected fallback, got %q", name)
	}
	// Localization map is still populated — translators may legitimately ship
	// before the English seed catches up, and Discord ignores duplicates.
	if loc == nil || loc[discordgo.German] != "Stufe 5" {
		t.Errorf("German choice localization missing: %v", loc)
	}
}
