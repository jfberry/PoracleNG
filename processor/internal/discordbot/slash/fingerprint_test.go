package slash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestFingerprintStable(t *testing.T) {
	cmds := []*discordgo.ApplicationCommand{
		{Name: "version", Description: "Show version"},
	}
	fp1 := Fingerprint(cmds)
	fp2 := Fingerprint(cmds)
	if fp1 != fp2 {
		t.Errorf("unstable: %s != %s", fp1, fp2)
	}
	if len(fp1) != 16 {
		t.Errorf("len(fp)=%d, want 16", len(fp1))
	}
}

func TestFingerprintChangeDetected(t *testing.T) {
	a := []*discordgo.ApplicationCommand{{Name: "version", Description: "Show version"}}
	b := []*discordgo.ApplicationCommand{{Name: "version", Description: "Show V2"}}
	if Fingerprint(a) == Fingerprint(b) {
		t.Error("change not detected")
	}
}

func TestFingerprintIgnoresOrder(t *testing.T) {
	a := []*discordgo.ApplicationCommand{
		{Name: "a", Description: "A"},
		{Name: "b", Description: "B"},
	}
	b := []*discordgo.ApplicationCommand{
		{Name: "b", Description: "B"},
		{Name: "a", Description: "A"},
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("should ignore order")
	}
}

func TestFingerprintCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Path: filepath.Join(dir, "fp.json")}

	c.Global = CacheEntry{Fingerprint: "abc", SyncedAt: time.Now()}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	var loaded Cache
	loaded.Path = c.Path
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if loaded.Global.Fingerprint != "abc" {
		t.Errorf("got %q", loaded.Global.Fingerprint)
	}
}

// TestSyncSkipsWhenFingerprintMatches verifies the cache hit path: when the
// cache file contains the same fingerprint as the intended command set and
// ForceSync is false, SyncCommands must not hit the Discord API.
func TestSyncSkipsWhenFingerprintMatches(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "fp.json")

	d := NewDispatcher(Config{Enabled: true, Global: true, CachePath: cachePath})
	d.appID = "app123"
	fs := &fakeSession{}
	d.commandsAPI = fs

	// Pre-compute the fingerprint of what SyncCommands will produce.
	intent := AllDefinitions(d.bundle, d.cfg.Enable)
	want := Fingerprint(intent)

	seed := Cache{
		Path:   cachePath,
		Global: CacheEntry{Fingerprint: want, SyncedAt: time.Now()},
		Guilds: map[string]CacheEntry{},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := d.SyncCommands(); err != nil {
		t.Fatal(err)
	}
	if len(fs.called) != 0 {
		t.Errorf("expected 0 API calls on cache hit, got %d (%v)", len(fs.called), fs.called)
	}
}

// TestSyncForcesWhenForceSyncSet verifies ForceSync=true bypasses a cache hit
// and still pushes to Discord.
func TestSyncForcesWhenForceSyncSet(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "fp.json")

	d := NewDispatcher(Config{Enabled: true, Global: true, CachePath: cachePath, ForceSync: true})
	d.appID = "app123"
	fs := &fakeSession{}
	d.commandsAPI = fs

	intent := AllDefinitions(d.bundle, d.cfg.Enable)
	want := Fingerprint(intent)

	seed := Cache{
		Path:   cachePath,
		Global: CacheEntry{Fingerprint: want, SyncedAt: time.Now()},
		Guilds: map[string]CacheEntry{},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := d.SyncCommands(); err != nil {
		t.Fatal(err)
	}
	if len(fs.called) != 1 {
		t.Errorf("expected 1 API call with ForceSync=true, got %d", len(fs.called))
	}
}
