package main

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// AlertTypeQuest is the alertType key for quest summaries. Used as
// the discriminator in summary_schedules rows and the SummaryBuffer's
// (humanID, alertType) keying. Future buffered-delivery alert types
// (raid summaries, …) would add their own constants alongside.
const AlertTypeQuest = "quest"

// SummaryDispatch is the per-(humanID, alertType) callback the scheduler
// invokes when a schedule fires. The wired implementation re-enriches
// buffered raw webhooks, groups by reward, renders one summary template
// per group, and clears the bucket.
type SummaryDispatch func(humanID, alertType string)

// schedulerSweepEvery: run SweepExpired every Nth tick. With ~6 ticks
// per hour from the profile minute marks, 6 ≈ hourly.
const schedulerSweepEvery = 6

// schedulerConfig is the slice of *config.Config the scheduler reads.
type schedulerConfig struct {
	// Locale is the fallback locale used when a human has no language set
	// (mirrors the rest of the processor's language fallback).
	Locale string
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
	cfg      schedulerConfig
	state    *state.Manager
	buffer   *tracker.SummaryBuffer
	dispatch SummaryDispatch
	// nowFunc is the clock; tests inject a fixed time. Production: time.Now.
	nowFunc func() time.Time
	stop    chan struct{}
	done    chan struct{}
}

// NewSummaryScheduler constructs a scheduler. The dispatch callback is
// invoked once per (humanID, alertType) whose schedule matches the
// current local time on each tick. Schedule reads come from
// state.State.SummarySchedules and human reads from state.State.Humans —
// no DB hit on the tick path.
func NewSummaryScheduler(
	cfg schedulerConfig,
	stateMgr *state.Manager,
	buffer *tracker.SummaryBuffer,
	dispatch SummaryDispatch,
) *SummaryScheduler {
	if dispatch == nil {
		dispatch = func(string, string) {}
	}
	return &SummaryScheduler{
		cfg:      cfg,
		state:    stateMgr,
		buffer:   buffer,
		dispatch: dispatch,
		nowFunc:  time.Now,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
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
			if tickN%schedulerSweepEvery == 0 {
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
			human := snap.Humans[humanID]
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
