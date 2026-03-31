package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pokemon/poracleng/processor/internal/metrics"
	log "github.com/sirupsen/logrus"
)

// QueueConfig controls per-platform concurrency limits.
type QueueConfig struct {
	ConcurrentDiscord  int
	ConcurrentWebhook  int
	ConcurrentTelegram int
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
	return &FairQueue{
		ch:          ch,
		senders:     senders,
		tracker:     tracker,
		ctx:         ctx,
		cancel:      cancel,
		discordSem:  make(chan struct{}, cfg.ConcurrentDiscord),
		webhookSem:  make(chan struct{}, cfg.ConcurrentWebhook),
		telegramSem: make(chan struct{}, cfg.ConcurrentTelegram),
	}
}

// Start launches worker goroutines (one per concurrency slot) that drain the job channel.
func (fq *FairQueue) Start() {
	total := cap(fq.discordSem) + cap(fq.webhookSem) + cap(fq.telegramSem)
	for i := 0; i < total; i++ {
		fq.wg.Add(1)
		go fq.worker()
	}
}

// Stop cancels in-flight sends, closes the job channel, and waits for workers to finish.
func (fq *FairQueue) Stop() {
	fq.cancel()
	close(fq.ch)
	log.Info("delivery: waiting for queue workers to drain...")
	fq.wg.Wait()
	log.Info("delivery: queue workers drained")
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

	// 2. Acquire platform semaphore (limits global concurrency per platform)
	sem := fq.semaphoreFor(job.Type)
	sem <- struct{}{}
	defer func() { <-sem }()

	platform := PlatformFromType(job.Type)

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

	// If job has EditKey, try editing existing message first
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil {
			log.Debugf("delivery: attempting edit for key=%s target=%s", job.EditKey, job.Target)
			if err := sender.Edit(fq.ctx, existing.SentID, job.Message); err == nil {
				log.Debugf("delivery: edit succeeded for key=%s", job.EditKey)
				metrics.DeliveryTotal.WithLabelValues(platform, "edit_ok").Inc()
				metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())
				return
			}
			// Edit failed — fall through to send new message
			log.Debugf("delivery: edit failed for key=%s, sending new message", job.EditKey)
		}
	}

	// 3. Send new message
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
			// TODO: disable user in DB (future work)
		} else {
			log.Errorf("delivery: send failed for %s/%s: %v", job.Type, job.Target, err)
			metrics.DeliveryTotal.WithLabelValues(platform, "error").Inc()
		}
		metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())
		return
	}

	metrics.DeliveryTotal.WithLabelValues(platform, "ok").Inc()
	metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())

	// 4. Track for clean/edit if needed
	if sent != nil && (job.Clean || job.EditKey != "") {
		ttl := job.TTH.Duration()
		if ttl <= 0 {
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
