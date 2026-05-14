package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(live, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := Save(dir, "config.toml")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(rel, "backups/config.toml.bak.") {
		t.Errorf("rel = %q, want backups/config.toml.bak.<ts>", rel)
	}
	got, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("backup content = %q, want hello", got)
	}
}

func TestSave_NestedRelPath(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "dts", "fort_update.txt")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("template"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := Save(dir, "dts/fort_update.txt")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(rel, filepath.Join("backups", "dts", "fort_update.txt.bak.")) {
		t.Errorf("rel = %q, want backups/dts/fort_update.txt.bak.<ts>", rel)
	}
	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		t.Errorf("backup file missing: %v", err)
	}
}

func TestSave_MissingLiveFile(t *testing.T) {
	dir := t.TempDir()
	rel, err := Save(dir, "does-not-exist.json")
	if err != nil {
		t.Fatalf("missing live file should not error, got %v", err)
	}
	if rel != "" {
		t.Errorf("rel = %q, want empty (no backup made)", rel)
	}
}

func TestCleanup_RemovesOldKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(backupDir, "old.bak")
	recent := filepath.Join(backupDir, "recent.bak")
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Backdate `old` to 10 days ago. Use a time well past the 7-day cutoff.
	past := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	removed, err := Cleanup(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old.bak should be gone, got err=%v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent.bak should remain: %v", err)
	}
}

func TestCleanup_MissingDirIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	// No backups/ subdir at all — Cleanup should be a no-op.
	if removed, err := Cleanup(dir, time.Hour); err != nil || removed != 0 {
		t.Errorf("missing tree: removed=%d err=%v, want 0/nil", removed, err)
	}
}

func TestCleanup_PreservesNestedTree(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "backups", "dts", "fresh.bak")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := Cleanup(dir, 7*24*time.Hour); err != nil || removed != 0 {
		t.Errorf("recent nested file: removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("recent nested file should remain: %v", err)
	}
}

func TestStartCleanupSweeper_StopsCleanly(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	stop := StartCleanupSweeper(ctx, dir, 7*24*time.Hour)

	// stop is supposed to cancel + wait. Run it in a goroutine and assert
	// it returns within a small budget.
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not return within 2s")
	}
}
