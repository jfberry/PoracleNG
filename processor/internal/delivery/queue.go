package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/logref"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	log "github.com/sirupsen/logrus"
)

// laneIdleTimeout is how long a destination's drainer waits with an empty lane
// before reaping itself. Deliberately a const — not exposed as config.
const laneIdleTimeout = 60 * time.Second

// lane is one destination's bounded queue + (implicitly) its single drainer.
type lane struct {
	ch     chan *Job
	target string
	// pending counts queued + in-flight + about-to-be-enqueued jobs. Guarded by
	// FairQueue.lanesMu. It exists so the reaper never deletes a lane that has
	// work or an in-flight enqueue (the counter-under-lock reap-safety pattern).
	pending int
}

// failRecord tracks consecutive failures and when the block was applied.
type failRecord struct {
	count     atomic.Int32
	blockedAt time.Time // zero until threshold reached
}

// QueueConfig controls per-platform concurrency limits.
type QueueConfig struct {
	ConcurrentDiscord  int
	ConcurrentWebhook  int
	ConcurrentTelegram int
	FailThreshold      int // consecutive failures before disabling (0 = default 10)
	// PerRouteBuffer is the buffered capacity of each destination's lane
	// (from [tuning] delivery_queue_size). <=0 defaults to 200.
	PerRouteBuffer int
	// OnDisabled is invoked when a target hits the failure threshold.
	// Implementation should: disable the user in DB, notify them, post shame.
	OnDisabled func(target, name, jobType string)

	// RateLimiter is the authoritative per-destination message-rate limiter.
	// When set, the queue calls Check before each genuine new send (edits and
	// jobs with BypassRateLimit=true are exempt). When nil, no rate limiting
	// is enforced at delivery time.
	RateLimiter *ratelimit.Limiter
	// RateLimitHooks receives notifications when a destination just breached
	// the limit or has been banned by accumulated breaches. Optional; when
	// nil, only metrics and logs are updated.
	RateLimitHooks RateLimitHooks
}

// FairQueue provides per-destination serialization with platform-level concurrency control.
type FairQueue struct {
	senders    map[string]Sender
	tracker    *MessageTracker
	dispatcher *Dispatcher
	wg         sync.WaitGroup

	// Shutdown context — cancelled when Stop() is called, aborts in-flight sends.
	ctx    context.Context
	cancel context.CancelFunc

	// Per-destination lanes. Each lane has one drainer goroutine; lanes spawn on
	// first job for a target and reap after idleTimeout idle. lanesMu guards
	// the map, each lane's pending, and stopped.
	lanesMu     sync.Mutex
	lanes       map[string]*lane
	stopped     bool
	perRouteBuf int

	// idleTimeout defaults to laneIdleTimeout; it's a per-instance field (not a
	// mutable global) solely so tests can shorten it without racing live
	// drainers on shared state — never wired to config.
	idleTimeout time.Duration

	// Per-destination consecutive failure tracking. After failThreshold
	// consecutive errors, the destination is disabled via onDisabled
	// callback and messages are dropped for failBlockDuration. After
	// that window the in-memory block expires — if the user re-enabled
	// via any path (PoracleWeb, !start, API), delivery resumes.
	failCounts        sync.Map // target string → *failRecord
	failThreshold     int
	failBlockDuration time.Duration
	onDisabled        func(target, name, jobType string)

	rateLimiter    *ratelimit.Limiter
	rateLimitHooks RateLimitHooks

	// backpressure counts send enqueues that blocked on a full lane.
	// lastBackpressureLog throttles the corresponding warn log to once per 5s
	// (unix nanoseconds, read/written via CompareAndSwap).
	backpressure        atomic.Int64
	lastBackpressureLog atomic.Int64
}

// NewFairQueue creates a FairQueue that dispatches jobs through the
// appropriate sender, respecting per-platform concurrency limits. Jobs are
// routed to per-destination lanes via enqueue; each lane spawns its own
// drainer goroutine on first use. d is the owning Dispatcher; it may be nil
// in tests that construct FairQueue directly and don't need pause support.
func NewFairQueue(senders map[string]Sender, tracker *MessageTracker, cfg QueueConfig, d *Dispatcher) *FairQueue {
	ctx, cancel := context.WithCancel(context.Background())
	failThreshold := cfg.FailThreshold
	if failThreshold <= 0 {
		failThreshold = 10
	}
	perRouteBuf := cfg.PerRouteBuffer
	if perRouteBuf <= 0 {
		perRouteBuf = 200
	}
	return &FairQueue{
		senders:           senders,
		tracker:           tracker,
		dispatcher:        d,
		ctx:               ctx,
		cancel:            cancel,
		lanes:             make(map[string]*lane),
		perRouteBuf:       perRouteBuf,
		idleTimeout:       laneIdleTimeout,
		failThreshold:     failThreshold,
		failBlockDuration: 5 * time.Minute,
		onDisabled:        cfg.OnDisabled,
		rateLimiter:       cfg.RateLimiter,
		rateLimitHooks:    cfg.RateLimitHooks,
	}
}

// enqueue routes a job to its destination lane, spawning the lane + drainer on
// first use. block=true (sends) applies backpressure: it blocks until the lane
// has room. block=false (clean-deletes) drops on a full lane and returns false.
// Returns false if the queue is stopping or the job was dropped.
func (fq *FairQueue) enqueue(job *Job, block bool) bool {
	fq.lanesMu.Lock()
	if fq.stopped {
		fq.lanesMu.Unlock()
		return false
	}
	l, ok := fq.lanes[job.Target]
	if !ok {
		l = &lane{ch: make(chan *Job, fq.perRouteBuf), target: job.Target}
		fq.lanes[job.Target] = l
		fq.wg.Add(1)
		go fq.runLane(l)
		metrics.DeliveryLaneSpawned.Inc()
	}
	l.pending++ // reserve BEFORE releasing the lock so the reaper can't drop us
	fq.lanesMu.Unlock()

	// A concurrent Stop() can close l.ch after we passed the stopped check and
	// released the lock; sending on a closed channel panics. Recover so a
	// shutdown race drops the job (returns false) instead of crashing the
	// caller. A channel send is the only panic source below this point, so this
	// masks nothing else.
	defer func() { _ = recover() }()

	if block {
		select {
		case l.ch <- job:
			return true
		default:
			// Lane full — record + throttle-log backpressure, then block.
			fq.recordBackpressure(l.target)
			l.ch <- job
			return true
		}
	}
	select {
	case l.ch <- job:
		return true
	default:
		fq.lanesMu.Lock()
		l.pending--
		fq.lanesMu.Unlock()
		fq.recordCleanDropped(l.target)
		return false
	}
}

// recordBackpressure is called when a send blocks on a full lane. It counts the
// event and logs at most once per 5s per queue (naming the target), so a hot
// lane doesn't flood the log.
func (fq *FairQueue) recordBackpressure(target string) {
	metrics.DeliveryLaneBackpressure.Inc()
	fq.backpressure.Add(1)
	now := time.Now().UnixNano()
	last := fq.lastBackpressureLog.Load()
	if now-last > int64(5*time.Second) && fq.lastBackpressureLog.CompareAndSwap(last, now) {
		log.Warnf("delivery: lane full, applying backpressure to sends for %s", target)
	}
}

// recordCleanDropped is called when a clean-delete is dropped on a full lane.
func (fq *FairQueue) recordCleanDropped(target string) {
	metrics.DeliveryCleanDeleteDropped.Inc()
}

// BackpressureCount returns the cumulative count of send enqueues that blocked
// on a full lane. Used by the [Status] reporter to detect a developing backlog.
func (fq *FairQueue) BackpressureCount() int64 { return fq.backpressure.Load() }

// runLane is one destination's drainer: it processes jobs FIFO and reaps itself
// after idleTimeout with an empty lane and no pending work.
func (fq *FairQueue) runLane(l *lane) {
	defer fq.wg.Done()
	idle := time.NewTimer(fq.idleTimeout)
	defer idle.Stop()
	for {
		select {
		case job, ok := <-l.ch:
			if !ok {
				return // channel closed on shutdown
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			fq.processJob(job)
			fq.lanesMu.Lock()
			l.pending--
			fq.lanesMu.Unlock()
			idle.Reset(fq.idleTimeout)
		case <-idle.C:
			fq.lanesMu.Lock()
			if l.pending == 0 {
				delete(fq.lanes, l.target)
				fq.lanesMu.Unlock()
				metrics.DeliveryLaneReaped.Inc()
				return // reap: no work, no in-flight enqueue
			}
			fq.lanesMu.Unlock()
			idle.Reset(fq.idleTimeout)
		}
	}
}

// Start is retained for API compatibility. Lanes spawn on demand in enqueue, so
// there is no worker pool to launch here.
func (fq *FairQueue) Start() {}

// Stop marks the queue stopped (new enqueues rejected), closes every live lane
// so its drainer finishes buffered jobs and exits, waits for all drainers, then
// cancels the context. Buffered jobs are still delivered before shutdown.
func (fq *FairQueue) Stop() {
	fq.lanesMu.Lock()
	fq.stopped = true
	for _, l := range fq.lanes {
		close(l.ch)
	}
	fq.lanesMu.Unlock()

	log.Info("delivery: waiting for delivery lanes to drain...")
	fq.wg.Wait()
	log.Info("delivery: delivery lanes drained")
	fq.cancel()
}

func (fq *FairQueue) processJob(job *Job) {
	// 1. Wait for rate limits. Per-platform wire concurrency is now enforced
	//    by the sender itself (see DiscordSender/TelegramSender.roundTrip),
	//    acquired per HTTP call rather than held across this whole job.
	platform := PlatformFromType(job.Type)
	if sender, ok := fq.senders[platform]; ok {
		sender.WaitForRateLimit(job.Target)
	}

	sender, ok := fq.senders[platform]
	if !ok {
		logref.Warnf(job.LogReference, "delivery: no sender for platform %q (type=%s target=%s)", platform, job.Type, job.Target)
		return
	}

	// Clean-delete job (routed here from the tracker's TTL eviction). This
	// job's lane drainer serialises it with other deletes and sends to this
	// target (one drainer per target) — the fix for a burst of expiring
	// alerts firing concurrent DELETEs that 429 each other into failure.
	// Sender.Delete carries its own 429 Retry-After backoff and acquires its
	// own wire-call concurrency slot. No alert-limit accounting, tracking,
	// snapshot, or failure-disable: a delete is cleanup, not a send.
	if job.DeleteSentID != "" {
		if err := sender.Delete(fq.ctx, job.DeleteSentID); err != nil {
			logref.Warnf(job.LogReference, "delivery: clean delete failed for %s: %v", job.DeleteSentID, err)
		} else {
			metrics.DeliveryCleanTotal.Inc()
		}
		return
	}

	start := time.Now()

	// If job has EditKey, try editing existing message first.
	// Edits are mutations of an already-counted send — they do NOT consume
	// rate-limit budget. Only fall through to the new-send path (which does
	// count) if no tracked message exists or the edit attempt fails.
	//
	// EDIT TAKES PRIORITY OVER REPLY: when both EditKey and ReplyKey are set
	// and the tracker has a prior under EditKey, the existing message is
	// updated in place — we do not fall through to the reply-stamping path
	// below, because the message reuses the prior rather than replying to
	// it.
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil {
			logref.Infof(job.LogReference, "edit: found tracked message for key=%s, attempting edit", job.EditKey)
			if err := sender.Edit(fq.ctx, existing.SentID, job.Message, job.StaticMapData); err == nil {
				logref.Infof(job.LogReference, "edit: succeeded for key=%s", job.EditKey)
				metrics.DeliveryTotal.WithLabelValues(platform, "edit_ok").Inc()
				metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())

				// Edits overwrite the snapshot (#108 — edits write a new
				// snapshot under the same key, so consumers always see the
				// most-recently-rendered state). Same composite-ID
				// extraction as the initial-send write above.
				if fq.dispatcher != nil && job.SnapshotData != nil {
					if store := fq.dispatcher.SnapshotStore(); store != nil {
						job.SnapshotData.MessageID = extractMessageIDForSnapshot(existing.SentID, PlatformFromType(job.Type))
						job.SnapshotData.CreatedAt = time.Now().Unix()
						if err := store.Write(fq.ctx, job.SnapshotData); err != nil {
							metrics.SnapshotWritesTotal.WithLabelValues("fail").Inc()
							logref.Warnf(job.LogReference, "snapshot write on edit failed: %v", err)
						} else {
							metrics.SnapshotWritesTotal.WithLabelValues("ok").Inc()
						}
					}
				}
				return
			} else {
				logref.Warnf(job.LogReference, "edit: failed for key=%s: %v, sending new message", job.EditKey, err)
			}
		} else {
			logref.Debugf(job.LogReference, "edit: no tracked message for key=%s, will send new and track", job.EditKey)
		}
	}

	// Reply target stamping: when ReplyKey is set and the tracker has a
	// known prior message for (ReplyKey, Target), stamp the prior's SentID
	// onto the job so the platform sender can inject a Discord
	// message_reference / Telegram reply_to_message_id.
	//
	// Only runs after the edit path falls through (either no EditKey, no
	// prior under EditKey, or the edit attempt failed and we're sending a
	// fresh message). Caller is not expected to set ReplyToID — it's an
	// ephemeral queue→sender field.
	if job.ReplyKey != "" && job.ReplyToID == "" {
		if msgID := fq.tracker.LookupReply(job.ReplyKey, job.Target); msgID != "" {
			job.ReplyToID = msgID
		}
	}

	// Pause gate. During maintenance, normal deliveries are DROPPED on the
	// floor rather than buffered — buffering would balloon memory on long
	// pauses and produce a flood of stale alerts to users on resume.
	// Bypass jobs (rate-limit notifications, ban farewells) still send;
	// they're administrative messages, not user alerts, and tend to be rare.
	if !job.BypassRateLimit && fq.dispatcher != nil && fq.dispatcher.IsPaused() {
		logref.Debugf(job.LogReference, "dropped — delivery paused (type=%s target=%s)",
			job.Type, job.Target)
		metrics.DeliveryTotal.WithLabelValues(platform, "dropped_paused").Inc()
		return
	}

	// 1b. Squash sends that ask for clean but whose TTH is already expired.
	// The tracker can't schedule a deletion for a past TTL, so sending would
	// leave the message in the channel forever. This commonly hits alerts
	// about events that expired before enrichment ran (Golbat flush delay,
	// webhook queue backpressure). Edit-only jobs are allowed through — a
	// one-shot message that won't be edited later is still useful, and an
	// edit whose original has already expired in the tracker falls back to
	// a new send which may still want to be visible.
	if db.IsClean(job.Clean) && job.TTH.Duration() <= 0 {
		logref.Warnf(job.LogReference, "clean message suppressed — TTL already expired before send (clean=%d type=%s target=%s)",
			job.Clean, job.Type, job.Target)
		metrics.DeliveryTotal.WithLabelValues(platform, "suppressed_expired").Inc()
		return
	}

	// 2. Send new message — skip if target has been disabled from repeated failures
	if fq.isTargetDisabled(job.Target) {
		metrics.DeliveryTotal.WithLabelValues(platform, "stopped").Inc()
		return
	}

	// 2b. Authoritative rate-limit count. Bypass jobs (rate-limit
	//     notifications, ban farewells) skip the check entirely so the
	//     limiter can never swallow the very message reporting on itself.
	if fq.rateLimiter != nil && !job.BypassRateLimit {
		result := fq.rateLimiter.Check(job.Target, job.Type)
		if !result.Allowed {
			if result.JustBreached {
				metrics.RateLimitBreaches.Inc()
				metrics.RateLimitDropped.Inc()
				logref.Infof(job.LogReference, "rate limit reached for %s %s %s (%d messages in %ds)",
					job.Type, job.Target, job.Name, result.Limit, result.ResetSeconds)
				if fq.rateLimitHooks != nil {
					// Hooks dispatch bypass jobs back into the same lane.
					// Calling them synchronously here would block this job
					// while its lane drainer is busy — and if the lane is
					// full of further jobs to the same target (the very
					// condition that produced the breach) those jobs cannot
					// drain because the drainer is stuck in the hook. Fire
					// and forget instead, so the drainer can move on
					// promptly while the hook completes asynchronously.
					hooks := fq.rateLimitHooks
					target, typ, name, lang := job.Target, job.Type, job.Name, job.Language
					limit, reset, banned, ref := result.Limit, result.ResetSeconds, result.Banned, job.LogReference
					go func() {
						hooks.OnBreach(target, typ, name, lang, limit, reset)
						if banned {
							metrics.RateLimitDisabled.Inc()
							logref.Infof(ref, "rate limit: banning %s %s %s (too many violations)",
								typ, target, name)
							hooks.OnBan(target, typ, name, lang)
						}
					}()
				}
			} else {
				metrics.RateLimitDropped.Inc()
				logref.Debugf(job.LogReference, "rate limited: dropping message for %s %s %s",
					job.Type, job.Target, job.Name)
			}
			return
		}
	}

	destKind := strings.ToUpper(strings.TrimPrefix(job.Type, platform+":"))
	if destKind == "" {
		destKind = strings.ToUpper(job.Type)
	}
	logref.Infof(job.LogReference, "-> %s %s %s Sending %s message", job.Name, job.Target, destKind, platform)

	sent, err := sender.Send(fq.ctx, job)
	if err != nil {
		var permErr *PermanentError
		if errors.As(err, &permErr) {
			logref.Warnf(job.LogReference, "delivery: permanent error for %s/%s: %s", job.Type, job.Target, permErr.Reason)
			metrics.DeliveryTotal.WithLabelValues(platform, "permanent_error").Inc()
			fq.recordFailure(job.Target, job.Name, job.Type)
		} else {
			logref.Errorf(job.LogReference, "delivery: send failed for %s/%s: %v", job.Type, job.Target, err)
			metrics.DeliveryTotal.WithLabelValues(platform, "error").Inc()
			fq.recordFailure(job.Target, job.Name, job.Type)
		}
		metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())
		return
	}

	// Successful send — reset failure counter
	fq.failCounts.Delete(job.Target)

	metrics.DeliveryTotal.WithLabelValues(platform, "ok").Inc()
	metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())

	// Write the per-delivery snapshot (#108) if the store is configured and
	// the render layer attached snapshot data to this job. The MessageID
	// only becomes available now — fill it in from the SentMessage. Failures
	// are non-fatal: snapshot writing is for buttons/inspection, never the
	// alert itself, so we log and continue.
	if fq.dispatcher != nil && job.SnapshotData != nil && sent != nil {
		if store := fq.dispatcher.SnapshotStore(); store != nil {
			// The Discord sender's SentMessage.ID is a composite like
			// "bot/channelID:discordMessageID" (used by delete/edit to
			// remember the channel). For the snapshot lookup we need
			// the raw Discord message ID — that's what shows up in
			// InteractionCreate.Message.ID when a user clicks a button.
			// Telegram SentMessage.IDs use a different shape that
			// passes through cleanly here.
			job.SnapshotData.MessageID = extractMessageIDForSnapshot(sent.ID, PlatformFromType(job.Type))
			if job.SnapshotData.CreatedAt == 0 {
				job.SnapshotData.CreatedAt = time.Now().Unix()
			}
			if err := store.Write(fq.ctx, job.SnapshotData); err != nil {
				metrics.SnapshotWritesTotal.WithLabelValues("fail").Inc()
				logref.Warnf(job.LogReference, "snapshot write failed for %s/%s: %v",
					job.Type, job.Target, err)
			} else {
				metrics.SnapshotWritesTotal.WithLabelValues("ok").Inc()
				logref.Debugf(job.LogReference, "snapshot stored key=%s sentID=%s",
					job.SnapshotData.Key(), sent.ID)
			}
		}
	}

	// 3. Track for clean/edit/reply if needed.
	// ReplyKey on its own (without clean/edit) is enough to want tracking —
	// otherwise reply chains can't form because the tracker has no entry
	// to find on the next change event.
	wantsTracking := db.IsClean(job.Clean) || db.IsEdit(job.Clean) || job.EditKey != "" || job.ReplyKey != ""
	if wantsTracking && sent == nil {
		logref.Warnf(job.LogReference, "clean/edit/reply tracking skipped — sender returned no SentMessage (clean=%d editKey=%q replyKey=%q)", job.Clean, job.EditKey, job.ReplyKey)
		return
	}
	if sent != nil && wantsTracking {
		ttl := job.TTH.Duration()
		if ttl <= 0 {
			logref.Warnf(job.LogReference, "clean/edit/reply tracking skipped — TTL already expired (clean=%d)", job.Clean)
			return
		}

		key := job.EditKey
		if key == "" {
			key = fmt.Sprintf("clean:%s:%s:%s", job.Type, job.Target, sent.ID)
		}

		// Pass ReplyKey through so MessageTracker.Track populates the
		// dedicated reply index for O(1) (replyKey, target) lookups on
		// the next change event.
		fq.tracker.Track(key, &TrackedMessage{
			SentID:   sent.ID,
			Target:   job.Target,
			Type:     job.Type,
			MsgType:  job.MsgType,
			Clean:    job.Clean,
			ReplyKey: job.ReplyKey,
			Template: job.Template,
		}, ttl)
		logref.Debugf(job.LogReference, "tracked message key=%s sentID=%s ttl=%v clean=%d replyKey=%q", key, sent.ID, ttl, job.Clean, job.ReplyKey)
	}
}

// LaneStats walks the lane map once under lanesMu. Returns total buffered jobs,
// active lane count, deepest single-lane depth, that lane's target, and the
// count of lanes at/over 80% of perRouteBuf. Pure read — sets no metrics.
func (fq *FairQueue) LaneStats() (totalQueued, active, maxDepth int, deepestTarget string, nearCap int) {
	fq.lanesMu.Lock()
	defer fq.lanesMu.Unlock()
	active = len(fq.lanes)
	threshold := fq.perRouteBuf * 8 / 10
	for target, l := range fq.lanes {
		d := len(l.ch)
		totalQueued += d
		if d > maxDepth {
			maxDepth = d
			deepestTarget = target
		}
		if threshold > 0 && d >= threshold {
			nearCap++
		}
	}
	return
}

// DiscordDepth returns discord bot (channel/DM/thread) wire calls in flight.
func (fq *FairQueue) DiscordDepth() int {
	if ds, ok := fq.senders["discord"].(*DiscordSender); ok {
		return ds.DiscordInFlight()
	}
	return 0
}

// WebhookDepth returns discord webhook wire calls in flight.
func (fq *FairQueue) WebhookDepth() int {
	if ds, ok := fq.senders["discord"].(*DiscordSender); ok {
		return ds.WebhookInFlight()
	}
	return 0
}

// TelegramDepth returns telegram wire calls in flight.
func (fq *FairQueue) TelegramDepth() int {
	if ts, ok := fq.senders["telegram"].(*TelegramSender); ok {
		return ts.TelegramInFlight()
	}
	return 0
}

// recordFailure increments the consecutive failure counter for a target.
// When the threshold is reached, invokes the onDisabled callback and
// blocks delivery for failBlockDuration.
func (fq *FairQueue) recordFailure(target, name, jobType string) {
	val, _ := fq.failCounts.LoadOrStore(target, &failRecord{})
	rec := val.(*failRecord)
	count := int(rec.count.Add(1))

	if count == fq.failThreshold {
		rec.blockedAt = time.Now()
		log.Warnf("delivery: disabling %s (%s) after %d consecutive delivery failures", target, name, count)
		if fq.onDisabled != nil {
			fq.onDisabled(target, name, jobType)
		}
	}
}

// isTargetDisabled returns true if the target has been disabled from repeated
// failures and the block window hasn't expired. After the window, the record
// is cleaned up so delivery can resume (if the user re-enabled via any path).
func (fq *FairQueue) isTargetDisabled(target string) bool {
	val, ok := fq.failCounts.Load(target)
	if !ok {
		return false
	}
	rec := val.(*failRecord)
	if int(rec.count.Load()) < fq.failThreshold {
		return false
	}
	// Block window expired — clean up and allow delivery
	if time.Since(rec.blockedAt) > fq.failBlockDuration {
		fq.failCounts.Delete(target)
		return false
	}
	return true
}
