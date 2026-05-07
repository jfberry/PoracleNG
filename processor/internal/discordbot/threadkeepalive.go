package discordbot

import (
	"context"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// threadKeepAlive keeps Poracle-managed Discord threads alive by
// unarchiving them on a schedule. Runs one sweep at startup, then on a
// timer at the configured cadence. The sweeper is independent of
// autocreate — it scans humans rows of type discord:thread, so it
// covers manual !channel add, interactive !autocreate, and bulk sync
// equally.
type threadKeepAlive struct {
	bot      *Bot
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// keepAliveTickerDuration converts the [discord]
// thread_keep_alive_interval_hours config into a Duration, applying
// the upper-bound clamp (168h = 7d) and treating zero/negative as
// "disabled".
func keepAliveTickerDuration(hours int) time.Duration {
	if hours <= 0 {
		return 0
	}
	if hours > 168 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}

// startThreadKeepAlive spawns the background sweeper. Returns a stop
// function that cancels the goroutine and waits for it to exit. Returns
// a no-op stop fn when keep-alive is disabled.
func startThreadKeepAlive(b *Bot, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	k := &threadKeepAlive{bot: b, interval: interval, cancel: cancel}
	k.wg.Add(1)
	go k.run(ctx)
	return func() {
		cancel()
		k.wg.Wait()
	}
}

func (k *threadKeepAlive) run(ctx context.Context) {
	defer k.wg.Done()

	// Run once immediately at startup — a processor that was down for
	// >7 days needs everything revived on first run.
	k.sweep(ctx)

	t := time.NewTicker(k.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.sweep(ctx)
		}
	}
}

func (k *threadKeepAlive) sweep(ctx context.Context) {
	if k.bot == nil || k.bot.Humans == nil || k.bot.session == nil {
		return
	}
	threads, err := k.bot.Humans.ListByType(bot.TypeDiscordThread)
	if err != nil {
		log.Warnf("thread keep-alive: list discord:thread humans: %v", err)
		return
	}
	if len(threads) == 0 {
		return
	}

	// Build a threadID → parentID map from the two in-process caches to avoid
	// a REST round-trip per thread. We collect under each cache's mutex once,
	// then release before doing per-thread work.

	// 1. threadCache (picker-button / interactive !autocreate flow).
	parents := k.bot.threadCache.parentByThread()

	// 2. autocreateSync cache (bulk-sync / autocreate.json on disk).
	// Take the global mu briefly to copy out the IDs, then release.
	k.bot.autocreateSync.mu.Lock()
	for _, ruleState := range k.bot.autocreateSync.cache {
		if ruleState == nil {
			continue
		}
		for _, fs := range ruleState.Fences {
			if fs == nil || fs.ChannelID == "" {
				continue
			}
			for _, tid := range fs.ThreadIDs {
				if tid != "" {
					if _, exists := parents[tid]; !exists {
						parents[tid] = fs.ChannelID
					}
				}
			}
		}
	}
	k.bot.autocreateSync.mu.Unlock()

	// Resolve parent channel for each managed thread.
	// Use the cache where possible; fall back to s.Channel() only for threads
	// neither cache knows about (e.g. those registered via legacy !channel add).
	parentsToWalk := map[string]bool{}
	for _, h := range threads {
		if parentID, ok := parents[h.ID]; ok {
			parentsToWalk[parentID] = true
			continue
		}
		ch, err := k.bot.session.Channel(h.ID)
		if err != nil {
			log.Debugf("thread keep-alive: Channel(%s): %v (likely deleted, skipping)", h.ID, err)
			continue
		}
		parents[h.ID] = ch.ParentID
		parentsToWalk[ch.ParentID] = true
	}

	// For each unique parent, page through its archived private threads
	// and unarchive any whose ID is in our managed set.
	managed := map[string]bool{}
	for _, h := range threads {
		managed[h.ID] = true
	}
	for parentID := range parentsToWalk {
		if ctx.Err() != nil {
			return
		}
		k.revivePrivateArchived(ctx, parentID, managed)
	}
}

// revivePrivateArchived pages through ThreadsPrivateArchived for one
// parent channel, unarchiving any thread whose ID is in `managed`.
func (k *threadKeepAlive) revivePrivateArchived(ctx context.Context, parentID string, managed map[string]bool) {
	var before *time.Time
	const pageLimit = 100
	for {
		if ctx.Err() != nil {
			return
		}
		page, err := k.bot.session.ThreadsPrivateArchived(parentID, before, pageLimit)
		if err != nil {
			log.Warnf("thread keep-alive: ThreadsPrivateArchived(%s): %v", parentID, err)
			return
		}
		for _, thread := range page.Threads {
			if !managed[thread.ID] {
				continue
			}
			f := false
			if _, err := k.bot.session.ChannelEdit(thread.ID, &discordgo.ChannelEdit{Archived: &f}); err != nil {
				log.Warnf("thread keep-alive: unarchive %s: %v", thread.ID, err)
				continue
			}
			log.Infof("thread keep-alive: unarchived %s under %s", thread.ID, parentID)
		}
		if !page.HasMore || len(page.Threads) == 0 {
			return
		}
		// Page using the oldest thread's archive timestamp.
		oldest := page.Threads[len(page.Threads)-1]
		if oldest.ThreadMetadata == nil {
			return
		}
		t := oldest.ThreadMetadata.ArchiveTimestamp
		before = &t
	}
}
