package bot

import "strings"

const (
	PlatformDiscord  = "discord"
	PlatformTelegram = "telegram"
)

// Reply markup is authored in Markdown on both platforms. Discord
// receives the Markdown directly. The Telegram polling bot's send
// path converts to Telegram-flavored HTML at the wire boundary.
//
// Helpers below escape user-derived content so it round-trips through
// either renderer literally — \X for Markdown special chars on the way
// out, then HTML-escaped on the Telegram converter side.

// EscapeForReply escapes user-derived text so any Markdown special
// characters render literally. Use for strings from user input (form
// names, geofence names, usernames, etc.) when interpolating into a
// reply.
//
// Escapes _ * ~ | ` \ — i.e. the Markdown markers callers' content
// must not accidentally trigger.
func (c *CommandContext) EscapeForReply(s string) string {
	return EscapeForPlatform(c.Platform, s)
}

// EscapeForCode escapes only the characters that are special inside a
// backtick code span. Use when interpolating user input into an
// existing backtick-wrapped i18n template. Inside Markdown code spans
// only the closing backtick and the backslash itself need escaping.
func (c *CommandContext) EscapeForCode(s string) string {
	return escapeMarkdownCode(s)
}

// EscapeJoin escapes each item via EscapeForReply and joins with sep.
func (c *CommandContext) EscapeJoin(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = c.EscapeForReply(s)
	}
	return strings.Join(out, sep)
}

// Bold wraps text in Markdown bold markers. Inner text is escaped so
// its own Markdown specials render literally.
func (c *CommandContext) Bold(s string) string {
	if s == "" {
		return ""
	}
	return "**" + escapeMarkdown(s) + "**"
}

// Italic wraps text in Markdown italic markers.
func (c *CommandContext) Italic(s string) string {
	if s == "" {
		return ""
	}
	return "_" + escapeMarkdown(s) + "_"
}

// Code wraps text in inline-code backticks. Inside a code span only
// the closing backtick and the backslash itself need escaping.
func (c *CommandContext) Code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + escapeMarkdownCode(s) + "`"
}

// CodeBlock wraps text in fenced-code backticks with newline padding.
func (c *CommandContext) CodeBlock(s string) string {
	if s == "" {
		return ""
	}
	return "```\n" + escapeMarkdownCode(s) + "\n```"
}

// EscapeForPlatform escapes Markdown special characters. Exposed for
// callers that have a platform string but no CommandContext. The
// platform argument is kept for forward-compatibility but currently
// unused — both supported platforms speak Markdown end-to-end.
func EscapeForPlatform(platform, s string) string {
	_ = platform
	return escapeMarkdown(s)
}

// escapeMarkdown escapes the Markdown formatting characters with a
// leading backslash. The Telegram-side converter and Discord both
// honor \X as a literal-X escape.
func escapeMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '*', '~', '|', '`', '\\', '[', ']':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeMarkdownCode escapes only the characters special inside a
// backtick code span: the backtick itself and the backslash.
func escapeMarkdownCode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '`', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
