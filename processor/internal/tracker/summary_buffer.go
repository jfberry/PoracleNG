package tracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
)

// BufferedQuest is a single matched-but-not-delivered quest awaiting
// a summary delivery window. The raw webhook bytes are kept verbatim so
// the renderer can re-enrich the entry at delivery time without depending
// on internal types whose shape may evolve.
//
// Form is only meaningful for pokemon-encounter rewards (type 7) where
// different forms of the same species (Spinda 01 vs 08, Unown, costumes)
// must NOT collapse into one group — they have distinct icons and
// distinct rewardName labels. For item/stardust/candy/mega-energy
// rewards Form is always 0.
type BufferedQuest struct {
	RewardType int    `json:"reward_type"`
	Reward     int    `json:"reward"`
	Form       int    `json:"form,omitempty"`
	PokestopID string `json:"pokestop_id"`
	WithAR     bool   `json:"with_ar"`
	Payload    []byte `json:"payload"`
	ExpiresAt  int64  `json:"expires_at"`
	CreatedAt  int64  `json:"created_at"`
}

// bufferKey deduplicates entries within a (humanID, alertType) bucket.
// Re-appending with the same key replaces the entry so quest-rotation
// updates clobber stale data while keeping the bucket compact.
type bufferKey struct {
	RewardType int
	Reward     int
	Form       int
	PokestopID string
	WithAR     bool
}

// SummaryBuffer holds matched-but-not-delivered quests in memory, keyed
// by (humanID, alertType). The contents are flushed by the summary
// scheduler at user-defined active hours.
//
// Persistence is best-effort: Save snapshots to disk on graceful
// shutdown and Load restores at startup. A crashed process loses any
// quests that arrived since the last Save — acceptable given quests
// are short-lived and re-emitted by the scanner.
type SummaryBuffer struct {
	mu   sync.Mutex
	data map[string]map[string]map[bufferKey]BufferedQuest
	path string // empty disables persistence
}

// NewSummaryBuffer creates an empty buffer. If snapshotPath is empty,
// Save and Load are no-ops.
func NewSummaryBuffer(snapshotPath string) *SummaryBuffer {
	return &SummaryBuffer{
		data: make(map[string]map[string]map[bufferKey]BufferedQuest),
		path: snapshotPath,
	}
}

// Append upserts q into the (humanID, alertType) bucket using
// bufferKey as the dedup primary key. The latest write wins, so
// rotating-reward updates replace the prior payload / ExpiresAt /
// CreatedAt.
func (sb *SummaryBuffer) Append(humanID, alertType string, q BufferedQuest) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	byType := sb.data[humanID]
	if byType == nil {
		byType = make(map[string]map[bufferKey]BufferedQuest)
		sb.data[humanID] = byType
	}
	bucket := byType[alertType]
	if bucket == nil {
		bucket = make(map[bufferKey]BufferedQuest)
		byType[alertType] = bucket
	}
	bucket[bufferKey{
		RewardType: q.RewardType,
		Reward:     q.Reward,
		Form:       q.Form,
		PokestopID: q.PokestopID,
		WithAR:     q.WithAR,
	}] = q
}

// List returns a snapshot copy of every entry in the (humanID,
// alertType) bucket. Order is unspecified.
func (sb *SummaryBuffer) List(humanID, alertType string) []BufferedQuest {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	bucket := sb.data[humanID][alertType]
	if len(bucket) == 0 {
		return nil
	}
	out := make([]BufferedQuest, 0, len(bucket))
	for _, q := range bucket {
		out = append(out, q)
	}
	return out
}

// Clear removes the entire (humanID, alertType) bucket. Other buckets
// for the same humanID are untouched.
func (sb *SummaryBuffer) Clear(humanID, alertType string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	byType := sb.data[humanID]
	if byType == nil {
		return
	}
	delete(byType, alertType)
	if len(byType) == 0 {
		delete(sb.data, humanID)
	}
}

// SweepExpired drops every entry that has aged out. Two independent
// conditions trigger eviction:
//
//   - q.ExpiresAt < asOf — the normal per-entry expiry the caller sets
//     at append time (e.g. quests expire at end-of-day local to the
//     pokestop).
//   - maxAgeSecs > 0 && q.CreatedAt > 0 && asOf - q.CreatedAt > maxAgeSecs
//     — a safety-net upper bound on how long any entry can live, used
//     to evict malformed payloads whose ExpiresAt is zero, far-future,
//     or otherwise unreliable. Pass 0 to disable this leg.
//
// Empty buckets/users are removed. Returns the number of entries removed.
func (sb *SummaryBuffer) SweepExpired(asOf int64, maxAgeSecs int64) int {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	removed := 0
	for humanID, byType := range sb.data {
		for alertType, bucket := range byType {
			for key, q := range bucket {
				expired := q.ExpiresAt < asOf
				stale := maxAgeSecs > 0 && q.CreatedAt > 0 && asOf-q.CreatedAt > maxAgeSecs
				if expired || stale {
					delete(bucket, key)
					removed++
				}
			}
			if len(bucket) == 0 {
				delete(byType, alertType)
			}
		}
		if len(byType) == 0 {
			delete(sb.data, humanID)
		}
	}
	return removed
}

// snapshotEntry is the serialised shape of a single bucket entry.
// We embed the dedup key explicitly so the format is self-describing
// (no need to recompute it from the entry on load) and so future
// fields on BufferedQuest don't accidentally change the dedup behaviour.
type snapshotEntry struct {
	Key   bufferKey     `json:"key"`
	Quest BufferedQuest `json:"quest"`
}

// snapshotBucket pairs an (alertType) with its entries.
type snapshotBucket struct {
	AlertType string          `json:"alert_type"`
	Entries   []snapshotEntry `json:"entries"`
}

// snapshotUser pairs a humanID with its alert-type buckets.
type snapshotUser struct {
	HumanID string           `json:"human_id"`
	Buckets []snapshotBucket `json:"buckets"`
}

// snapshotFile is the top-level on-disk shape.
type snapshotFile struct {
	Version int            `json:"version"`
	Users   []snapshotUser `json:"users"`
}

const snapshotVersion = 1

// Save writes the buffer to the configured snapshot path as JSON. The
// containing directory is created if missing. No-op when the path is "".
func (sb *SummaryBuffer) Save() error {
	if sb.path == "" {
		return nil
	}

	sb.mu.Lock()
	out := snapshotFile{Version: snapshotVersion}
	for humanID, byType := range sb.data {
		user := snapshotUser{HumanID: humanID}
		for alertType, bucket := range byType {
			b := snapshotBucket{AlertType: alertType}
			for k, q := range bucket {
				b.Entries = append(b.Entries, snapshotEntry{Key: k, Quest: q})
			}
			user.Buckets = append(user.Buckets, b)
		}
		out.Users = append(out.Users, user)
	}
	sb.mu.Unlock()

	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("summary buffer marshal: %w", err)
	}
	if dir := filepath.Dir(sb.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("summary buffer mkdir: %w", err)
		}
	}
	return os.WriteFile(sb.path, data, 0o644)
}

// Load restores the buffer from disk. Missing file is silent (no error,
// empty buffer). A malformed file is logged and silently treated as
// empty so a corrupt snapshot can never block startup.
func (sb *SummaryBuffer) Load() error {
	if sb.path == "" {
		return nil
	}

	data, err := os.ReadFile(sb.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("summary buffer read: %w", err)
	}

	var loaded snapshotFile
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Warnf("summary buffer: failed to parse %s: %v (starting empty)", sb.path, err)
		return nil
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	restored := 0
	for _, user := range loaded.Users {
		if user.HumanID == "" {
			continue
		}
		byType := sb.data[user.HumanID]
		if byType == nil {
			byType = make(map[string]map[bufferKey]BufferedQuest)
			sb.data[user.HumanID] = byType
		}
		for _, b := range user.Buckets {
			if b.AlertType == "" {
				continue
			}
			bucket := byType[b.AlertType]
			if bucket == nil {
				bucket = make(map[bufferKey]BufferedQuest)
				byType[b.AlertType] = bucket
			}
			for _, e := range b.Entries {
				bucket[e.Key] = e.Quest
				restored++
			}
		}
	}

	log.Debugf("summary buffer: restored %d entries from %s", restored, sb.path)
	return nil
}
