package enrichment

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	eventsURL         = "https://raw.githubusercontent.com/bigfoott/ScrapedDuck/data/events.json"
	eventsRefreshSecs = 6 * 60 * 60 // 6 hours
	eventsRetrySecs   = 15 * 60     // 15 minutes on error
	eventsTimeoutSecs = 10
)

// ScrapedDuckEvent represents a single event from the ScrapedDuck events.json.
type ScrapedDuckEvent struct {
	Name      string `json:"name"`
	EventType string `json:"eventType"`
	Start     string `json:"start"`
	End       string `json:"end"`
}

// EventResult holds the result of checking if a future event overlaps a spawn/quest window.
type EventResult struct {
	FutureEvent        bool   `json:"futureEvent"`
	FutureEventTime    string `json:"futureEventTime"`
	FutureEventName    string `json:"futureEventName"`
	FutureEventTrigger string `json:"futureEventTrigger"`
}

// PogoEventChecker downloads and caches pogo events, checking for spawn/quest overlaps.
type PogoEventChecker struct {
	mu          sync.RWMutex
	spawnEvents []ScrapedDuckEvent
	questEvents []ScrapedDuckEvent
	timeLayout  string
	stopCh      chan struct{}
}

// NewPogoEventChecker creates a new event checker that periodically downloads events.
func NewPogoEventChecker(timeLayout string) *PogoEventChecker {
	ec := &PogoEventChecker{
		timeLayout: timeLayout,
		stopCh:     make(chan struct{}),
	}
	go ec.refreshLoop()
	return ec
}

// Close stops the background refresh loop.
func (ec *PogoEventChecker) Close() {
	close(ec.stopCh)
}

func (ec *PogoEventChecker) refreshLoop() {
	ec.fetchAndLoad()
	for {
		select {
		case <-ec.stopCh:
			return
		case <-time.After(eventsRefreshSecs * time.Second):
			ec.fetchAndLoad()
		}
	}
}

func (ec *PogoEventChecker) fetchAndLoad() {
	log.Info("PogoEvents: Fetching new event file")
	events, err := downloadEvents()
	if err != nil {
		log.Errorf("PogoEvents: Cannot download pogo event file: %s", err)
		// Retry sooner on error
		go func() {
			select {
			case <-ec.stopCh:
				return
			case <-time.After(eventsRetrySecs * time.Second):
				ec.fetchAndLoad()
			}
		}()
		return
	}
	ec.loadEvents(events)
	log.Infof("PogoEvents: Loaded %d spawn events, %d quest events", len(ec.spawnEvents), len(ec.questEvents))
}

func downloadEvents() ([]ScrapedDuckEvent, error) {
	client := &http.Client{Timeout: eventsTimeoutSecs * time.Second}
	resp, err := client.Get(eventsURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("server error: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var events []ScrapedDuckEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return events, nil
}

func (ec *PogoEventChecker) loadEvents(events []ScrapedDuckEvent) {
	var spawn, quest []ScrapedDuckEvent
	for _, e := range events {
		switch e.EventType {
		case "community-day":
			spawn = append(spawn, e)
			quest = append(quest, e)
		case "pokemon-spotlight-hour":
			spawn = append(spawn, e)
		}
	}
	ec.mu.Lock()
	ec.spawnEvents = spawn
	ec.questEvents = quest
	ec.mu.Unlock()
}

// parseEventTime parses a ScrapedDuck event time string in the given timezone.
// ScrapedDuck times are wall-clock strings like "2024-01-13 11:00" interpreted
// in the pokemon/quest location's timezone.
func parseEventTime(eventTime, tz string) (int64, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	// Try common ScrapedDuck formats
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	} {
		t, err := time.ParseInLocation(layout, eventTime, loc)
		if err == nil {
			return t.Unix(), nil
		}
	}
	return 0, fmt.Errorf("cannot parse event time %q", eventTime)
}

// extractHourMinute extracts "HH:mm" from a ScrapedDuck time string.
// This matches the JS behavior of moment(event.start).format('HH:mm').
func extractHourMinute(eventTime, tz, timeLayout string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	} {
		t, err := time.ParseInLocation(layout, eventTime, loc)
		if err == nil {
			return t.Format(timeLayout)
		}
	}
	return eventTime
}

// EventChangesSpawn checks if any spawn-relevant event boundary falls
// within the window [startTime, disappearTime] at the given location.
func (ec *PogoEventChecker) EventChangesSpawn(startTime, disappearTime int64, tz string) *EventResult {
	ec.mu.RLock()
	events := ec.spawnEvents
	ec.mu.RUnlock()

	if len(events) == 0 {
		return nil
	}

	return checkEvents(events, startTime, disappearTime, tz, ec.timeLayout)
}

// EventChangesQuest checks if any quest-relevant event boundary falls
// within the window [startTime, disappearTime] at the given location.
func (ec *PogoEventChecker) EventChangesQuest(startTime, disappearTime int64, tz string) *EventResult {
	ec.mu.RLock()
	events := ec.questEvents
	ec.mu.RUnlock()

	if len(events) == 0 {
		return nil
	}

	return checkEvents(events, startTime, disappearTime, tz, ec.timeLayout)
}

func checkEvents(events []ScrapedDuckEvent, startTime, disappearTime int64, tz, timeLayout string) *EventResult {
	for _, event := range events {
		eventStart, err := parseEventTime(event.Start, tz)
		if err != nil {
			continue
		}
		eventEnd, err := parseEventTime(event.End, tz)
		if err != nil {
			continue
		}
		if startTime < eventStart && eventStart < disappearTime {
			return &EventResult{
				FutureEvent:        true,
				FutureEventTime:    extractHourMinute(event.Start, tz, timeLayout),
				FutureEventName:    event.Name,
				FutureEventTrigger: "start",
			}
		}
		if startTime < eventEnd && eventEnd < disappearTime {
			return &EventResult{
				FutureEvent:        true,
				FutureEventTime:    extractHourMinute(event.End, tz, timeLayout),
				FutureEventName:    event.Name,
				FutureEventTrigger: "end",
			}
		}
	}
	return nil
}
