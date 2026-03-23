package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Sender batches and sends matched results to the alerter.
// Payloads with pending tiles are held until the tile resolves or a deadline expires.
type Sender struct {
	alerterURL    string
	client        *http.Client
	mu            sync.Mutex
	batch         []OutboundPayload
	batchSize     int
	flushInterval time.Duration
	done          chan struct{}
}

// NewSender creates a new matched result sender.
func NewSender(alerterURL string, batchSize int, flushIntervalMillis int) *Sender {
	s := &Sender{
		alerterURL:    alerterURL,
		client:        &http.Client{Timeout: 10 * time.Second},
		batchSize:     batchSize,
		flushInterval: time.Duration(flushIntervalMillis) * time.Millisecond,
		done:          make(chan struct{}),
	}
	go s.flushLoop()
	return s
}

// Send queues a payload for sending to the alerter.
func (s *Sender) Send(payload OutboundPayload) {
	s.mu.Lock()
	s.batch = append(s.batch, payload)
	depth := len(s.batch)
	s.mu.Unlock()

	metrics.SenderQueueDepth.Set(float64(depth))
	metrics.IntervalMatched.Add(1)

	// Don't trigger immediate flush — let the flush loop handle it
	// so pending tiles have time to resolve
}

// Close stops the sender and flushes remaining items.
func (s *Sender) Close() {
	close(s.done)
	// Force-resolve any remaining pending tiles with fallback
	s.mu.Lock()
	for i := range s.batch {
		if tp := s.batch[i].TilePending; tp != nil {
			select {
			case url := <-tp.Result:
				tp.Apply(url)
			default:
				tp.Apply(tp.Fallback)
			}
			s.batch[i].TilePending = nil
		}
	}
	s.mu.Unlock()
	s.flush()
}

func (s *Sender) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.done:
			return
		}
	}
}

func (s *Sender) flush() {
	s.mu.Lock()
	if len(s.batch) == 0 {
		s.mu.Unlock()
		return
	}

	now := time.Now()
	var ready []OutboundPayload
	var waiting []OutboundPayload

	for i := range s.batch {
		p := s.batch[i]
		if p.TilePending == nil {
			ready = append(ready, p)
			continue
		}

		// Non-blocking check for tile result
		select {
		case url := <-p.TilePending.Result:
			p.TilePending.Apply(url)
			p.TilePending = nil
			ready = append(ready, p)
		default:
			// Not yet resolved — check deadline
			if now.After(p.TilePending.Deadline) {
				p.TilePending.Apply(p.TilePending.Fallback)
				p.TilePending = nil
				ready = append(ready, p)
				metrics.TileTotal.WithLabelValues("sender_deadline").Inc()
			} else {
				waiting = append(waiting, p)
			}
		}
	}

	s.batch = waiting
	depth := len(waiting)
	s.mu.Unlock()

	metrics.SenderQueueDepth.Set(float64(depth))

	if len(ready) > 0 {
		s.sendBatch(ready)
	}
}

func (s *Sender) sendBatch(batch []OutboundPayload) {
	metrics.SenderBatchSize.Observe(float64(len(batch)))

	data, err := json.Marshal(batch)
	if err != nil {
		log.Errorf("Failed to marshal batch: %s", err)
		metrics.SenderBatches.WithLabelValues("error").Inc()
		return
	}

	start := time.Now()
	resp, err := s.client.Post(s.alerterURL+"/api/matched", "application/json", bytes.NewReader(data))
	metrics.SenderFlushDuration.Observe(time.Since(start).Seconds())

	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		log.Errorf("Failed to send to alerter (%d items): %s", len(batch), err)
		metrics.SenderBatches.WithLabelValues("error").Inc()
		return
	}

	if resp.StatusCode >= 300 {
		log.Errorf("Alerter returned status %d (%d items)", resp.StatusCode, len(batch))
		metrics.SenderBatches.WithLabelValues("error").Inc()
	} else {
		log.Debugf("Sent batch of %d items to alerter", len(batch))
		metrics.SenderBatches.WithLabelValues("success").Inc()
	}
}
