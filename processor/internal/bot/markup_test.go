package bot

import "testing"

func TestEscapeMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"antique_form", `antique\_form`},
		{"a*b", `a\*b`},
		{"path\\file", `path\\file`},
		{"all _*~|` and \\", "all \\_\\*\\~\\|\\` and \\\\"},
		{"brackets [x]", `brackets \[x\]`},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeMarkdown(c.in); got != c.want {
			t.Errorf("escapeMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeMarkdownCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"antique_form", "antique_form"}, // _ literal inside code
		{"some `code`", "some \\`code\\`"},
		{"path\\", "path\\\\"},
		{"plain text", "plain text"},
		{"a*b", "a*b"}, // * literal inside code
		{"3 - 1 = 2", "3 - 1 = 2"},
	}
	for _, c := range cases {
		if got := escapeMarkdownCode(c.in); got != c.want {
			t.Errorf("escapeMarkdownCode(%q) = %q, want %q", c.in, got, c.want)
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
		// Bold/italic/code emit Markdown identically on both platforms.
		{"discord.Bold", discord.Bold("hi"), "**hi**"},
		{"telegram.Bold", telegram.Bold("hi"), "**hi**"},
		{"discord.Bold escapes input", discord.Bold("a*b"), `**a\*b**`},
		{"telegram.Bold escapes input", telegram.Bold("a*b"), `**a\*b**`},
		{"discord.Italic", discord.Italic("hi"), "_hi_"},
		{"telegram.Italic", telegram.Italic("hi"), "_hi_"},
		{"discord.Code", discord.Code("antique_form"), "`antique_form`"},
		{"telegram.Code", telegram.Code("antique_form"), "`antique_form`"},
		{"discord.Code escapes backtick", discord.Code("a`b"), "`a\\`b`"},
		{"telegram.Code escapes backtick", telegram.Code("a`b"), "`a\\`b`"},
		{"discord.CodeBlock", discord.CodeBlock("hi"), "```\nhi\n```"},
		{"telegram.CodeBlock", telegram.CodeBlock("hi"), "```\nhi\n```"},

		// EscapeForReply
		{"discord.EscapeForReply", discord.EscapeForReply("antique_form"), `antique\_form`},
		{"telegram.EscapeForReply", telegram.EscapeForReply("antique_form"), `antique\_form`},

		// Empty input
		{"empty Bold", discord.Bold(""), ""},
		{"empty Code", telegram.Code(""), ""},

		// Unknown platform — still Markdown (for /api/command path).
		{"other.Bold", other.Bold("hi"), "**hi**"},
		{"other.EscapeForReply", other.EscapeForReply("antique_form"), `antique\_form`},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestCommandContextEscapeJoin(t *testing.T) {
	ctx := &CommandContext{Platform: PlatformTelegram}
	got := ctx.EscapeJoin([]string{"alpha_one", "beta*two"}, ", ")
	want := `alpha\_one, beta\*two`
	if got != want {
		t.Errorf("EscapeJoin = %q, want %q", got, want)
	}
	if got := ctx.EscapeJoin(nil, ", "); got != "" {
		t.Errorf("EscapeJoin(nil) = %q, want empty", got)
	}
}
