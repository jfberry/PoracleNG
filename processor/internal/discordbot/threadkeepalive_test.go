package discordbot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TestCleanupDeadThreadsUnder verifies that when a master/parent channel is
// confirmed gone in Discord, the keep-alive sweeper deletes the orphaned
// thread humans rows AND purges them from both in-process caches — so the
// next sweep no longer walks the dead parent and the warning stops firing.
func TestCleanupDeadThreadsUnder(t *testing.T) {
	const deadMaster = "deadMaster"
	const liveMaster = "liveMaster"

	humans := store.NewMockHumanStore()
	for _, id := range []string{"t1", "t2", "t3", "t4"} {
		humans.AddHuman(&store.Human{ID: id, Type: bot.TypeDiscordThread})
	}

	tmpDir := t.TempDir()

	// threadCache: t1, t2 live under the dead master; t3 under a live master.
	tc := newThreadCache(filepath.Join(tmpDir, "config", ".cache", "autocreate-threads.json"))
	tc.ensureMaster("g1", deadMaster)
	tc.upsertThread(deadMaster, threadCacheEntry{ThreadID: "t1", Label: "a"})
	tc.upsertThread(deadMaster, threadCacheEntry{ThreadID: "t2", Label: "b"})
	tc.ensureMaster("g1", liveMaster)
	tc.upsertThread(liveMaster, threadCacheEntry{ThreadID: "t3", Label: "c"})

	// autocreate-sync cache: t4 hosted under a channel whose ID is the dead master.
	as := newAutocreateSyncer()
	as.cache = autocreateCache{
		"rule1": &autocreateRuleState{
			Fences: map[string]*autocreateFenceState{
				"fence1": {
					ChannelID:  deadMaster,
					ChannelIDs: map[string]string{"chan": deadMaster},
					ThreadIDs:  map[string]map[string]string{"chan": {"d": "t4"}},
				},
			},
		},
	}

	b := &Bot{
		BotDeps:        bot.BotDeps{Humans: humans, Cfg: &config.Config{BaseDir: tmpDir}},
		threadCache:    tc,
		autocreateSync: as,
	}
	k := &threadKeepAlive{bot: b}

	parents := map[string]string{
		"t1": deadMaster, "t2": deadMaster, "t4": deadMaster, "t3": liveMaster,
	}
	managed := map[string]bool{"t1": true, "t2": true, "t3": true, "t4": true}

	k.cleanupDeadThreadsUnder(deadMaster, parents, managed)

	// Rows under the dead master are deleted; the live-master row survives.
	for _, id := range []string{"t1", "t2", "t4"} {
		if h, _ := humans.Get(id); h != nil {
			t.Errorf("thread %s should have been deleted but still exists", id)
		}
	}
	if h, _ := humans.Get("t3"); h == nil {
		t.Errorf("thread t3 under live master should not have been deleted")
	}

	// threadCache: dead master section removed, live master untouched.
	if _, ok := tc.master(deadMaster); ok {
		t.Errorf("threadCache still has dead master %s", deadMaster)
	}
	if _, ok := tc.master(liveMaster); !ok {
		t.Errorf("threadCache lost live master %s", liveMaster)
	}

	// autocreate-sync cache: the thread under the dead channel is purged.
	if fs := as.cache["rule1"].Fences["fence1"]; len(fs.ThreadIDs) != 0 {
		t.Errorf("autocreate cache still has threads under dead parent: %v", fs.ThreadIDs)
	}
}

func TestThreadKeepAliveDuration_RespectsConfig(t *testing.T) {
	if d := keepAliveTickerDuration(0); d != 0 {
		t.Errorf("0 → 0 (disabled), got %v", d)
	}
	if d := keepAliveTickerDuration(24); d != 24*time.Hour {
		t.Errorf("24 → 24h, got %v", d)
	}
	if d := keepAliveTickerDuration(200); d != 168*time.Hour {
		t.Errorf("200 → clamped to 168h, got %v", d)
	}
	if d := keepAliveTickerDuration(-5); d != 0 {
		t.Errorf("-5 → 0, got %v", d)
	}
}
