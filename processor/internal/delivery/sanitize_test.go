package delivery

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Discord embed sanitiser tests ---

func TestSanitizeDiscordEmbed_DropsEmptyField(t *testing.T) {
	embed := map[string]any{
		"title": "test",
		"fields": []any{
			map[string]any{"name": "", "value": ""},
			map[string]any{"name": "keep", "value": "this"},
		},
	}
	keep := sanitizeDiscordEmbed(embed)
	if !keep {
		t.Fatal("expected embed to be kept")
	}
	fields, _ := embed["fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("expected 1 field after sanitisation, got %d", len(fields))
	}
}

func TestSanitizeDiscordEmbed_BlankNameSwappedToZWS(t *testing.T) {
	embed := map[string]any{
		"title": "test",
		"fields": []any{
			map[string]any{"name": "", "value": "value-only"},
			map[string]any{"name": "name-only", "value": ""},
		},
	}
	sanitizeDiscordEmbed(embed)
	fields := embed["fields"].([]any)
	f0 := fields[0].(map[string]any)
	f1 := fields[1].(map[string]any)
	if f0["name"] != zeroWidthSpace {
		t.Errorf("expected blank name → ZWS, got %q", f0["name"])
	}
	if f0["value"] != "value-only" {
		t.Errorf("value should be preserved, got %q", f0["value"])
	}
	if f1["name"] != "name-only" {
		t.Errorf("name should be preserved, got %q", f1["name"])
	}
	if f1["value"] != zeroWidthSpace {
		t.Errorf("expected blank value → ZWS, got %q", f1["value"])
	}
}

func TestSanitizeDiscordEmbed_TruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", 2000)
	embed := map[string]any{
		"title": strings.Repeat("a", 300),
		"fields": []any{
			map[string]any{"name": "n", "value": long},
		},
	}
	sanitizeDiscordEmbed(embed)
	if title, _ := embed["title"].(string); len([]rune(title)) > maxEmbedTitle {
		t.Errorf("title not truncated: %d runes", len([]rune(title)))
	}
	field := embed["fields"].([]any)[0].(map[string]any)
	if v, _ := field["value"].(string); len([]rune(v)) > maxEmbedFieldValue {
		t.Errorf("field value not truncated: %d runes", len([]rune(v)))
	}
}

func TestSanitizeDiscordEmbed_DropsEmptyImageURL(t *testing.T) {
	embed := map[string]any{
		"title": "test",
		"image": map[string]any{"url": ""},
	}
	sanitizeDiscordEmbed(embed)
	if _, ok := embed["image"]; ok {
		t.Error("expected empty image to be dropped")
	}
}

func TestSanitizeDiscordEmbed_DropsNullStringLiterals(t *testing.T) {
	embed := map[string]any{
		"title":       "null",
		"description": "undefined",
		"image":       map[string]any{"url": "null"},
	}
	keep := sanitizeDiscordEmbed(embed)
	if _, ok := embed["title"]; ok {
		t.Error("title=\"null\" should be dropped")
	}
	if _, ok := embed["description"]; ok {
		t.Error("description=\"undefined\" should be dropped")
	}
	if _, ok := embed["image"]; ok {
		t.Error("image with url=\"null\" should be dropped")
	}
	if keep {
		t.Error("expected empty embed to be dropped")
	}
}

func TestSanitizeDiscordEmbed_DropsEmptyFooter(t *testing.T) {
	embed := map[string]any{
		"title":  "test",
		"footer": map[string]any{"text": "  "},
	}
	sanitizeDiscordEmbed(embed)
	if _, ok := embed["footer"]; ok {
		t.Error("expected whitespace-only footer to be dropped")
	}
}

func TestSanitizeDiscordEmbed_CapsFieldsAt25(t *testing.T) {
	var fields []any
	for i := 0; i < 30; i++ {
		fields = append(fields, map[string]any{"name": "n", "value": "v"})
	}
	embed := map[string]any{"title": "test", "fields": fields}
	sanitizeDiscordEmbed(embed)
	got := embed["fields"].([]any)
	if len(got) != maxEmbedFields {
		t.Errorf("expected %d fields, got %d", maxEmbedFields, len(got))
	}
}

func TestNormalizeAndExtractImage_DropsEmptyEmbedFromList(t *testing.T) {
	input := `{"embeds":[{"title":"keep"},{"image":{"url":""}}]}`
	out, _, err := NormalizeAndExtractImage(json.RawMessage(input), false)
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatal(err)
	}
	embeds := msg["embeds"].([]any)
	if len(embeds) != 1 {
		t.Errorf("expected 1 embed after sanitisation, got %d", len(embeds))
	}
}

func TestNormalizeAndExtractImage_DropsEmptyContent(t *testing.T) {
	input := `{"content":"   ","embeds":[{"title":"x"}]}`
	out, _, err := NormalizeAndExtractImage(json.RawMessage(input), false)
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	json.Unmarshal(out, &msg)
	if _, ok := msg["content"]; ok {
		t.Error("expected whitespace-only content to be dropped")
	}
}

// --- Telegram sanitiser tests ---

func TestSanitizeTelegramText_StripsEmptyMarkdownLinks(t *testing.T) {
	in := "hello [click]() world"
	out := sanitizeTelegramText(in, 4096)
	if strings.Contains(out, "[click]()") {
		t.Errorf("empty markdown link not stripped: %q", out)
	}
}

func TestSanitizeTelegramText_StripsEmptyLabelLinks(t *testing.T) {
	in := "see [](https://example.com)"
	out := sanitizeTelegramText(in, 4096)
	if strings.Contains(out, "[](") {
		t.Errorf("empty label link not stripped: %q", out)
	}
}

func TestSanitizeTelegramText_StripsNullLiterals(t *testing.T) {
	in := "name: null cp: undefined"
	out := sanitizeTelegramText(in, 4096)
	if strings.Contains(out, "null") || strings.Contains(out, "undefined") {
		t.Errorf("null literals not stripped: %q", out)
	}
}

func TestSanitizeTelegramText_Truncates(t *testing.T) {
	in := strings.Repeat("a", 5000)
	out := sanitizeTelegramText(in, maxTelegramText)
	if len([]rune(out)) > maxTelegramText {
		t.Errorf("not truncated: %d runes", len([]rune(out)))
	}
}

func TestSanitizeTelegramText_EmptyAfterStrip(t *testing.T) {
	in := "  null  "
	out := sanitizeTelegramText(in, 4096)
	if out != "" {
		t.Errorf("expected empty after stripping null/whitespace, got %q", out)
	}
}

func TestStripBlankURL(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"   ":                  "",
		"null":                 "",
		"NULL":                 "",
		"undefined":            "",
		"<nil>":                "",
		"https://example.com":  "https://example.com",
		"  https://e.com  ":    "  https://e.com  ", // only fully empty/null returns ""
	}
	for in, want := range cases {
		got := stripBlankURL(in)
		if got != want {
			t.Errorf("stripBlankURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeTelegramMessage_FullPath(t *testing.T) {
	msg := &telegramMessage{
		Content: "Hello [click]() see null below",
		Sticker: "  ",
		Photo:   "null",
	}
	sanitizeTelegramMessage(msg)
	if strings.Contains(msg.Content, "null") {
		t.Errorf("Content still has null: %q", msg.Content)
	}
	if strings.Contains(msg.Content, "[click]()") {
		t.Errorf("Content still has empty link: %q", msg.Content)
	}
	if msg.Sticker != "" {
		t.Errorf("Sticker should be empty, got %q", msg.Sticker)
	}
	if msg.Photo != "" {
		t.Errorf("Photo should be empty, got %q", msg.Photo)
	}
}
