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

// lowerASCII returns s lowercased. A thin wrapper around strings.ToLower
// kept as a named function so call-sites in the sync runner are
// self-documenting about intent (match the bot parser's lower-case pass).
func lowerASCII(s string) string {
	return strings.ToLower(s)
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
