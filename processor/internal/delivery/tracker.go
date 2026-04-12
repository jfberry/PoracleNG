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
	SentID string `json:"sent_id"`
	Target string `json:"target"`
	Type   string `json:"type"` // "discord:user", "telegram:group", etc.
	Clean  int    `json:"clean"`
}

// MessageTracker manages sent messages with TTL-based expiry and clean deletion.
type MessageTracker struct {
	cache    *ttlcache.Cache[string, *TrackedMessage]
	senders  map[string]Sender // "discord" → sender, "telegram" → sender
	cacheDir string
	mu       sync.Mutex
}

// NewMessageTracker creates a new MessageTracker with TTL cache and eviction-based clean deletion.
func NewMessageTracker(cacheDir string, senders map[string]Sender) *MessageTracker {
	mt := &MessageTracker{
		senders:  senders,
		cacheDir: cacheDir,
	}

	cache := ttlcache.New[string, *TrackedMessage](
		ttlcache.WithDisableTouchOnHit[string, *TrackedMessage](),
	)

	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, *TrackedMessage]) {
		if reason != ttlcache.EvictionReasonExpired {
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

// Track inserts a message into the cache with the given TTL.
func (mt *MessageTracker) Track(key string, msg *TrackedMessage, ttl time.Duration) {
	mt.cache.Set(key, msg, ttl)
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
		mt.cache.Set(entry.Key, &TrackedMessage{
			SentID: entry.Message.SentID,
			Target: entry.Message.Target,
			Type:   entry.Message.Type,
			Clean:  entry.Message.Clean,
		}, remaining)
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
