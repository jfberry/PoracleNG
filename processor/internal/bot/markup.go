package bot

import "strings"

const (
	PlatformDiscord  = "discord"
	PlatformTelegram = "telegram"
)

// EscapeForReply escapes user-derived text so any Markdown special
// characters render literally on the active platform. Use for strings
// from user input (form names, geofence names, usernames, etc.) when
// interpolating into a reply.
//
// Discord:           escapes _ * ~ | ` \
// Telegram MarkdownV2: escapes _ * [ ] ( ) ~ ` > # + - = | { } . ! \
// Other platforms:   passes through unchanged.
func (c *CommandContext) EscapeForReply(s string) string {
	return EscapeForPlatform(c.Platform, s)
}

// EscapeForCode escapes only the characters special inside a backtick
// code span. Use when interpolating user input into an existing
// backtick-wrapped i18n template — escaping the full Markdown set would
// inject literal backslashes into the rendered code span.
//
// Telegram MarkdownV2: escapes ` and \
// Discord:             pass-through (Discord renders code spans literally)
func (c *CommandContext) EscapeForCode(s string) string {
	if c.Platform == PlatformTelegram {
		return escapeTelegramV2Code(s)
	}
	return s
}

// EscapeJoin escapes each item in items via EscapeForReply and joins
// them with sep. Convenience for the common "Foo: a, b, c" pattern
// where a/b/c are user-derived names.
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

// Bold wraps text in platform-correct bold syntax. Inner text is
// escaped so its own Markdown special characters render literally.
func (c *CommandContext) Bold(s string) string {
	if s == "" {
		return ""
	}
	switch c.Platform {
	case PlatformDiscord:
		return "**" + escapeDiscord(s) + "**"
	case PlatformTelegram:
		return "*" + escapeTelegramV2(s) + "*"
	}
	return s
}

// Italic wraps text in platform-correct italic syntax.
func (c *CommandContext) Italic(s string) string {
	if s == "" {
		return ""
	}
	switch c.Platform {
	case PlatformDiscord:
		return "_" + escapeDiscord(s) + "_"
	case PlatformTelegram:
		return "_" + escapeTelegramV2(s) + "_"
	}
	return s
}

// Code wraps text in inline-code backticks. Inside a code span only the
// backtick and backslash need escaping on Telegram MarkdownV2; Discord
// treats everything inside backticks as literal.
func (c *CommandContext) Code(s string) string {
	if s == "" {
		return ""
	}
	switch c.Platform {
	case PlatformDiscord:
		return "`" + s + "`"
	case PlatformTelegram:
		return "`" + escapeTelegramV2Code(s) + "`"
	}
	return s
}

// CodeBlock wraps text in fenced-code backticks with newline padding.
func (c *CommandContext) CodeBlock(s string) string {
	if s == "" {
		return ""
	}
	switch c.Platform {
	case PlatformDiscord:
		return "```\n" + s + "\n```"
	case PlatformTelegram:
		return "```\n" + escapeTelegramV2Code(s) + "\n```"
	}
	return s
}

// EscapeForPlatform escapes Markdown special characters for the given
// platform string. Exposed for callers that have a platform string but
// no CommandContext.
func EscapeForPlatform(platform, s string) string {
	switch platform {
	case PlatformDiscord:
		return escapeDiscord(s)
	case PlatformTelegram:
		return escapeTelegramV2(s)
	}
	return s
}

func escapeDiscord(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '*', '~', '|', '`', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeTelegramV2 escapes the Telegram Bot API MarkdownV2 reserved
// characters: _ * [ ] ( ) ~ ` > # + - = | { } . ! \
func escapeTelegramV2(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '*', '[', ']', '(', ')', '~', '`',
			'>', '#', '+', '-', '=', '|',
			'{', '}', '.', '!', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeTelegramV2Code escapes the only characters special inside a
// MarkdownV2 code or pre-code span: backtick and backslash.
func escapeTelegramV2Code(s string) string {
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
