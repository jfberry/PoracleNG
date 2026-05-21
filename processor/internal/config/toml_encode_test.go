package config

import (
	"regexp"
	"slices"
	"testing"
)

// TestEncodeOrderedTOML_SectionOrder — known sections come out in
// SectionOrder priority; unknown sections fall to an alphabetical
// tail. Pins the operator-facing file layout.
func TestEncodeOrderedTOML_SectionOrder(t *testing.T) {
	raw := map[string]any{
		"alerter":   map[string]any{"api_secret": "x"},
		"ai":        map[string]any{"enabled": false},
		"discord":   map[string]any{"prefix": "!"},
		"database":  map[string]any{"host": "localhost"},
		"general":   map[string]any{"locale": "en"},
		"processor": map[string]any{"port": int64(3030)},
		"geofence":  map[string]any{"paths": []any{"x.json"}},
		"locale":    map[string]any{"available_languages": map[string]any{}},
		// Sections that fall to the alphabetical tail. "ai" / "alerter"
		// also tail-end now — they used to be priority-listed.
		"zzz_unknown": map[string]any{"foo": "bar"},
		"aaa_unknown": map[string]any{"foo": "bar"},
	}
	out, err := EncodeOrderedTOML(raw)
	if err != nil {
		t.Fatalf("EncodeOrderedTOML: %v", err)
	}

	// Pull section headers in the order they appear.
	re := regexp.MustCompile(`(?m)^\[([a-z_][a-z0-9_]*)\]`)
	matches := re.FindAllStringSubmatch(string(out), -1)
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, m[1])
	}

	// Expected: SectionOrder priority first, then alphabetical tail.
	want := []string{
		"processor", "database", "geofence", "discord", "telegram",
		"general", "locale", "logging", "webhookLogging",
		"aaa_unknown", "ai", "alerter", "zzz_unknown",
	}
	// Trim want to only sections present in raw — the test inputs
	// don't include every SectionOrder entry.
	present := make(map[string]bool, len(raw))
	for k := range raw {
		present[k] = true
	}
	filtered := make([]string, 0, len(want))
	for _, n := range want {
		if present[n] {
			filtered = append(filtered, n)
		}
	}
	if !slices.Equal(got, filtered) {
		t.Errorf("section order:\ngot:  %v\nwant: %v\n\nemitted:\n%s", got, filtered, out)
	}
}

// TestEncodeOrderedTOML_BlankLinesBetweenSections — operators read the
// file by skimming; each section needs a blank line above it.
func TestEncodeOrderedTOML_BlankLinesBetweenSections(t *testing.T) {
	raw := map[string]any{
		"processor": map[string]any{"port": int64(3030)},
		"discord":   map[string]any{"prefix": "!"},
	}
	out, err := EncodeOrderedTOML(raw)
	if err != nil {
		t.Fatalf("EncodeOrderedTOML: %v", err)
	}
	// The exact pattern: every [section] header (except the first)
	// must be preceded by a blank line.
	re := regexp.MustCompile(`(?m)^[^\n]*\n\[discord\]`)
	header := re.FindString(string(out))
	if header == "" {
		t.Fatalf("no [discord] header found:\n%s", out)
	}
	// The line immediately preceding [discord] should be blank.
	if header[0] != '\n' {
		t.Errorf("expected blank line before [discord]; got line: %q\n\nfull:\n%s", header, out)
	}
}
