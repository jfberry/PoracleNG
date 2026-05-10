package bot

import "testing"

func TestEscapeDiscord(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"antique_form", `antique\_form`},
		{"a*b", `a\*b`},
		{"path\\file", `path\\file`},
		{"all _*~|` and \\", "all \\_\\*\\~\\|\\` and \\\\"},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeDiscord(c.in); got != c.want {
			t.Errorf("escapeDiscord(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramV2(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"antique_form", `antique\_form`},
		{"hi.", `hi\.`},
		{"why!", `why\!`},
		{"3 - 1 = 2", `3 \- 1 \= 2`},
		{"(parens)", `\(parens\)`},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeTelegramV2(c.in); got != c.want {
			t.Errorf("escapeTelegramV2(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramV2Code(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"antique_form", "antique_form"},
		{"some `code`", "some \\`code\\`"},
		{"path\\", "path\\\\"},
		{"plain text", "plain text"},
		{"3 - 1 = 2", "3 - 1 = 2"},
	}
	for _, c := range cases {
		if got := escapeTelegramV2Code(c.in); got != c.want {
			t.Errorf("escapeTelegramV2Code(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCommandContextStyleHelpers(t *testing.T) {
	discord := &CommandContext{Platform: PlatformDiscord}
	telegram := &CommandContext{Platform: PlatformTelegram}
	other := &CommandContext{Platform: ""}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"discord.Bold", discord.Bold("hi"), "**hi**"},
		{"telegram.Bold", telegram.Bold("hi"), "*hi*"},
		{"other.Bold", other.Bold("hi"), "hi"},
		{"discord.Bold escapes input", discord.Bold("a*b"), `**a\*b**`},
		{"telegram.Bold escapes input", telegram.Bold("a*b"), `*a\*b*`},
		{"discord.Italic", discord.Italic("hi"), "_hi_"},
		{"telegram.Italic", telegram.Italic("hi"), "_hi_"},
		{"discord.Code", discord.Code("antique_form"), "`antique_form`"},
		{"telegram.Code", telegram.Code("antique_form"), "`antique_form`"},
		{"telegram.Code escapes backtick", telegram.Code("a`b"), "`a\\`b`"},
		{"discord.EscapeForReply", discord.EscapeForReply("antique_form"), `antique\_form`},
		{"telegram.EscapeForReply", telegram.EscapeForReply("antique_form"), `antique\_form`},
		{"other.EscapeForReply", other.EscapeForReply("antique_form"), "antique_form"},
		{"empty Bold", discord.Bold(""), ""},
		{"empty Code", telegram.Code(""), ""},
		{"discord.CodeBlock", discord.CodeBlock("hi"), "```\nhi\n```"},
		{"telegram.CodeBlock", telegram.CodeBlock("hi"), "```\nhi\n```"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
