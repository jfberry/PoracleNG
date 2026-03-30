package delivery

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

	// Per-platform concurrency semaphores
	discordSem  chan struct{}
	webhookSem  chan struct{}
	telegramSem chan struct{}
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

	return &FairQueue{
		ch:          ch,
		senders:     senders,
		tracker:     tracker,
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

// Stop closes the job channel and waits for all workers to finish.
func (fq *FairQueue) Stop() {
	close(fq.ch)
	fq.wg.Wait()
}

func (fq *FairQueue) worker() {
	defer fq.wg.Done()
	for job := range fq.ch {
		fq.processJob(job)
	}
}

func (fq *FairQueue) processJob(job *Job) {
	// 1. Acquire platform semaphore
	sem := fq.semaphoreFor(job.Type)
	sem <- struct{}{}
	defer func() { <-sem }()

	platform := PlatformFromType(job.Type)
	sender, ok := fq.senders[platform]
	if !ok {
		log.Warnf("delivery: no sender for platform %q (type=%s)", platform, job.Type)
		return
	}

	start := time.Now()

	// 2. If job has EditKey, try editing existing message first
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil {
			if err := sender.Edit(context.Background(), existing.SentID, job.Message); err == nil {
				metrics.DeliveryTotal.WithLabelValues(platform, "edit_ok").Inc()
				metrics.DeliveryDuration.WithLabelValues(platform).Observe(time.Since(start).Seconds())
				return
			}
			// Edit failed — fall through to send new message
			log.Debugf("delivery: edit failed for %s, sending new message", job.EditKey)
		}
	}

	// 3. Send new message
	sent, err := sender.Send(context.Background(), job)
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
