package main

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// SummaryDispatch is the per-(humanID, alertType) callback the scheduler
// invokes when a schedule fires. The implementation in PR 6 will:
//   1. List entries from the buffer
//   2. Filter expired
//   3. Re-enrich each per-user-language
//   4. Group by (rewardType, reward)
//   5. Render + dispatch one questSummary message per group
//   6. Clear the bucket
//
// Until PR 6 lands, the callback installed by main.go is a no-op + log so
// the scheduler structure can be tested and wired through end-to-end.
type SummaryDispatch func(humanID, alertType string)

// schedulerConfig is the slice of *config.Config the scheduler needs.
// Passed inline instead of taking the whole config to keep the test seam
// thin and make it explicit which knobs the scheduler reads.
type schedulerConfig struct {
	// Locale is the fallback locale used when a human has no language set
	// (mirrors the rest of the processor's language fallback).
	Locale string
	// QuestSummaryBufferTTLHours is currently advisory. The scheduler
	// sweeps based on each entry's reported ExpiresAt; this knob is
	// reserved for a future safety-net sweep on CreatedAt.
	QuestSummaryBufferTTLHours int
}

// SummaryScheduler wakes at fixed wall-clock minute marks (the same marks
// the profile scheduler uses), walks each user's per-(alertType)
// summary schedule, and dispatches a grouped summary for every
// (humanID, alertType) whose schedule matches the current local time.
//
// Buffer expiry is swept every Nth tick to evict entries whose reported
// ExpiresAt has passed (e.g. quests that rolled over before the user's
// schedule fired).
type SummaryScheduler struct {
	cfg       schedulerConfig
	state     *state.Manager
	humans    store.HumanStore
	schedules store.SummaryScheduleStore
	buffer    *tracker.SummaryBuffer
	dispatch  SummaryDispatch
	// sweepEvery: run SweepExpired every Nth tick. <=0 disables sweep.
	// With ~6 ticks/hour (the profile minute marks), 6 is hourly.
	sweepEvery int
	// nowFunc is the clock; tests inject a fixed time. Production: time.Now.
	nowFunc func() time.Time
	stop    chan struct{}
	done    chan struct{}
}

// NewSummaryScheduler constructs a scheduler. The dispatch callback is
// invoked once per (humanID, alertType) whose schedule matches the
// current local time on each tick. schedules is currently used for CRUD
// writes by callers; reads happen against state.State.SummarySchedules.
func NewSummaryScheduler(
	cfg schedulerConfig,
	stateMgr *state.Manager,
	humans store.HumanStore,
	schedules store.SummaryScheduleStore,
	buffer *tracker.SummaryBuffer,
	dispatch SummaryDispatch,
	sweepEvery int,
) *SummaryScheduler {
	if dispatch == nil {
		dispatch = func(string, string) {}
	}
	return &SummaryScheduler{
		cfg:        cfg,
		state:      stateMgr,
		humans:     humans,
		schedules:  schedules,
		buffer:     buffer,
		dispatch:   dispatch,
		sweepEvery: sweepEvery,
		nowFunc:    time.Now,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start launches the scheduler loop in a background goroutine. Safe to
// call exactly once.
func (s *SummaryScheduler) Start() { go s.loop() }

// Close stops the loop and waits for it to drain.
func (s *SummaryScheduler) Close() {
	if s == nil {
		return
	}
	close(s.stop)
	<-s.done
}

// loop is the main scheduler loop. It aligns wakeups to the same fixed
// wall-clock minute marks the profile scheduler uses so that the
// 10-minute matchesTimeWindow lines up with user-configured schedules.
func (s *SummaryScheduler) loop() {
	defer close(s.done)
	tickN := 0
	for {
		now := s.nowFunc()
		next := nextScheduleTime(now, profileScheduleMinutes)
		select {
		case <-s.stop:
			return
		case <-time.After(next.Sub(now)):
			s.tick()
			tickN++
			if s.sweepEvery > 0 && tickN%s.sweepEvery == 0 {
				removed := s.buffer.SweepExpired(s.nowFunc().Unix())
				if removed > 0 {
					log.Infof("summary scheduler: swept %d expired buffered entries", removed)
				}
			}
		}
	}
}

// tick walks every (humanID, alertType) schedule in the current state
// snapshot and invokes dispatch for those whose active_hours match the
// current local time.
func (s *SummaryScheduler) tick() {
	snap := s.state.Get()
	if snap == nil {
		return
	}
	for humanID, byType := range snap.SummarySchedules {
		for alertType, entries := range byType {
			if len(entries) == 0 {
				continue
			}
			human, err := s.humans.Get(humanID)
			if err != nil {
				log.Debugf("summary scheduler: humans.Get(%s) failed: %v", humanID, err)
				continue
			}
			if human == nil {
				continue
			}
			if !isScheduleActive(entries, human.Latitude, human.Longitude, s.nowFunc) {
				continue
			}
			s.dispatch(humanID, alertType)
		}
	}
}
