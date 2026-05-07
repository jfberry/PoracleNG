package discordbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadChannelTemplatesRaw_MissingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadChannelTemplatesRaw(dir)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("missing file should return [], got %s", got)
	}
}

func TestSaveChannelTemplatesRaw_RoundtripWithBackup(t *testing.T) {
	dir := t.TempDir()
	first := []byte(`[{"name":"a","definition":{"channels":[{"channelName":"c1"}]}}]`)
	if backup, err := SaveChannelTemplatesRaw(dir, first); err != nil {
		t.Fatalf("first save: %v", err)
	} else if backup != "" {
		t.Errorf("first save should not produce a backup, got %q", backup)
	}

	// Second save should produce a backup of the first.
	second := []byte(`[{"name":"a","definition":{"channels":[{"channelName":"c2"}]}}]`)
	backup, err := SaveChannelTemplatesRaw(dir, second)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if !strings.HasPrefix(backup, "channelTemplate.json.bak.") {
		t.Errorf("backup name = %q, want prefix channelTemplate.json.bak.", backup)
	}
	backupBytes, err := os.ReadFile(filepath.Join(dir, "config", backup))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.Contains(string(backupBytes), `"channelName": "c1"`) && !strings.Contains(string(backupBytes), `"channelName":"c1"`) {
		t.Errorf("backup should contain the first version, got %s", backupBytes)
	}

	// Live file should now be the second version.
	live, err := LoadChannelTemplatesRaw(dir)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if !strings.Contains(string(live), `"c2"`) {
		t.Errorf("live should contain c2, got %s", live)
	}
}

func TestValidateChannelTemplatesRaw(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		errors int
		warns  int
	}{
		{"clean", `[{"name":"a","definition":{"channels":[{"channelName":"c1"}]}}]`, 0, 0},
		{"missing name", `[{"definition":{"channels":[{"channelName":"c1"}]}}]`, 1, 0},
		{"name with space", `[{"name":"a b","definition":{"channels":[{"channelName":"c1"}]}}]`, 1, 0},
		{"duplicate", `[{"name":"a","definition":{"channels":[{"channelName":"c1"}]}},{"name":"a","definition":{"channels":[{"channelName":"c2"}]}}]`, 1, 0},
		{"no channels", `[{"name":"a","definition":{"channels":[]}}]`, 1, 0},
		{"bad channelType", `[{"name":"a","definition":{"channels":[{"channelName":"c1","channelType":"bogus"}]}}]`, 1, 0},
		{"bad controlType", `[{"name":"a","definition":{"channels":[{"channelName":"c1","controlType":"bogus"}]}}]`, 1, 0},
		{"bad buttonStyle", `[{"name":"a","definition":{"channels":[{"channelName":"c1","threads":[{"name":"t","buttonStyle":"weird"}]}]}}]`, 1, 0},
		{"voice with topic — warning only", `[{"name":"a","definition":{"channels":[{"channelName":"c1","channelType":"voice","topic":"oops"}]}}]`, 0, 1},
		{"picker without threads — warning only", `[{"name":"a","definition":{"channels":[{"channelName":"c1","threadPicker":{"embedTitle":"T"}}]}}]`, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateChannelTemplatesRaw([]byte(tc.body))
			gotE, gotW := 0, 0
			for _, e := range errs {
				switch e.Severity {
				case "error":
					gotE++
				case "warning":
					gotW++
				}
			}
			if gotE != tc.errors {
				t.Errorf("error count = %d, want %d (full = %+v)", gotE, tc.errors, errs)
			}
			if gotW != tc.warns {
				t.Errorf("warning count = %d, want %d (full = %+v)", gotW, tc.warns, errs)
			}
		})
	}
}

func TestDeleteChannelTemplate(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`[{"name":"a","definition":{"channels":[{"channelName":"c1"}]}},{"name":"b","definition":{"channels":[{"channelName":"c2"}]}}]`)
	if _, err := SaveChannelTemplatesRaw(dir, body); err != nil {
		t.Fatalf("seed: %v", err)
	}
	backup, err := DeleteChannelTemplate(dir, "a")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if backup == "" {
		t.Errorf("expected a backup name from delete")
	}
	live, _ := LoadChannelTemplatesRaw(dir)
	var got []ChannelTemplate
	if err := json.Unmarshal(live, &got); err != nil {
		t.Fatalf("re-parse live: %v", err)
	}
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("after delete: got %+v, want only b", got)
	}

	if _, err := DeleteChannelTemplate(dir, "nope"); !os.IsNotExist(err) {
		t.Errorf("delete non-existent should return os.ErrNotExist, got %v", err)
	}
}

func TestPermissionFlagsList_NonEmpty(t *testing.T) {
	flags := PermissionFlagsList()
	if len(flags) == 0 {
		t.Fatal("PermissionFlagsList should not be empty")
	}
	// Smoke-check the source-of-truth invariant: every key in
	// rolePermissionFlags must appear in the list.
	seen := map[string]bool{}
	for _, f := range flags {
		seen[f.Name] = true
	}
	for k := range rolePermissionFlags {
		if !seen[k] {
			t.Errorf("rolePermissionFlags key %q missing from PermissionFlagsList", k)
		}
	}
}
