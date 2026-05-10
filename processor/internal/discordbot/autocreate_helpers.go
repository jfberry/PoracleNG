package discordbot

import (
	"strings"
	"unicode"
)

// tokenizeParamString splits a rendered param string into tokens the bot
// parser understands. Segments wrapped in "double quotes" are kept as one
// token (quotes stripped); all other whitespace-separated words are
// individual tokens. This mirrors quoteForCommand's output: the bulk
// runner quotes multi-word fence names so the bot parser doesn't split
// "gent centrum" into two args.
func tokenizeParamString(s string) []string {
	var tokens []string
	inQuote := false
	var cur strings.Builder
	for _, ch := range s {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case !inQuote && unicode.IsSpace(ch):
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// collectingReporter implements reporter by buffering all messages into
// slices. Used by the bulk runner so progress is written to the log (and
// eventually to a summary response) rather than spamming a Discord channel
// for every one of potentially hundreds of fences.
type collectingReporter struct {
	infos  []string
	warns  []string
	errors []string
}

func (r *collectingReporter) Info(msg string)  { r.infos = append(r.infos, msg) }
func (r *collectingReporter) Warn(msg string)  { r.warns = append(r.warns, msg) }
func (r *collectingReporter) Error(msg string) { r.errors = append(r.errors, msg) }

// normalizeDiscordChannelName mirrors Discord's channel-name normalization
// so our snapshot lookups match what Discord actually stores. Discord
// lowercases, replaces whitespace with hyphens, and strips any character
// outside [a-z0-9_-] from text/voice channel names — so a template that
// renders "Canterbury_(Wincheap)" is stored by Discord as
// "canterbury_wincheap", and the cache's pretty name fails to lookup
// without this normalization.
//
// Categories and threads keep their original characters; this helper is
// only used for the text/voice/forum channel-name index in guildSnapshot.
func normalizeDiscordChannelName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	return b.String()
}
