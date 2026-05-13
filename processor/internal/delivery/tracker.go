package delivery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	Clean    int    `json:"clean"`
	ReplyKey string `json:"reply_key,omitempty"`
}

// MessageTracker manages sent messages with TTL-based expiry and clean deletion.
type MessageTracker struct {
	cache      *ttlcache.Cache[string, *TrackedMessage]
	replyIndex *ttlcache.Cache[string, string] // (replyKey + "\x00" + target) → latest SentID
	senders    map[string]Sender               // "discord" → sender, "telegram" → sender
	cacheDir   string
	mu         sync.Mutex

	// replyTargets is the replyKey → set-of-targets reverse index of
	// replyIndex, maintained on Track and replyIndex OnEviction.
	// Lets LookupReplyTargets enumerate recipients in O(targets) so
	// change-event fanout doesn't scan the full replyIndex.
	replyTargetsMu sync.RWMutex
	replyTargets   map[string]map[string]struct{}
}

// replyIndexKeySep separates replyKey and target in replyIndex keys.
// NUL avoids collisions with any printable substring (e.g. "abc:def").
const replyIndexKeySep = "\x00"

func replyIndexKey(replyKey, target string) string {
	return replyKey + replyIndexKeySep + target
}

// NewMessageTracker creates a new MessageTracker with TTL cache and eviction-based clean deletion.
func NewMessageTracker(cacheDir string, senders map[string]Sender) *MessageTracker {
	mt := &MessageTracker{
		senders:      senders,
		cacheDir:     cacheDir,
		replyTargets: make(map[string]map[string]struct{}),
	}

	cache := ttlcache.New[string, *TrackedMessage](
		ttlcache.WithDisableTouchOnHit[string, *TrackedMessage](),
	)

	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, *TrackedMessage]) {
		if reason != ttlcache.EvictionReasonExpired {
			log.Debugf("delivery: tracker evicted %s (reason=%v) — not a TTL expiry, skipping clean", item.Key(), reason)
			return
		}
		metrics.DeliveryTrackerEvictions.Inc()
		msg := item.Value()
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

	replyIndex := ttlcache.New[string, string](
		ttlcache.WithDisableTouchOnHit[string, string](),
	)
	// Maintain the reverse index alongside the replyIndex cache: when
	// a (replyKey, target) entry expires, drop it from replyTargets too.
	// Manual deletes (cache.Delete) and capacity evictions also flow
	// through here — we don't differentiate, the reverse index should
	// reflect the cache's live set regardless of why an entry left.
	replyIndex.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[string, string]) {
		key := item.Key()
		sep := strings.Index(key, replyIndexKeySep)
		if sep < 0 {
			return // malformed key; should never happen given Track is the only writer
		}
		replyKey := key[:sep]
		target := key[sep+len(replyIndexKeySep):]
		mt.replyTargetsMu.Lock()
		if set := mt.replyTargets[replyKey]; set != nil {
			delete(set, target)
			if len(set) == 0 {
				delete(mt.replyTargets, replyKey)
			}
		}
		mt.replyTargetsMu.Unlock()
	})
	go replyIndex.Start()
	mt.replyIndex = replyIndex

	return mt
}

// Track inserts a message into the cache with the given TTL.
// If msg.ReplyKey is non-empty, the (ReplyKey, Target) pair also indexes
// msg.SentID in the reply index for O(1) reply lookup, and the reverse
// index `replyTargets` is updated so LookupReplyTargets can enumerate
// all recipients for an encounter ID in O(targets).
func (mt *MessageTracker) Track(key string, msg *TrackedMessage, ttl time.Duration) {
	mt.cache.Set(key, msg, ttl)
	if msg.ReplyKey != "" {
		mt.replyIndex.Set(replyIndexKey(msg.ReplyKey, msg.Target), msg.SentID, ttl)
		mt.replyTargetsMu.Lock()
		set := mt.replyTargets[msg.ReplyKey]
		if set == nil {
			set = make(map[string]struct{})
			mt.replyTargets[msg.ReplyKey] = set
		}
		set[msg.Target] = struct{}{}
		mt.replyTargetsMu.Unlock()
	}
}

// LookupReplyTargets returns the list of targets that have a live
// reply-index entry for the given replyKey. Used by the pokemon
// change-event dispatcher to find recipients who should be notified
// even when they don't match the new state (the "your tracked
// pokemon has changed" case). Order is unspecified.
//
// O(targets) under RLock — safe to call on every change event.
func (mt *MessageTracker) LookupReplyTargets(replyKey string) []string {
	if replyKey == "" {
		return nil
	}
	mt.replyTargetsMu.RLock()
	defer mt.replyTargetsMu.RUnlock()
	set := mt.replyTargets[replyKey]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}

// LookupReply returns the SentID of the latest message tracked for
// this (replyKey, target) pair, or "" if none. O(1).
func (mt *MessageTracker) LookupReply(replyKey, target string) string {
	if replyKey == "" {
		return ""
	}
	item := mt.replyIndex.Get(replyIndexKey(replyKey, target))
	if item == nil {
		return ""
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

// Size returns the number of tracked messages.
func (mt *MessageTracker) Size() int {
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
			Clean:    entry.Message.Clean,
			ReplyKey: entry.Message.ReplyKey,
		}
		mt.cache.Set(entry.Key, msg, remaining)
		if msg.ReplyKey != "" {
			mt.replyIndex.Set(replyIndexKey(msg.ReplyKey, msg.Target), msg.SentID, remaining)
			// replyIndex.OnEviction is already live (replyIndex.Start
			// ran in the constructor), so the reverse-index write
			// must be guarded against a concurrent eviction.
			mt.replyTargetsMu.Lock()
			set := mt.replyTargets[msg.ReplyKey]
			if set == nil {
				set = make(map[string]struct{})
				mt.replyTargets[msg.ReplyKey] = set
			}
			set[msg.Target] = struct{}{}
			mt.replyTargetsMu.Unlock()
		}
	}

	log.Infof("delivery: tracker loaded %d entries (%d expired clean, %d active)", len(entries), expiredClean, active)
	return nil
}

// Stop stops the background eviction processor and persists state to disk.
func (mt *MessageTracker) Stop() {
	mt.cache.Stop()
	mt.replyIndex.Stop()
	if err := mt.Save(); err != nil {
		log.Warnf("delivery: failed to save tracker on stop: %v", err)
	}
}
