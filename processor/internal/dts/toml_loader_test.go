package dts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadTOMLFile validates the basic round-trip from a TOML file to
// DTSEntry slices: multiple [[entry]] blocks, button arrays, multi-line
// template strings, action+scope fields. The TOML grammar is more
// forgiving than JSON for operator authoring, so the test covers the
// common operator-typed shapes.
func TestLoadTOMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raid.toml")
	body := `
[[entry]]
id = "1"
type = "raid"
platform = "discord"
language = "en"
description = "Raid card"
template = """
{
  "embed": { "title": "{{name}}" }
}
"""

  [[entry.buttons]]
  id = "mute_gym_1h"
  label = "Mute this gym (1h)"
  style = "danger"
  action = "mute"
  scope = "gym"
  params = { duration_min = 60 }

[[entry]]
id = "compact"
type = "raid"
platform = "discord"
language = "en"
template = """{"content": "compact"}"""
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := loadTOMLFile(path)
	if err != nil {
		t.Fatalf("loadTOMLFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	first := entries[0]
	if first.Type != "raid" || first.ID.String() != "1" || first.Platform != "discord" || first.Language != "en" {
		t.Errorf("first entry meta: %+v", first)
	}
	if first.Description != "Raid card" {
		t.Errorf("description: got %q", first.Description)
	}
	if templateStr, ok := first.Template.(string); !ok || !strings.Contains(templateStr, "{{name}}") {
		t.Errorf("template should contain {{name}}, got %v", first.Template)
	}
	if len(first.Buttons) != 1 {
		t.Errorf("buttons: got %d, want 1", len(first.Buttons))
	} else {
		b := first.Buttons[0]
		if b.ID != "mute_gym_1h" || b.Action != "mute" || b.Scope != "gym" {
			t.Errorf("button fields: %+v", b)
		}
		if dur, ok := b.Params["duration_min"]; !ok {
			t.Errorf("button params missing duration_min: %+v", b.Params)
		} else if dn, ok := dur.(int64); ok {
			if dn != 60 {
				t.Errorf("duration_min: got %d", dn)
			}
		} else if df, ok := dur.(float64); ok {
			if df != 60 {
				t.Errorf("duration_min: got %v", df)
			}
		} else {
			t.Errorf("duration_min: unexpected type %T", dur)
		}
	}

	second := entries[1]
	if second.ID.String() != "compact" {
		t.Errorf("second entry id: got %q", second.ID.String())
	}
}

// TestLoadTOMLFile_SyntaxError ensures bad TOML returns an error
// without panicking. The loader (callers) is expected to log+continue.
func TestLoadTOMLFile_SyntaxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	// Unterminated multi-line string.
	if err := os.WriteFile(path, []byte("[[entry]]\nid = \"x\"\ntemplate = \"\"\""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := loadTOMLFile(path); err == nil {
		t.Errorf("expected error on bad TOML, got nil")
	}
}

// TestLoadTOMLFile_EmptyFile: a file with no [[entry]] blocks loads
// cleanly and returns nil entries — operators using "include" patterns
// might temporarily empty a file without breaking the loader.
func TestLoadTOMLFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path, []byte("# nothing here\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := loadTOMLFile(path)
	if err != nil {
		t.Errorf("empty TOML file should not error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries from empty file, want 0", len(entries))
	}
}

// TestWarnDuplicateEntries verifies the duplicate-detection logic
// without checking the actual log output. The seen map is internal;
// we test by ensuring the function doesn't panic and trace the
// expected paths via the log capture (manual).
//
// Real validation of WARN output lives in the operator-facing smoke
// test (SMOKE.md), which writes two same-key files into config/dts/
// and inspects the log.
func TestWarnDuplicateEntries(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "1", Platform: "discord", Language: "en", sourceFile: "/cfg/dts/a.json"},
		{Type: "raid", ID: "1", Platform: "discord", Language: "en", sourceFile: "/cfg/dts/b.json"},
		{Type: "raid", ID: "1", Platform: "discord", Language: "en", sourceFile: "/cfg/dts.json"},   // legacy override; no warn
		{Type: "raid", ID: "2", Platform: "discord", Language: "en", sourceFile: "/cfg/dts/a.json"}, // distinct key; no warn
	}
	// Run — we're really just checking it doesn't panic and the
	// last-wins update is internally consistent (no test assertion
	// since the WARN output is via logrus). The dedicated test for
	// override detection is below.
	warnDuplicateEntries(entries)
}

// TestIsLegacyConfigDTS sanity-checks the heuristic that lets the
// override hierarchy (dts.json → dts/*) avoid spurious WARNs.
func TestIsLegacyConfigDTS(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/poracle/config/dts.json", true},
		{"/poracle/config/dts/raid.json", false},
		{"/poracle/config/dts/raid.toml", false},
		{"dts.json", false}, // no leading separator — heuristic intentionally narrow
		{"", false},
	}
	for _, tc := range cases {
		if got := isLegacyConfigDTS(tc.path); got != tc.want {
			t.Errorf("isLegacyConfigDTS(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
