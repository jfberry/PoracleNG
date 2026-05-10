package telegrambot

import "strings"

// markdownToHTML converts a Markdown subset to Telegram-flavored HTML
// for sending with parse_mode=HTML.
//
// Supported constructs:
//   **bold**           → <b>bold</b>
//   _italic_           → <i>italic</i>
//   `code`             → <code>code</code>      (literal contents, only \ and ` are special)
//   ```pre```          → <pre>pre</pre>         (same)
//   [text](url)        → <a href="url">text</a>
//   \X                 → literal X (Markdown backslash escape)
//
// Literal <, >, and & are HTML-escaped to &lt; &gt; &amp; everywhere
// they appear as text — including inside <code>/<pre> spans and inside
// link href attributes.
//
// Markers that don't pair (e.g. a stray *) render as the literal
// character. The converter never errors; pathological input degrades
// to readable plain text.
func markdownToHTML(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	convertRange(&b, []rune(s))
	return b.String()
}

func convertRange(b *strings.Builder, runes []rune) {
	for i := 0; i < len(runes); {
		r := runes[i]

		// Backslash escape — emit next rune literally (HTML-escaped).
		if r == '\\' && i+1 < len(runes) {
			writeHTMLEscaped(b, runes[i+1])
			i += 2
			continue
		}

		// Triple-backtick code block.
		if r == '`' && i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`' {
			end := indexSeq(runes, i+3, []rune{'`', '`', '`'})
			if end < 0 {
				b.WriteString("```")
				i += 3
				continue
			}
			b.WriteString("<pre>")
			writeCodeContents(b, runes[i+3:end])
			b.WriteString("</pre>")
			i = end + 3
			continue
		}

		// Inline code.
		if r == '`' {
			end := findClose(runes, i+1, '`')
			if end < 0 {
				b.WriteRune('`')
				i++
				continue
			}
			b.WriteString("<code>")
			writeCodeContents(b, runes[i+1:end])
			b.WriteString("</code>")
			i = end + 1
			continue
		}

		// **bold**
		if r == '*' && i+1 < len(runes) && runes[i+1] == '*' {
			end := indexSeq(runes, i+2, []rune{'*', '*'})
			if end < 0 {
				b.WriteString("**")
				i += 2
				continue
			}
			b.WriteString("<b>")
			convertRange(b, runes[i+2:end])
			b.WriteString("</b>")
			i = end + 2
			continue
		}

		// _italic_
		if r == '_' {
			end := findClose(runes, i+1, '_')
			if end < 0 {
				b.WriteRune('_')
				i++
				continue
			}
			b.WriteString("<i>")
			convertRange(b, runes[i+1:end])
			b.WriteString("</i>")
			i = end + 1
			continue
		}

		// [text](url)
		if r == '[' {
			closeText := findClose(runes, i+1, ']')
			if closeText > 0 && closeText+1 < len(runes) && runes[closeText+1] == '(' {
				closeURL := findClose(runes, closeText+2, ')')
				if closeURL > 0 {
					b.WriteString(`<a href="`)
					writeAttrEscaped(b, runes[closeText+2:closeURL])
					b.WriteString(`">`)
					convertRange(b, runes[i+1:closeText])
					b.WriteString("</a>")
					i = closeURL + 1
					continue
				}
			}
			// Not a link — emit literal '['.
			b.WriteRune('[')
			i++
			continue
		}

		writeHTMLEscaped(b, r)
		i++
	}
}

// findClose returns the index of the first unescaped occurrence of c in
// runes[start:], or -1 if not found. Backslash escapes are skipped.
func findClose(runes []rune, start int, c rune) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if runes[i] == c {
			return i
		}
	}
	return -1
}

// indexSeq returns the index of the first occurrence of seq in
// runes[start:], or -1. Backslash escapes are honored at the start
// position only (a leading \ skips one rune).
func indexSeq(runes []rune, start int, seq []rune) int {
	for i := start; i+len(seq) <= len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		match := true
		for j, sr := range seq {
			if runes[i+j] != sr {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// writeHTMLEscaped writes a single rune, replacing < > & with HTML
// entities. Other runes are written verbatim.
func writeHTMLEscaped(b *strings.Builder, r rune) {
	switch r {
	case '<':
		b.WriteString("&lt;")
	case '>':
		b.WriteString("&gt;")
	case '&':
		b.WriteString("&amp;")
	default:
		b.WriteRune(r)
	}
}

// writeCodeContents writes the contents of a <code> or <pre> span:
// no Markdown is parsed, but \X escapes still produce literal X, and
// HTML <>& are escaped.
func writeCodeContents(b *strings.Builder, runes []rune) {
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			writeHTMLEscaped(b, runes[i+1])
			i++
			continue
		}
		writeHTMLEscaped(b, r)
	}
}

// writeAttrEscaped writes a URL attribute value, escaping HTML specials
// plus the double-quote since the attribute is quoted.
func writeAttrEscaped(b *strings.Builder, runes []rune) {
	for _, r := range runes {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
}
