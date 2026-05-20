package snapshots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/akrylysov/pogreb"
	log "github.com/sirupsen/logrus"
)

// pogrebStore is the on-disk Store implementation backed by pogreb.
//
// pogreb is goroutine-safe for concurrent Put/Get/Delete so we don't add a
// layer of locking here — every method is a thin wrapper around the
// underlying db call plus JSON marshal/unmarshal. The only mutable state
// owned by pogrebStore is the closed flag, accessed via sync/atomic.
type pogrebStore struct {
	db     *pogreb.DB
	closed atomic.Bool
}

// Open opens (or creates) a pogreb-backed snapshot store at path. The
// directory is created if missing. Callers should invoke Close on shutdown.
//
// path should be a directory dedicated to snapshot storage — pogreb writes
// several files into the directory and does its own compaction. Sharing the
// directory with other data is not supported.
func Open(path string) (Store, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("snapshots: create dir %s: %w", path, err)
	}
	db, err := pogreb.Open(path, nil)
	if err != nil {
		return nil, fmt.Errorf("snapshots: open pogreb at %s: %w", path, err)
	}
	return &pogrebStore{db: db}, nil
}

func (s *pogrebStore) Write(_ context.Context, snap *Snapshot) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if snap == nil {
		return errors.New("snapshots: nil snapshot")
	}
	if snap.MessageID == "" || snap.Target == "" {
		return errors.New("snapshots: snapshot missing MessageID or Target")
	}
	snap.Version = SchemaVersion
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("snapshots: marshal: %w", err)
	}
	if err := s.db.Put([]byte(snap.Key()), data); err != nil {
		return fmt.Errorf("snapshots: put %s: %w", snap.Key(), err)
	}
	return nil
}

func (s *pogrebStore) Read(_ context.Context, key string) (*Snapshot, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}
	data, err := s.db.Get([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("snapshots: get %s: %w", key, err)
	}
	if data == nil {
		return nil, ErrNotFound
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// Corruption surfaces as a miss — the consumer responds with "alert
		// has expired" and the entry will be cleaned up by the next sweep.
		log.Warnf("snapshots: corrupt record for key %s: %v", key, err)
		return nil, ErrNotFound
	}
	if snap.Version > SchemaVersion {
		// Newer-than-supported records are treated as a miss. The on-disk
		// record is left intact so a newer binary can still read it.
		return nil, ErrNotFound
	}
	return &snap, nil
}

func (s *pogrebStore) Delete(_ context.Context, key string) error {
	if s.closed.Load() {
		return ErrClosed
	}
	// pogreb's Delete is a no-op on missing keys, which matches our contract.
	if err := s.db.Delete([]byte(key)); err != nil {
		return fmt.Errorf("snapshots: delete %s: %w", key, err)
	}
	return nil
}

// Sweep walks every key in the store and deletes records whose ExpiresAt is
// far enough in the past that they're considered orphaned (the normal
// per-message delete callback didn't fire — crash, restart, or a destination
// that's gone away). Records that can't be unmarshalled are also deleted —
// they're either pre-version corruption or partial writes.
//
// The grace period (how long after ExpiresAt to keep a record before sweep
// removes it) is the caller's responsibility: pass `now - grace` as the
// `now` argument and Sweep will treat that as the threshold.
func (s *pogrebStore) Sweep(_ context.Context, now int64) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}
	deleted := 0
	it := s.db.Items()
	for {
		k, v, err := it.Next()
		if errors.Is(err, pogreb.ErrIterationDone) {
			break
		}
		if err != nil {
			return deleted, fmt.Errorf("snapshots: sweep iterate: %w", err)
		}
		// Decode only the expiry field for efficiency; full unmarshal is
		// wasted work for the sweep path.
		var hdr struct {
			ExpiresAt int64 `json:"expiresAt"`
		}
		if err := json.Unmarshal(v, &hdr); err != nil {
			// Corrupt entry — delete it.
			if delErr := s.db.Delete(k); delErr == nil {
				deleted++
			}
			continue
		}
		if hdr.ExpiresAt > 0 && hdr.ExpiresAt >= now {
			// Still in its TTL window.
			continue
		}
		if err := s.db.Delete(k); err != nil {
			log.Warnf("snapshots: sweep delete %s: %v", string(k), err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

func (s *pogrebStore) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil // already closed
	}
	return s.db.Close()
}

// Count returns the approximate number of records in the store. Used by
// metrics and tests; not part of the Store interface to keep that surface
// minimal.
func (s *pogrebStore) Count() uint32 {
	if s.closed.Load() {
		return 0
	}
	return s.db.Count()
}
