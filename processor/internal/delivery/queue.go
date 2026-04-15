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
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	log "github.com/sirupsen/logrus"
)

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
	ch      chan *Job
	senders map[string]Sender
	tracker *MessageTracker
	wg      sync.WaitGroup

	// Shutdown context — cancelled when Stop() is called, aborts in-flight sends.
	ctx    context.Context
	cancel context.CancelFunc

	// Per-platform concurrency semaphores
	discordSem  chan struct{}
	webhookSem  chan struct{}
	telegramSem chan struct{}

	// Per-destination locks ensure max 1 in-flight send per target
	destLocks sync.Map // target string → *sync.Mutex

	// Per-platform in-flight counters for metrics
	discordInFlight  atomic.Int64
	webhookInFlight  atomic.Int64
	telegramInFlight atomic.Int64

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
}

// NewFairQueue creates a FairQueue that reads jobs from ch and dispatches them
// through the appropriate sender, respecting per-platform concurrency limits.
func NewFairQueue(ch chan *Job, senders map[string]Sender, tracker *MessageTracker, cfg QueueConfig) *FairQueue {
	if cfg.ConcurrentDiscord <= 0 {
		cfg.ConcurrentDiscord = 1
	}
	if cfg.ConcurrentWebhook <= 0 {
		cfg.ConcurrentWebhook = 1
	}
	if cfg.ConcurrentTelegram <= 0 {
		cfg.ConcurrentTelegram = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	failThreshold := cfg.FailThreshold
	if failThreshold <= 0 {
		failThreshold = 10
	}
	return &FairQueue{
		ch:                ch,
		senders:           senders,
		tracker:           tracker,
		ctx:               ctx,
		cancel:            cancel,
		discordSem:        make(chan struct{}, cfg.ConcurrentDiscord),
		webhookSem:        make(chan struct{}, cfg.ConcurrentWebhook),
		telegramSem:       make(chan struct{}, cfg.ConcurrentTelegram),
		failThreshold:     failThreshold,
		failBlockDuration: 5 * time.Minute,
		onDisabled:        cfg.OnDisabled,
		rateLimiter:       cfg.RateLimiter,
		rateLimitHooks:    cfg.RateLimitHooks,
	}
}

// Start launches worker goroutines (one per concurrency slot) that drain the job channel.
func (fq *FairQueue) Start() {
	total := cap(fq.discordSem) + cap(fq.webhookSem) + cap(fq.telegramSem)
	for range total {
		fq.wg.Add(1)
		go fq.worker()
	}
}

// Stop closes the job channel, waits for workers to drain remaining jobs,
// then cancels the context. Channel is closed first so queued jobs are still
// delivered before shutdown. The Dispatcher owns channel creation; FairQueue
// closes it here as part of the coordinated shutdown sequence.
func (fq *FairQueue) Stop() {
	close(fq.ch)
	log.Info("delivery: waiting for queue workers to drain...")
	fq.wg.Wait()
	log.Info("delivery: queue workers drained")
	fq.cancel()
}

func (fq *FairQueue) worker() {
	defer fq.wg.Done()
	for job := range fq.ch {
		fq.processJob(job)
	}
}

func (fq *FairQueue) processJob(job *Job) {
	// 1. Acquire per-destination lock (ensures max 1 send per target)
	lockI, _ := fq.destLocks.LoadOrStore(job.Target, &sync.Mutex{})
	destLock := lockI.(*sync.Mutex)
	destLock.Lock()
	defer destLock.Unlock()

	// 2. Wait for rate limits BEFORE acquiring semaphore so that
	//    rate-limited goroutines don't hold concurrency slots.
	platform := PlatformFromType(job.Type)
	if sender, ok := fq.senders[platform]; ok {
		sender.WaitForRateLimit(job.Target)
	}

	// 3. Acquire platform semaphore (limits global concurrency per platform)
	sem := fq.semaphoreFor(job.Type)
	sem <- struct{}{}
	defer func() { <-sem }()

	// Track per-platform in-flight count
	counter := fq.counterFor(job.Type)
	counter.Add(1)
	metrics.DeliveryInFlight.WithLabelValues(platform).Inc()
	defer func() {
		counter.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues(platform).Dec()
	}()
	sender, ok := fq.senders[platform]
	if !ok {
		log.Warnf("delivery: no sender for platform %q (type=%s)", platform, job.Type)
		return
	}

	start := time.Now()

	// If job has EditKey, try editing existing message first.
	// Edits are mutations of an already-counted send — they do NOT consume
	// rate-limit budget. Only fall through to the new-send path (which does
	// count) if no tracked message exists or the edit attempt fails.
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil {
			log.Infof("%s: edit: found tracked message for key=%s, attempting edit", job.LogReference, job.EditKey)
			if err := sender.Edit(fq.ctx, existing.SentID, job.Message); err == nil {
				log.Infof("%s: edit: succeeded for key=%s", job.LogReference, job.EditKey)
				metrics.DeliveryTotal.WithLabelValues(platform, "edit_ok").Inc()
				metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())
				return
			} else {
				log.Warnf("%s: edit: failed for key=%s: %v, sending new message", job.LogReference, job.EditKey, err)
			}
		} else {
			log.Debugf("%s: edit: no tracked message for key=%s, will send new and track", job.LogReference, job.EditKey)
		}
	}

	// 3. Send new message — skip if target has been disabled from repeated failures
	if fq.isTargetDisabled(job.Target) {
		metrics.DeliveryTotal.WithLabelValues(platform, "stopped").Inc()
		return
	}

	// 3b. Authoritative rate-limit count. Bypass jobs (rate-limit
	//     notifications, ban farewells) skip the check entirely so the
	//     limiter can never swallow the very message reporting on itself.
	if fq.rateLimiter != nil && !job.BypassRateLimit {
		result := fq.rateLimiter.Check(job.Target, job.Type)
		if !result.Allowed {
			if result.JustBreached {
				metrics.RateLimitBreaches.Inc()
				metrics.RateLimitDropped.Inc()
				log.Infof("%s: rate limit reached for %s %s %s (%d messages in %ds)",
					job.LogReference, job.Type, job.Target, job.Name, result.Limit, result.ResetSeconds)
				if fq.rateLimitHooks != nil {
					// Hooks dispatch bypass jobs back into the same channel.
					// Calling them synchronously here would block this worker
					// while it still holds the per-destination mutex — and if
					// the channel is full of further jobs to the same target
					// (the very condition that produced the breach) those
					// jobs cannot drain because we hold their lock. Fire and
					// forget instead, so the worker can release the dest lock
					// promptly while the hook completes asynchronously.
					hooks := fq.rateLimitHooks
					target, typ, name, lang := job.Target, job.Type, job.Name, job.Language
					limit, reset, banned, ref := result.Limit, result.ResetSeconds, result.Banned, job.LogReference
					go func() {
						hooks.OnBreach(target, typ, name, lang, limit, reset)
						if banned {
							metrics.RateLimitDisabled.Inc()
							log.Infof("%s: rate limit: banning %s %s %s (too many violations)",
								ref, typ, target, name)
							hooks.OnBan(target, typ, name, lang)
						}
					}()
				}
			} else {
				metrics.RateLimitDropped.Inc()
				log.Debugf("%s: rate limited: dropping message for %s %s %s",
					job.LogReference, job.Type, job.Target, job.Name)
			}
			return
		}
	}

	destKind := strings.ToUpper(strings.TrimPrefix(job.Type, platform+":"))
	if destKind == "" {
		destKind = strings.ToUpper(job.Type)
	}
	log.Infof("%s: -> %s %s %s Sending %s message", job.LogReference, job.Name, job.Target, destKind, platform)

	sent, err := sender.Send(fq.ctx, job)
	if err != nil {
		var permErr *PermanentError
		if errors.As(err, &permErr) {
			log.Warnf("delivery: permanent error for %s/%s: %s", job.Type, job.Target, permErr.Reason)
			metrics.DeliveryTotal.WithLabelValues(platform, "permanent_error").Inc()
			fq.recordFailure(job.Target, job.Name, job.Type)
		} else {
			log.Errorf("delivery: send failed for %s/%s: %v", job.Type, job.Target, err)
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

	// 4. Track for clean/edit if needed
	if sent != nil && (db.IsClean(job.Clean) || db.IsEdit(job.Clean) || job.EditKey != "") {
		ttl := job.TTH.Duration()
		if ttl <= 0 {
			log.Warnf("%s: clean/edit tracking skipped — TTL already expired (clean=%d)", job.LogReference, job.Clean)
			return
		}

		key := job.EditKey
		if key == "" {
			key = fmt.Sprintf("clean:%s:%s:%s", job.Type, job.Target, sent.ID)
		}

		fq.tracker.Track(key, &TrackedMessage{
			SentID: sent.ID,
			Target: job.Target,
			Type:   job.Type,
			Clean:  job.Clean,
		}, ttl)
		log.Debugf("%s: tracked message key=%s sentID=%s ttl=%v clean=%d", job.LogReference, key, sent.ID, ttl, job.Clean)
	}
}

func (fq *FairQueue) semaphoreFor(jobType string) chan struct{} {
	if jobType == "webhook" {
		return fq.webhookSem
	}
	platform := PlatformFromType(jobType)
	switch platform {
	case "telegram":
		return fq.telegramSem
	default:
		return fq.discordSem
	}
}

func (fq *FairQueue) counterFor(jobType string) *atomic.Int64 {
	if jobType == "webhook" {
		return &fq.webhookInFlight
	}
	platform := PlatformFromType(jobType)
	switch platform {
	case "telegram":
		return &fq.telegramInFlight
	default:
		return &fq.discordInFlight
	}
}

// DiscordDepth returns the number of discord jobs currently in-flight.
func (fq *FairQueue) DiscordDepth() int { return int(fq.discordInFlight.Load()) }

// WebhookDepth returns the number of webhook jobs currently in-flight.
func (fq *FairQueue) WebhookDepth() int { return int(fq.webhookInFlight.Load()) }

// TelegramDepth returns the number of telegram jobs currently in-flight.
func (fq *FairQueue) TelegramDepth() int { return int(fq.telegramInFlight.Load()) }

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
