package delivery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	log "github.com/sirupsen/logrus"
)

// TrackedMessage represents a sent message being tracked for clean deletion or editing.
type TrackedMessage struct {
	SentID   string `json:"sent_id"`
	Target   string `json:"target"`
	Type     string `json:"type"` // "discord:user", "telegram:group", etc.
	// MsgType is the source webhook alert type ("raid", "egg", "pokemon", etc.)
	// — distinct from Type, which is the destination type ("discord:user").
	// Used by the raid handler's partitionRaidUsers first-visible check.
	//
	// Backward compat: entries persisted before this field existed deserialise
	// with MsgType="". The next webhook for that user will see prior.MsgType=""
	// != current msgType (e.g. "raid"), classify as first-visible, and emit one
	// extra full raid card. Subsequent webhooks will have the new MsgType set
	// and chain to rsvpChanges normally. Acceptable failure mode (one extra
	// full card, no missed updates).
	MsgType  string `json:"msg_type,omitempty"`
	Clean    int    `json:"clean"`
	ReplyKey string `json:"reply_key,omitempty"`
}

// MessageTracker manages sent messages with TTL-based expiry and clean
// deletion. Reply linking (LookupReply / LookupReplyTargets) is layered
// on top via a single reverse index that points back into the cache by
// editKey — no separate ttlcache or duplicated SentID.
type MessageTracker struct {
	cache    *ttlcache.Cache[string, *TrackedMessage]
	senders  map[string]Sender // "discord" → sender, "telegram" → sender
	cacheDir string
	mu       sync.Mutex

	// replies is the reply-key reverse index: replyKey → target →
	// editKey. The editKey points at the latest cache entry for that
	// (replyKey, target). Maintained in Track and cleaned up by the
	// cache's OnEviction handler (which holds the TrackedMessage and
	// can read its ReplyKey / Target). Single source of truth for
	// expiration — the cache entry owning the data is the same one
	// that drives the cleanup.
	repliesMu sync.RWMutex
	replies   map[string]map[string]string

	// onEvict is invoked on every TTL eviction (clean or not) — gives
	// satellite stores like the snapshot pogreb a chance to drop their
	// per-message records alongside the tracker entry. Optional; nil =
	// no extra cleanup. Set via SetEvictionHook before Track is called.
	onEvictMu sync.RWMutex
	onEvict   func(target, sentID string)
}

// SetEvictionHook installs a callback invoked for every TTL-expired entry,
// regardless of clean status. The hook receives (target, sentID) and is
// expected to remove any satellite records keyed by the same pair (e.g. the
// per-delivery snapshot). The hook runs synchronously inside the eviction
// goroutine — keep it cheap; expensive work should be dispatched async.
//
// Safe to call before or after Track. Passing nil clears the hook.
func (mt *MessageTracker) SetEvictionHook(fn func(target, sentID string)) {
	mt.onEvictMu.Lock()
	mt.onEvict = fn
	mt.onEvictMu.Unlock()
}

// NewMessageTracker creates a new MessageTracker with TTL cache and eviction-based clean deletion.
func NewMessageTracker(cacheDir string, senders map[string]Sender) *MessageTracker {
	mt := &MessageTracker{
		senders:  senders,
		cacheDir: cacheDir,
		replies:  make(map[string]map[string]string),
	}

	cache := ttlcache.New[string, *TrackedMessage](
		ttlcache.WithDisableTouchOnHit[string, *TrackedMessage](),
	)

	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, *TrackedMessage]) {
		msg := item.Value()

		// Drop the reverse-index pointer if it still references THIS
		// editKey. A later Track for the same (replyKey, target) with
		// a fresh editKey will have overwritten the pointer — leave
		// that newer entry alone.
		if msg.ReplyKey != "" {
			evictedKey := item.Key()
			mt.repliesMu.Lock()
			if set := mt.replies[msg.ReplyKey]; set != nil {
				if set[msg.Target] == evictedKey {
					delete(set, msg.Target)
					if len(set) == 0 {
						delete(mt.replies, msg.ReplyKey)
					}
				}
			}
			mt.repliesMu.Unlock()
		}

		if reason != ttlcache.EvictionReasonExpired {
			log.Debugf("delivery: tracker evicted %s (reason=%v) — not a TTL expiry, skipping clean", item.Key(), reason)
			return
		}
		metrics.DeliveryTrackerEvictions.Inc()

		// Satellite-store cleanup hook (snapshot store, etc.). Runs for
		// every TTL-expired entry whether the message was clean or not —
		// snapshots have outlived their useful window either way.
		mt.onEvictMu.RLock()
		hook := mt.onEvict
		mt.onEvictMu.RUnlock()
		if hook != nil && msg.SentID != "" {
			hook(msg.Target, msg.SentID)
		}

		if !db.IsClean(msg.Clean) {
			return
		}
		platform := PlatformFromType(msg.Type)
		sender, ok := mt.senders[platform]
		if !ok {
			log.Warnf("delivery: clean delete skipped — no sender for platform %q (type=%s target=%s sentID=%s)", platform, msg.Type, msg.Target, msg.SentID)
			return
		}
		log.Infof("delivery: clean delete %s/%s sentID=%s", msg.Type, msg.Target, msg.SentID)
		go func() {
			if err := sender.Delete(context.Background(), msg.SentID); err != nil {
				log.Warnf("delivery: clean delete failed for %s: %v", msg.SentID, err)
			} else {
				metrics.DeliveryCleanTotal.Inc()
			}
		}()
	})

	go cache.Start()
	mt.cache = cache

	return mt
}

// Track inserts a message into the cache with the given TTL. If
// msg.ReplyKey is non-empty, the (ReplyKey, Target) pair also indexes
// the cache key so LookupReply and LookupReplyTargets can find it.
func (mt *MessageTracker) Track(key string, msg *TrackedMessage, ttl time.Duration) {
	mt.cache.Set(key, msg, ttl)
	if msg.ReplyKey != "" {
		mt.repliesMu.Lock()
		set := mt.replies[msg.ReplyKey]
		if set == nil {
			set = make(map[string]string)
			mt.replies[msg.ReplyKey] = set
		}
		set[msg.Target] = key
		mt.repliesMu.Unlock()
	}
}

// LookupReplyTargets returns the targets with a live reply entry for
// the given replyKey. Used by change-event fanout (e.g. pokemon
// dispatch) to find recipients of the original alert when nobody
// currently matches. Order is unspecified. O(targets) under RLock.
func (mt *MessageTracker) LookupReplyTargets(replyKey string) []string {
	if replyKey == "" {
		return nil
	}
	mt.repliesMu.RLock()
	set := mt.replies[replyKey]
	if len(set) == 0 {
		mt.repliesMu.RUnlock()
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	mt.repliesMu.RUnlock()
	return out
}

// LookupReply returns the SentID of the latest message tracked for
// this (replyKey, target) pair, or "" if none. Two O(1) lookups:
// reverse-index map then cache. Returns "" if the cache entry has
// expired between the two reads (treated as "no prior").
func (mt *MessageTracker) LookupReply(replyKey, target string) string {
	msg := mt.LookupReplyMessage(replyKey, target)
	if msg == nil {
		return ""
	}
	return msg.SentID
}

// LookupReplyMessage returns the full TrackedMessage for the latest
// message under this (replyKey, target) pair, or nil if none. Used
// by change-event dispatch to inherit rule-level fields (Clean,
// Type) from the prior alert when reconstructing a recipient.
// Same race semantics as LookupReply: nil if the cache entry
// expires between the index read and the cache read.
func (mt *MessageTracker) LookupReplyMessage(replyKey, target string) *TrackedMessage {
	if replyKey == "" {
		return nil
	}
	mt.repliesMu.RLock()
	editKey, ok := mt.replies[replyKey][target]
	mt.repliesMu.RUnlock()
	if !ok {
		return nil
	}
	item := mt.cache.Get(editKey)
	if item == nil {
		return nil
	}
	return item.Value()
}

// LookupEdit returns the tracked message for the given edit key, or nil if not found/expired.
func (mt *MessageTracker) LookupEdit(editKey string) *TrackedMessage {
	item := mt.cache.Get(editKey)
	if item == nil {
		return nil
	}
	return item.Value()
}

// UpdateEdit updates the sent ID of an existing tracked message by replacing
// the cache entry atomically (avoids race on concurrent value reads).
func (mt *MessageTracker) UpdateEdit(editKey string, newSentID string) {
	item := mt.cache.Get(editKey)
	if item == nil {
		return
	}
	msg := item.Value()
	updated := *msg
	updated.SentID = newSentID
	mt.cache.Set(editKey, &updated, ttlcache.DefaultTTL)
}

// Size returns the number of tracked messages. Safe to call on a nil
// tracker (returns 0) or a tracker whose cache hasn't been initialised
// yet — the status goroutine can hit Size before init completes during
// a partial dispatcher setup, and crashing on a metrics read is worse
// than reporting zero.
func (mt *MessageTracker) Size() int {
	if mt == nil || mt.cache == nil {
		return 0
	}
	return mt.cache.Len()
}

type persistedEntry struct {
	Key       string         `json:"key"`
	Message   TrackedMessage `json:"message"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// Save serializes all cache items to disk for persistence across restarts.
func (mt *MessageTracker) Save() error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	var entries []persistedEntry
	mt.cache.Range(func(item *ttlcache.Item[string, *TrackedMessage]) bool {
		entries = append(entries, persistedEntry{
			Key:       item.Key(),
			Message:   *item.Value(),
			ExpiresAt: item.ExpiresAt(),
		})
		return true
	})

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(mt.cacheDir, 0o755); err != nil {
		return err
	}

	log.Infof("delivery: tracker saved %d entries to disk", len(entries))
	return os.WriteFile(filepath.Join(mt.cacheDir, "delivery-tracker.json"), data, 0o644)
}

// Load restores tracked messages from disk. Expired clean messages trigger immediate deletion.
func (mt *MessageTracker) Load() error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(mt.cacheDir, "delivery-tracker.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var entries []persistedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	now := time.Now()
	expiredClean := 0
	active := 0
	for _, entry := range entries {
		if entry.ExpiresAt.Before(now) {
			// Expired entry
			if db.IsClean(entry.Message.Clean) {
				expiredClean++
				msg := entry.Message
				platform := PlatformFromType(msg.Type)
				sender, ok := mt.senders[platform]
				if !ok {
					log.Warnf("delivery: clean delete on load skipped — no sender for platform %q (type=%s target=%s sentID=%s)", platform, msg.Type, msg.Target, msg.SentID)
					continue
				}
				go func() {
					if err := sender.Delete(context.Background(), msg.SentID); err != nil {
						log.Warnf("delivery: clean delete on load failed for %s: %v", msg.SentID, err)
					} else {
						metrics.DeliveryCleanTotal.Inc()
					}
				}()
			}
			// Discard expired non-clean entries
			continue
		}

		active++
		remaining := entry.ExpiresAt.Sub(now)
		msg := &TrackedMessage{
			SentID:   entry.Message.SentID,
			Target:   entry.Message.Target,
			Type:     entry.Message.Type,
			MsgType:  entry.Message.MsgType,
			Clean:    entry.Message.Clean,
			ReplyKey: entry.Message.ReplyKey,
		}
		mt.cache.Set(entry.Key, msg, remaining)
		if msg.ReplyKey != "" {
			// Cache OnEviction is already live, so the reverse-index
			// write must be guarded.
			mt.repliesMu.Lock()
			set := mt.replies[msg.ReplyKey]
			if set == nil {
				set = make(map[string]string)
				mt.replies[msg.ReplyKey] = set
			}
			set[msg.Target] = entry.Key
			mt.repliesMu.Unlock()
		}
	}

	log.Infof("delivery: tracker loaded %d entries (%d expired clean, %d active)", len(entries), expiredClean, active)
	return nil
}

// Stop stops the background eviction processor and persists state to disk.
func (mt *MessageTracker) Stop() {
	mt.cache.Stop()
	if err := mt.Save(); err != nil {
		log.Warnf("delivery: failed to save tracker on stop: %v", err)
	}
}
