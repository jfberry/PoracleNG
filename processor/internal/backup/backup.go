// Package backup centralises file-snapshot logic used by the config and
// template editor endpoints. Every write that overwrites a user-editable
// file routes through here first so the previous version is preserved
// under <baseDir>/backups/, and the cleanup sweeper bounds disk use to
// the retention window.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Retention is how long a backup is kept before the cleanup sweeper
// removes it. Hard-coded for now; promote to config when an operator
// asks for it.
const Retention = 7 * 24 * time.Hour

// CleanupInterval is how often the sweeper rewalks the backup tree.
// Daily is plenty given the 7-day retention.
const CleanupInterval = 24 * time.Hour

// dirName is the directory under baseDir that holds backups. Sibling to
// the live config files (config/backups vs config/dts, config/cache).
// Discoverable on purpose — no leading dot.
const dirName = "backups"

// Save snapshots the current contents of <baseDir>/<relPath> into
// <baseDir>/backups/<relPath>.bak.<timestamp>, creating any intermediate
// directories. Returns the backup's path relative to baseDir, or "" if
// there was no live file to back up (first-time save). Errors otherwise.
//
// Mirrors the directory structure of the source so multi-file editors
// (DTS templates, config sections) keep their backups grouped under the
// same relative path they live at.
func Save(baseDir, relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("backup.Save: relPath is required")
	}
	livePath := filepath.Join(baseDir, relPath)
	data, err := os.ReadFile(livePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil // first save — nothing to back up
		}
		return "", fmt.Errorf("backup.Save: read live %s: %w", relPath, err)
	}

	ts := time.Now().Format("2006-01-02_150405")
	backupRel := filepath.Join(dirName, relPath+".bak."+ts)
	backupAbs := filepath.Join(baseDir, backupRel)

	if err := os.MkdirAll(filepath.Dir(backupAbs), 0o755); err != nil {
		return "", fmt.Errorf("backup.Save: mkdir %s: %w", filepath.Dir(backupRel), err)
	}
	if err := os.WriteFile(backupAbs, data, 0o644); err != nil {
		return "", fmt.Errorf("backup.Save: write %s: %w", backupRel, err)
	}
	return backupRel, nil
}

// Cleanup walks <baseDir>/backups/ and removes regular files whose mtime
// is older than retention. Best-effort: a per-file error is logged and
// the walk continues. Returns the count of files removed and the first
// error encountered (if any). A missing backup tree is not an error.
func Cleanup(baseDir string, retention time.Duration) (int, error) {
	root := filepath.Join(baseDir, dirName)
	cutoff := time.Now().Add(-retention)
	removed := 0
	var firstErr error

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return nil
			}
			removed++
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) && firstErr == nil {
		firstErr = walkErr
	}
	if errors.Is(firstErr, os.ErrNotExist) {
		// Tree doesn't exist yet — that's fine.
		return 0, nil
	}
	return removed, firstErr
}

// StartCleanupSweeper runs Cleanup once immediately and then on a ticker
// at CleanupInterval until ctx is cancelled. Returns a stop function the
// caller calls on shutdown to wait for the goroutine to exit cleanly.
// Errors from Cleanup are logged + swallowed; this is hygiene, not
// load-bearing.
func StartCleanupSweeper(ctx context.Context, baseDir string, retention time.Duration) func() {
	derived, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runOnce := func() {
			n, err := Cleanup(baseDir, retention)
			if err != nil {
				log.Warnf("backup cleanup: %v", err)
			}
			if n > 0 {
				log.Infof("backup cleanup: removed %d file(s) older than %s", n, retention)
			}
		}
		runOnce()
		t := time.NewTicker(CleanupInterval)
		defer t.Stop()
		for {
			select {
			case <-derived.Done():
				return
			case <-t.C:
				runOnce()
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
