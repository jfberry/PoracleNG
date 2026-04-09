package dts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmojiCustomOverride(t *testing.T) {
	dir := t.TempDir()
	writeEmojiJSON(t, dir, `{
		"discord": {"fire": "<:fire:123>", "water": "<:water:456>"},
		"telegram": {"fire": "\ud83d\udd25"}
	}`)

	defaults := map[string]string{
		"fire":  "F",
		"water": "W",
		"grass": "G",
	}

	e := LoadEmoji(dir, defaults)

	// Custom override for discord
	if got := e.Lookup("fire", "discord"); got != "<:fire:123>" {
		t.Errorf("discord fire: got %q", got)
	}

	// Custom override for telegram
	if got := e.Lookup("fire", "telegram"); got != "\U0001f525" {
		t.Errorf("telegram fire: got %q", got)
	}
}

func TestEmojiDefaultFallback(t *testing.T) {
	dir := t.TempDir()
	writeEmojiJSON(t, dir, `{"discord": {"fire": "<:fire:123>"}}`)

	defaults := map[string]string{
		"fire":  "F",
		"grass": "G",
	}

	e := LoadEmoji(dir, defaults)

	// Key not in custom for discord → falls back to default
	if got := e.Lookup("grass", "discord"); got != "G" {
		t.Errorf("default fallback: got %q", got)
	}

	// Platform not in custom → falls back to default
	if got := e.Lookup("fire", "webhook"); got != "F" {
		t.Errorf("missing platform: got %q", got)
	}
}

func TestEmojiMissingKey(t *testing.T) {
	dir := t.TempDir()
	defaults := map[string]string{"fire": "F"}
	e := LoadEmoji(dir, defaults) // No emoji.json

	// Missing key returns ""
	if got := e.Lookup("unknown", "discord"); got != "" {
		t.Errorf("missing key: got %q", got)
	}
}

func TestEmojiNilDefaults(t *testing.T) {
	dir := t.TempDir()
	e := LoadEmoji(dir, nil)

	// Should not panic, returns ""
	if got := e.Lookup("fire", "discord"); got != "" {
		t.Errorf("nil defaults: got %q", got)
	}
}

func TestEmojiNoFile(t *testing.T) {
	dir := t.TempDir()
	defaults := map[string]string{"fire": "F"}
	e := LoadEmoji(dir, defaults)

	// No emoji.json — only defaults
	if got := e.Lookup("fire", "discord"); got != "F" {
		t.Errorf("no file: got %q", got)
	}
}

func TestEmojiNilLookup(t *testing.T) {
	var e *EmojiLookup
	if got := e.Lookup("fire", "discord"); got != "" {
		t.Errorf("nil receiver: got %q", got)
	}
}

func writeEmojiJSON(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "emoji.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
