package nlp

import (
	"strings"
)

// fillerPrefixes are stripped from the beginning of input (longest first).
var fillerPrefixes = []string{
	"notify me about",
	"let me know about",
	"alert me when",
	"alert me about",
	"tell me about",
	"can you please",
	"looking for",
	"can you",
	"find me",
	"get me",
	"show me",
	"i'd like",
	"i want",
	"please",
	"track",
}

// noiseWords are removed from anywhere in the token list.
var noiseWords = map[string]bool{
	"and": true, "or": true, "&": true, "for": true, "with": true,
	"the": true, "a": true, "an": true, "some": true, "any": true,
	"of": true, "about": true, "when": true, "that": true, "are": true,
	"is": true, "there": true, "to": true, "me": true, "my": true,
	"on": true, "in": true, "it": true, "its": true, "by": true,
}

// Normalize lowercases the input, strips filler prefixes and noise words,
// replaces commas with spaces, and collapses whitespace.
func Normalize(input string) string {
	s := strings.ToLower(input)
	s = strings.ReplaceAll(s, ",", " ")
	s = collapseSpaces(s)

	// Strip filler prefixes (longest first).
	for _, prefix := range fillerPrefixes {
		if after, ok := strings.CutPrefix(s, prefix); ok {
			s = after
			s = strings.TrimSpace(s)
			break
		}
	}

	// Remove noise words.
	tokens := strings.Fields(s)
	filtered := tokens[:0]
	for _, t := range tokens {
		if !noiseWords[t] {
			filtered = append(filtered, t)
		}
	}

	return strings.Join(filtered, " ")
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prev {
				b.WriteByte(' ')
			}
			prev = true
		} else {
			b.WriteRune(r)
			prev = false
		}
	}
	return strings.TrimSpace(b.String())
}
