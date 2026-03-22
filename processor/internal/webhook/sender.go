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
	shouldFlush := depth >= s.batchSize
	s.mu.Unlock()

	metrics.SenderQueueDepth.Set(float64(depth))

	if shouldFlush {
		s.flush()
	}
}

// Close stops the sender and flushes remaining items.
func (s *Sender) Close() {
	close(s.done)
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
	toSend := s.batch
	s.batch = nil
	s.mu.Unlock()

	metrics.SenderQueueDepth.Set(0)
	metrics.SenderBatchSize.Observe(float64(len(toSend)))

	data, err := json.Marshal(toSend)
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
		log.Errorf("Failed to send to alerter (%d items): %s", len(toSend), err)
		metrics.SenderBatches.WithLabelValues("error").Inc()
		return
	}

	if resp.StatusCode >= 300 {
		log.Errorf("Alerter returned status %d (%d items)", resp.StatusCode, len(toSend))
		metrics.SenderBatches.WithLabelValues("error").Inc()
	} else {
		log.Debugf("Sent batch of %d items to alerter", len(toSend))
		metrics.SenderBatches.WithLabelValues("success").Inc()
	}
}
