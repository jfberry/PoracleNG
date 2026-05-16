package slash

import (
	"regexp"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// poracleToDiscord maps Poracle locale codes (as used in the i18n bundle and
// the humans.language column) to the discordgo.Locale constants Discord
// expects in name_localizations / description_localizations.
//
// Not every Poracle locale has a corresponding Discord locale (Discord does
// not ship nb-NO, for instance). Unmapped locales are skipped silently —
// localizationsForKey simply won't emit an entry for them.
var poracleToDiscord = map[string]discordgo.Locale{
	"de":    discordgo.German,
	"fr":    discordgo.French,
	"es":    discordgo.SpanishES,
	"it":    discordgo.Italian,
	"nl":    discordgo.Dutch,
	"pl":    discordgo.Polish,
	"ru":    discordgo.Russian,
	"ja":    discordgo.Japanese,
	"zh-cn": discordgo.ChineseCN,
	"zh-tw": discordgo.ChineseTW,
	"ko":    discordgo.Korean,
	"sv":    discordgo.Swedish,
	"pt-br": discordgo.PortugueseBR,
}

// slashNameRe matches Discord's slash command name regex: 1..32 chars of
// letters (any script), digits, underscore, or hyphen. Discord also requires
// names to be lowercase, but our i18n key values are author-supplied — we
// only enforce the character-set/length constraint here so a stray "Verfolge"
// in a translator's bundle doesn't reject the whole sync.
var slashNameRe = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)

// validSlashName reports whether s is a syntactically valid Discord slash
// command name. Used by both the primary name resolver (definitions.go) and
// the localization map builder (localizationsForKey) to drop invalid entries
// rather than poison the entire sync.
func validSlashName(s string) bool {
	return slashNameRe.MatchString(s)
}

// localizationsForKey walks every loaded language in the bundle, looks up
// the given i18n key in each, and returns a Discord-locale-keyed map of the
// non-English values that have a real entry in that language.
//
// Behaviour:
//   - English is always skipped: its value is the primary Name/Description,
//     not a localization.
//   - Languages with no Discord-locale mapping are skipped silently.
//   - The lookup deliberately bypasses the English-fallback chain: we read
//     the translator's own message map directly so a German translator
//     missing a slash.cmd.* key does NOT emit "track" via the fallback.
//     Discord ignores localizations that match the primary anyway, but the
//     extra weight just bloats the payload (and the fingerprint) for no
//     user-visible gain.
//   - Empty values are skipped.
//   - When validateName=true, values that don't match Discord's slash-name
//     regex are dropped. Used for name_localizations only; descriptions can
//     contain spaces/punctuation so we skip the check there.
//
// Returns nil when no localization survives the filters. Discordgo emits
// name_localizations / description_localizations only when the pointer is
// non-nil, so nil here means "don't ship this field at all".
func localizationsForKey(bundle *i18n.Bundle, key string, validateName bool) *map[discordgo.Locale]string {
	if bundle == nil {
		return nil
	}
	out := map[discordgo.Locale]string{}
	for _, lang := range bundle.LoadedLanguages() {
		if lang == "en" {
			continue
		}
		discordCode, ok := poracleToDiscord[lang]
		if !ok {
			continue
		}
		tr := bundle.For(lang)
		if tr == nil {
			continue
		}
		// Read the translator's own messages directly to avoid the
		// English fallback path Translator.T uses for missing keys.
		val, ok := tr.Messages()[key]
		if !ok || val == "" {
			continue
		}
		if validateName && !validSlashName(val) {
			continue
		}
		out[discordCode] = val
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}
