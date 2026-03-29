package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	apiSecret     string
	client        *http.Client
	mu            sync.Mutex
	batch         []OutboundPayload
	batchSize     int
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewSender creates a new matched result sender.
func NewSender(alerterURL string, apiSecret string, batchSize int, flushIntervalMillis int) *Sender {
	s := &Sender{
		alerterURL:    alerterURL,
		apiSecret:     apiSecret,
		client:        &http.Client{Timeout: 10 * time.Second},
		batchSize:     batchSize,
		flushInterval: time.Duration(flushIntervalMillis) * time.Millisecond,
		done:          make(chan struct{}),
	}
	s.wg.Add(1)
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

// Close stops the flush loop, resolves any remaining pending tiles, and flushes.
func (s *Sender) Close() {
	close(s.done)
	s.wg.Wait() // wait for flushLoop to exit before touching batch

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
	defer s.wg.Done()
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
	req, err := http.NewRequest(http.MethodPost, s.alerterURL+"/api/matched", bytes.NewReader(data))
	if err != nil {
		log.Errorf("Failed to create request: %s", err)
		metrics.SenderBatches.WithLabelValues("error").Inc()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiSecret != "" {
		req.Header.Set("X-Poracle-Secret", s.apiSecret)
	}

	resp, err := s.client.Do(req)
	metrics.SenderFlushDuration.Observe(time.Since(start).Seconds())

	if resp != nil {
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
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

// DeliverMessages POSTs pre-rendered delivery jobs directly to the alerter's
// /api/deliverMessages endpoint, bypassing the matched→controller pipeline.
func (s *Sender) DeliverMessages(jobs []DeliveryJob) error {
	body, err := json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("marshal delivery jobs: %w", err)
	}
	if len(body) <= 2 { // "[]" or "null"
		return nil
	}

	url := s.alerterURL + "/api/deliverMessages"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create deliver request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiSecret != "" {
		req.Header.Set("X-Poracle-Secret", s.apiSecret)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver messages: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deliver messages HTTP %d", resp.StatusCode)
	}

	return nil
}
