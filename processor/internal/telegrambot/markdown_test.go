package telegrambot

import "testing"

func TestMarkdownToHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Plain text
		{"plain", "hello world", "hello world"},
		{"empty", "", ""},

		// HTML escape of literal text
		{"escape lt", "1 < 2", "1 &lt; 2"},
		{"escape gt", "a > b", "a &gt; b"},
		{"escape amp", "Tom & Jerry", "Tom &amp; Jerry"},
		{"escape <pokemon>", "Use <pokemon>", "Use &lt;pokemon&gt;"},

		// Bold
		{"bold", "**hi**", "<b>hi</b>"},
		{"bold mid", "Hello **world**!", "Hello <b>world</b>!"},
		{"bold unmatched", "**hello", "**hello"},
		{"bold empty", "****", "<b></b>"},

		// Italic
		{"italic", "_hi_", "<i>hi</i>"},
		{"italic mid", "Hello _world_!", "Hello <i>world</i>!"},
		{"italic unmatched", "_hello", "_hello"},
		{"single underscore literal", "antique_form", "antique_form"},
		{"two underscores pair", "antique_form_x", "antique<i>form</i>x"},

		// Code
		{"code", "`x`", "<code>x</code>"},
		{"code with underscores", "`antique_form`", "<code>antique_form</code>"},
		{"code with html", "`<b>`", "<code>&lt;b&gt;</code>"},
		{"code unmatched", "`hello", "`hello"},
		{"code with escaped backtick", "`a\\`b`", "<code>a`b</code>"},

		// Pre block
		{"pre block", "```\nhi\n```", "<pre>\nhi\n</pre>"},
		{"pre with markdown inside", "```\n**not bold**\n```", "<pre>\n**not bold**\n</pre>"},

		// Backslash escape
		{"escape asterisk", `\*not bold\*`, "*not bold*"},
		{"escape underscore", `antique\_form`, "antique_form"},
		{"escape backtick", "\\`x\\`", "`x`"},
		{"escape backslash", `a\\b`, `a\b`},

		// Mixed / nested
		{"bold and italic", "**hi** and _there_", "<b>hi</b> and <i>there</i>"},
		{"italic in bold", "**a _b_ c**", "<b>a <i>b</i> c</b>"},
		{"code keeps markdown literal", "`*not bold*`", "<code>*not bold*</code>"},

		// Link
		{"link", "[click](https://example.com)", `<a href="https://example.com">click</a>`},
		{"link with text formatting", "[**bold**](https://example.com)", `<a href="https://example.com"><b>bold</b></a>`},
		{"unmatched bracket", "[hello", "[hello"},
		{"bracket no parens", "[hello]", "[hello]"},

		// Realistic
		{"info form list", "`Sinistea form:antique_form`", "<code>Sinistea form:antique_form</code>"},
		{"bold name with paren", "**Sinistea** (#854)", "<b>Sinistea</b> (#854)"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := markdownToHTML(c.in)
			if got != c.want {
				t.Errorf("markdownToHTML(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
		})
	}
}
