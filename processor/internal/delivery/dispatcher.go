package delivery

import (
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// DispatcherConfig holds all configuration for the delivery dispatcher.
type DispatcherConfig struct {
	DiscordToken  string
	TelegramToken string
	UploadImages  bool
	DeleteDelayMs int
	QueueSize     int
	CacheDir      string
	Queue         QueueConfig
	// TileProviderURL / TileInternalURL let the Discord sender rewrite a
	// remote tile URL (embed.image.url, which is the public tileserver URL
	// for Discord clients to resolve) to the internal URL before the
	// processor itself downloads the bytes for multipart re-upload. This
	// avoids the CDN/proxy fronting the public URL serving stale bytes to
	// our re-upload path. Empty TileInternalURL falls back to TileProviderURL.
	TileProviderURL string
	TileInternalURL string
}

// DispatchBypass enqueues a job that must skip the rate-limit count and
// will not be dropped if the destination is over its limit. Used for the
// rate-limit notification and ban farewell.
func (d *Dispatcher) DispatchBypass(job *Job) {
	job.BypassRateLimit = true
	d.ch <- job
}

// Dispatcher is the top-level entry point for message delivery.
// It owns the job channel, fair queue, and message tracker.
type Dispatcher struct {
	ch      chan *Job
	queue   *FairQueue
	tracker *MessageTracker

	// Pause state — protected by pauseMu.
	pauseMu     sync.Mutex
	paused      bool
	pauseReason string
	pausedSince time.Time

	// pausedAtomic mirrors paused for cheap lock-free reads (e.g. the
	// per-reply maintenance-suffix check and the per-job drop check in
	// FairQueue.processJob). Always updated inside pauseMu alongside paused.
	pausedAtomic atomic.Bool

	// snapshotStore is the opt-in per-delivery snapshot store (#108). Nil
	// when [snapshots] enabled = false. Wired in by SetSnapshotStore after
	// dispatcher construction so the delivery package doesn't take a hard
	// dependency on snapshots being initialised. Safe to read concurrently
	// from queue workers via SnapshotStore().
	snapshotStore atomic.Pointer[snapshots.Store]
}

// SetSnapshotStore wires the snapshot store into the dispatcher. Called by
// ProcessorService startup after the store has been opened (if enabled).
// Safe to call with nil to clear; subsequent snapshot writes will no-op.
func (d *Dispatcher) SetSnapshotStore(store snapshots.Store) {
	if store == nil {
		d.snapshotStore.Store(nil)
		return
	}
	d.snapshotStore.Store(&store)
}

// SnapshotStore returns the currently-wired snapshot store, or nil. Queue
// workers call this and skip the write if nil.
func (d *Dispatcher) SnapshotStore() snapshots.Store {
	p := d.snapshotStore.Load()
	if p == nil {
		return nil
	}
	return *p
}

// NewDispatcher creates a Dispatcher with the configured senders, tracker, and queue.
func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error) {
	senders := make(map[string]Sender)
	if cfg.DiscordToken != "" {
		ds := NewDiscordSender(cfg.DiscordToken, cfg.UploadImages, cfg.DeleteDelayMs)
		ds.SetTileURLRewrite(cfg.TileProviderURL, cfg.TileInternalURL)
		senders["discord"] = ds
	}
	if cfg.TelegramToken != "" {
		senders["telegram"] = NewTelegramSender(cfg.TelegramToken)
	}

	tracker := NewMessageTracker(cfg.CacheDir, senders)
	if err := tracker.Load(); err != nil {
		log.Warnf("delivery: failed to load tracker cache: %v", err)
	}

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = 1000
	}
	ch := make(chan *Job, queueSize)

	d := &Dispatcher{ch: ch, tracker: tracker}
	d.queue = NewFairQueue(ch, senders, tracker, cfg.Queue, d)

	return d, nil
}

// NewDispatcherWithSenders creates a Dispatcher with externally-provided senders (for testing).
func NewDispatcherWithSenders(senders map[string]Sender, tracker *MessageTracker, queueSize int, queueCfg QueueConfig) *Dispatcher {
	if queueSize <= 0 {
		queueSize = 1000
	}
	ch := make(chan *Job, queueSize)
	d := &Dispatcher{ch: ch, tracker: tracker}
	d.queue = NewFairQueue(ch, senders, tracker, queueCfg, d)
	return d
}

// Start launches the fair queue workers.
func (d *Dispatcher) Start() {
	d.queue.Start()
}

// Dispatch enqueues a job for delivery.
func (d *Dispatcher) Dispatch(job *Job) {
	d.ch <- job
}

// Stop closes the job channel, drains remaining jobs, and persists tracker state.
func (d *Dispatcher) Stop() {
	log.Info("delivery: stopping dispatcher...")
	d.queue.Stop()
	d.tracker.Stop()
	log.Info("delivery: dispatcher stopped")
}

// QueueDepth returns the number of jobs waiting in the channel.
func (d *Dispatcher) QueueDepth() int { return len(d.ch) }

// TrackerSize returns the number of messages being tracked.
func (d *Dispatcher) TrackerSize() int { return d.tracker.Size() }

// MessageTracker exposes the underlying tracker so callers can perform
// reply-key lookups (used by pokemon-changed dispatch to partition matched
// users into "has prior message" / "doesn't" buckets).
func (d *Dispatcher) MessageTracker() *MessageTracker { return d.tracker }

// DiscordDepth returns the number of discord jobs currently in-flight.
func (d *Dispatcher) DiscordDepth() int { return d.queue.DiscordDepth() }

// WebhookDepth returns the number of webhook jobs currently in-flight.
func (d *Dispatcher) WebhookDepth() int { return d.queue.WebhookDepth() }

// TelegramDepth returns the number of telegram jobs currently in-flight.
func (d *Dispatcher) TelegramDepth() int { return d.queue.TelegramDepth() }

// RateLimitWaiting returns the number of delivery goroutines currently blocked
// waiting for Discord rate limits to clear.
func (d *Dispatcher) RateLimitWaiting() int64 {
	if ds, ok := d.queue.senders["discord"].(*DiscordSender); ok {
		return ds.rateLimiter.WaitingCount()
	}
	return 0
}

// DiscordRateSnapshot returns a point-in-time snapshot of Discord rate-limit
// state. Returns a zero-value snapshot when Discord is not configured.
func (d *Dispatcher) DiscordRateSnapshot() DiscordRateSnapshot {
	if ds, ok := d.queue.senders["discord"].(*DiscordSender); ok {
		return ds.rateLimiter.Snapshot()
	}
	return DiscordRateSnapshot{}
}

// TelegramRateSnapshot returns a point-in-time snapshot of Telegram rate-limit
// state. Returns a zero-value snapshot when Telegram is not configured.
func (d *Dispatcher) TelegramRateSnapshot() TelegramRateSnapshot {
	if ts, ok := d.queue.senders["telegram"].(*TelegramSender); ok {
		return ts.Snapshot()
	}
	return TelegramRateSnapshot{}
}

// Pause suspends outbound message delivery. Normal (non-bypass) jobs are
// DROPPED on the floor for the duration of the pause — they are not buffered.
// This avoids OOM during long pauses and the stale-alert flood that would
// follow a Resume. Bypass jobs (rate-limit notifications, ban farewells)
// still send.
//
// Calling Pause while already paused is a no-op — the original reason and
// timestamp are preserved.
func (d *Dispatcher) Pause(reason string) {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	if !d.paused {
		d.paused = true
		d.pauseReason = reason
		d.pausedSince = time.Now()
		d.pausedAtomic.Store(true)
		log.Infof("delivery: paused (reason: %s)", reason)
	}
}

// Resume lifts a previous Pause. Subsequent jobs are delivered normally;
// jobs dropped while paused are gone. Safe to call when not paused.
func (d *Dispatcher) Resume() {
	d.pauseMu.Lock()
	wasPaused := d.paused
	d.paused = false
	d.pauseReason = ""
	d.pausedSince = time.Time{}
	d.pausedAtomic.Store(false)
	d.pauseMu.Unlock()
	if wasPaused {
		log.Info("delivery: resumed")
	}
}

// IsPaused returns whether delivery is currently paused. Lock-free fast path —
// suitable for the per-reply maintenance-suffix check and the per-job drop
// check in FairQueue.processJob.
func (d *Dispatcher) IsPaused() bool {
	return d.pausedAtomic.Load()
}

// PauseState returns the current pause state: whether delivery is paused, the
// reason given when Pause was called, and the time at which Pause was called.
// All three zero-values when not paused.
func (d *Dispatcher) PauseState() (paused bool, reason string, since time.Time) {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	return d.paused, d.pauseReason, d.pausedSince
}
