package delivery

import (
	log "github.com/sirupsen/logrus"
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
}

// Dispatcher is the top-level entry point for message delivery.
// It owns the job channel, fair queue, and message tracker.
type Dispatcher struct {
	ch      chan *Job
	queue   *FairQueue
	tracker *MessageTracker
}

// NewDispatcher creates a Dispatcher with the configured senders, tracker, and queue.
func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error) {
	senders := make(map[string]Sender)
	if cfg.DiscordToken != "" {
		senders["discord"] = NewDiscordSender(cfg.DiscordToken, cfg.UploadImages, cfg.DeleteDelayMs)
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

	queue := NewFairQueue(ch, senders, tracker, cfg.Queue)

	return &Dispatcher{ch: ch, queue: queue, tracker: tracker}, nil
}

// NewDispatcherWithSenders creates a Dispatcher with externally-provided senders (for testing).
func NewDispatcherWithSenders(senders map[string]Sender, tracker *MessageTracker, queueSize int, queueCfg QueueConfig) *Dispatcher {
	if queueSize <= 0 {
		queueSize = 1000
	}
	ch := make(chan *Job, queueSize)
	queue := NewFairQueue(ch, senders, tracker, queueCfg)
	return &Dispatcher{ch: ch, queue: queue, tracker: tracker}
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
	d.queue.Stop()
	d.tracker.Stop()
}

// QueueDepth returns the number of jobs waiting in the channel.
func (d *Dispatcher) QueueDepth() int { return len(d.ch) }

// TrackerSize returns the number of messages being tracked.
func (d *Dispatcher) TrackerSize() int { return d.tracker.Size() }
