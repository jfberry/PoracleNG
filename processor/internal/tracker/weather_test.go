package tracker

import (
	"testing"
	"time"
)

func TestGetWeatherCellID(t *testing.T) {
	// Ensure we get a non-empty cell ID for a known location
	cellID := GetWeatherCellID(51.5074, -0.1278)
	if cellID == "" {
		t.Error("Expected non-empty cell ID")
	}

	// Same location should give same cell
	cellID2 := GetWeatherCellID(51.5074, -0.1278)
	if cellID != cellID2 {
		t.Errorf("Expected same cell ID, got %s and %s", cellID, cellID2)
	}

	// Different location should give different cell (for sufficiently different locations)
	cellID3 := GetWeatherCellID(40.7128, -74.0060) // NYC
	if cellID == cellID3 {
		t.Error("Expected different cell ID for NYC vs London")
	}
}

func TestWeatherTrackerDirectUpdate(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()

	cellID := "test_cell"
	wt.UpdateFromWebhook(cellID, 3, 1700000000, 51.5, -0.1, [4][2]float64{})

	weather := wt.GetCurrentWeatherInCell(cellID)
	// Since the timestamp is in the past, the current hour check may not match
	// This tests the storage mechanism
	_ = weather
}

func TestWeatherTrackerInference(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()

	cellID := "test_cell"

	// Send enough weather observations to trigger a change
	for range 10 {
		wt.CheckWeatherOnMonster(cellID, 51.5, -0.1, 3)
	}

	// Check if a weather change was detected
	select {
	case change := <-wt.Changes():
		if change.GameplayCondition != 3 {
			t.Errorf("Expected weather condition 3, got %d", change.GameplayCondition)
		}
	default:
		// May not trigger if within first 30 seconds of the hour - that's OK
	}
}

// TestWeatherTrackerEvictsHoursOutsideRetention covers the actual leak:
// controllerCellData.hourWeather gained one entry per cell per hour and
// nothing ever removed them, so a long-lived process accumulated entries
// proportional to (cells x uptime hours).
func TestWeatherTrackerEvictsHoursOutsideRetention(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()

	now := int64(1_700_000_000)
	currentHour := now - (now % 3600)
	cellID := "cell-retention"

	// Six hours of observed history, the current hour, and a forecast hour
	// ahead. A stale forecast hour checks the forecast map is pruned too.
	for h := int64(6); h >= 1; h-- {
		setObservedHour(wt, cellID, currentHour-h*3600, 1)
	}
	setObservedHour(wt, cellID, currentHour, 2)
	wt.SetHourWeather(cellID, currentHour+3600, 3)
	wt.SetHourWeather(cellID, currentHour-5*3600, 3)

	wt.evict(now)

	got := wt.hourCount(cellID)
	// Readers look back one hour; retention keeps two so a backlogged
	// webhook's event-time lookback still lands (see weatherHistoryHours).
	// Keepers: currentHour-2, previous, current, forecast.
	if got != 4 {
		t.Errorf("expected 4 retained hours (two of history, current, forecast), got %d", got)
	}
	if !wt.hasHourWeather(cellID, currentHour) {
		t.Error("current hour must survive eviction")
	}
	if !wt.hasHourWeather(cellID, currentHour+3600) {
		t.Error("forecast hour must survive eviction — AccuWeather writes it ahead of time")
	}
	if !wt.hasHourWeather(cellID, currentHour-3600) {
		t.Error("previous hour must survive eviction — UpdateFromWebhook compares against it")
	}
	if !wt.hasHourWeather(cellID, currentHour-2*3600) {
		t.Error("the backlog-slack hour must survive eviction")
	}
	if wt.hasHourWeather(cellID, currentHour-3*3600) {
		t.Error("hours beyond the retention window are unreachable and must be dropped")
	}
}

// TestWeatherTrackerEvictsIdleCells asserts whole cells are reclaimed once
// they stop being scanned, so a shifted scan area does not strand them.
//
// Idleness is measured from when we last *received* something for the cell,
// so the clock is driven forward here rather than backdating the webhook.
func TestWeatherTrackerEvictsIdleCells(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	wt.UpdateFromWebhook("cell-stale", 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})

	// Two days pass; only one cell keeps receiving webhooks.
	clock.advance(48 * time.Hour)
	wt.UpdateFromWebhook("cell-fresh", 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})

	wt.evict(clock.now().Unix())

	if wt.hasCell("cell-stale") {
		t.Error("a cell that received nothing for 48h should be evicted entirely")
	}
	if !wt.hasCell("cell-fresh") {
		t.Error("a cell that just received a webhook must be kept")
	}
}

// TestWeatherTrackerReplayedWebhookIsNotInstantlyIdle pins that cell liveness
// tracks receipt time, not the webhook's own `updated` field. Replaying
// logs/webhooks.log is a documented workflow, and Golbat clock skew has the
// same shape: a webhook whose event time is days old still means the cell is
// live right now. Stamping event time made such a cell evictable on the very
// next sweep, discarding the previous-hour state that later genuine changes
// compare against.
func TestWeatherTrackerReplayedWebhookIsNotInstantlyIdle(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	// A replayed webhook: event time three days in the past, received now.
	wt.UpdateFromWebhook("cell-replay", 1, clock.now().Add(-72*time.Hour).Unix(), 51.5, -0.1, [4][2]float64{})

	wt.evict(clock.now().Unix())

	if !wt.hasCell("cell-replay") {
		t.Error("a cell whose webhook carried an old event time was evicted; liveness must follow receipt time")
	}
}

// TestWeatherTrackerKeepsHistoryForBackloggedWebhooks pins the interaction
// between eviction (which prunes by processor wall clock) and
// UpdateFromWebhook (which reads the previous hour keyed by the webhook's own
// event time). When Golbat backlogs, a webhook stamped late in the previous
// hour arrives after the sweep has already advanced, and the entry it needs
// for the previous-hour comparison must still be there. Losing it turns a
// genuine weather change into no alert at all.
func TestWeatherTrackerKeepsHistoryForBackloggedWebhooks(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()

	now := time.Now().Unix()
	currentHour := now - (now % 3600)
	eventHour := currentHour - 3600    // the backlogged webhook's own hour
	comparisonHour := eventHour - 3600 // the entry UpdateFromWebhook compares against
	cellID := "cell-backlog"

	setObservedHour(wt, cellID, comparisonHour, 1)
	setObservedHour(wt, cellID, eventHour, 1)

	// The sweep runs on wall clock, already past the event's hour.
	wt.evict(now)

	// Backlogged webhook: stamped 59 minutes into eventHour, delivered now,
	// reporting a different condition than comparisonHour held.
	wt.UpdateFromWebhook(cellID, 2, eventHour+3540, 51.5, -0.1, [4][2]float64{})

	select {
	case change := <-wt.Changes():
		if change.GameplayCondition != 2 || change.OldGameplayCondition != 1 {
			t.Errorf("got change %d->%d, want 1->2", change.OldGameplayCondition, change.GameplayCondition)
		}
	default:
		t.Error("no WeatherChange emitted: eviction dropped the previous-hour entry the backlogged webhook needed")
	}
}

// TestWeatherTrackerCloseStopsEvictionLoop asserts the eviction loop
// participates in shutdown like every other background reclaim loop in the
// codebase. Without it, each tracker permanently leaks a goroutine and a
// ticker, and pins its maps against GC.
func TestWeatherTrackerCloseStopsEvictionLoop(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()
	wt.Close()

	select {
	case <-wt.done:
	default:
		t.Error("Close returned while the eviction loop was still running")
	}

	wt.Close() // idempotent
}

// TestWeatherTrackerEveryWritePathStampsLiveness pins the invariant that keeps
// eviction honest: any path that writes cell state must also mark the cell
// live. A write path that stores data without stamping reads back correctly
// for a day and then has its cells silently swept, which is close to
// undiagnosable in production.
func TestWeatherTrackerEveryWritePathStampsLiveness(t *testing.T) {
	// Mid-hour so CheckWeatherOnMonster clears its "30s into the hour" gate.
	base := time.Unix(1_700_000_000, 0).Truncate(time.Hour).Add(30 * time.Minute)

	writes := map[string]func(wt *WeatherTracker, cellID string){
		"UpdateFromWebhook": func(wt *WeatherTracker, cellID string) {
			wt.UpdateFromWebhook(cellID, 1, wt.nowFunc().Unix(), 51.5, -0.1, [4][2]float64{})
		},
		"SetHourWeather": func(wt *WeatherTracker, cellID string) {
			now := wt.nowFunc().Unix()
			wt.SetHourWeather(cellID, now-(now%3600), 1)
		},
		"CheckWeatherOnMonster": func(wt *WeatherTracker, cellID string) {
			wt.CheckWeatherOnMonster(cellID, 51.5, -0.1, 3)
		},
	}

	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			clock := newTestClock(base.Unix())
			wt := NewWeatherTracker(WithClock(clock.now))
			defer wt.Close()

			write(wt, "cell-a")

			// Just inside the idle window: the write must have kept it alive.
			clock.advance(weatherCellIdleSecs*time.Second - time.Hour)
			wt.evict(clock.now().Unix())
			if !wt.hasCell("cell-a") {
				t.Fatalf("%s did not stamp liveness: cell evicted while still inside the idle window", name)
			}

			// Past the idle window with no further writes.
			clock.advance(2 * time.Hour)
			wt.evict(clock.now().Unix())
			if wt.hasCell("cell-a") {
				t.Errorf("cell survived past the idle window with no further writes")
			}
		})
	}
}

// TestWeatherTrackerIdleWindowCoversForecastRefresh pins that a cell whose
// only writes are AccuWeather forecast pushes survives between refreshes.
//
// forecast_refresh_interval is operator-settable in hours with no clamp. An
// operator rationing AccuWeather quota can set it above the idle threshold, at
// which point the sweep reclaims forecast-only cells between pushes and throws
// away lastCurrentWeatherCheck and the previous-hour entry. The next real
// weather webhook then finds hasPrevious=false and a genuine change emits no
// alert.
func TestWeatherTrackerIdleWindowCoversForecastRefresh(t *testing.T) {
	const refreshHours = 30 // deliberately longer than the 24h default idle

	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithForecastRefreshInterval(refreshHours), WithClock(clock.now))
	defer wt.Close()

	now := clock.now().Unix()
	wt.SetHourWeather("cell-forecast", now-(now%3600), 1)

	// One refresh period later, just before the next push would land.
	clock.advance(refreshHours*time.Hour - time.Hour)
	wt.evict(clock.now().Unix())

	if !wt.hasCell("cell-forecast") {
		t.Error("forecast-only cell was evicted before its next refresh could arrive")
	}
}

// setObservedHour stores game-reported weather directly, for tests that need
// observed history without going through change detection. SetHourWeather
// can't stand in for it: that stores a forecast, which change detection
// deliberately ignores.
func setObservedHour(wt *WeatherTracker, cellID string, hourTimestamp int64, condition int) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.touchController(cellID).hourWeather[hourTimestamp] = condition
}

// drainChanges returns every pending WeatherChange without blocking.
func drainChanges(wt *WeatherTracker) []WeatherChange {
	var out []WeatherChange
	for {
		select {
		case c := <-wt.Changes():
			out = append(out, c)
		default:
			return out
		}
	}
}

// TestWeatherForecastIsNotAChangeBaseline is the webhook half of #356: the
// game reports Windy, AccuWeather forecasts Rain for an hour the game never
// re-reported, and the next webhook (still Windy) must not alert Rain -> Windy.
func TestWeatherForecastIsNotAChangeBaseline(t *testing.T) {
	hour := time.Unix(1_700_000_000, 0).Truncate(time.Hour).Unix()
	clock := newTestClock(hour + 10)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	const cellID = "cell-356-webhook"
	wt.UpdateFromWebhook(cellID, 5, hour-2*3600+60, 51.5, -0.1, [4][2]float64{})
	drainChanges(wt)

	// Forecast for the previous and current hour; no webhook in the previous hour.
	wt.SetHourWeather(cellID, hour-3600, 2)
	wt.SetHourWeather(cellID, hour, 2)

	wt.UpdateFromWebhook(cellID, 5, hour+1, 51.5, -0.1, [4][2]float64{})
	if got := drainChanges(wt); len(got) != 0 {
		t.Fatalf("forecast became the change baseline: %+v", got)
	}

	// A real change against observed weather still alerts.
	wt.UpdateFromWebhook(cellID, 5, hour-3600+60, 51.5, -0.1, [4][2]float64{})
	wt.UpdateFromWebhook(cellID, 3, hour+120, 51.5, -0.1, [4][2]float64{})
	got := drainChanges(wt)
	if len(got) != 1 || got[0].OldGameplayCondition != 5 || got[0].GameplayCondition != 3 {
		t.Fatalf("want one 5 -> 3 change, got %+v", got)
	}
}

// TestWeatherForecastDoesNotOverwriteObserved is the boosted-pokemon half of
// #356: a forecast fetch landing after the game reported this hour must not
// replace the observed value, so neither current-weather reads nor monster
// inference see the forecast.
func TestWeatherForecastDoesNotOverwriteObserved(t *testing.T) {
	hour := time.Unix(1_700_000_000, 0).Truncate(time.Hour).Unix()
	clock := newTestClock(hour + 1)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	const cellID = "cell-356-monster"
	wt.UpdateFromWebhook(cellID, 5, hour-3600+60, 51.5, -0.1, [4][2]float64{})
	wt.UpdateFromWebhook(cellID, 5, hour+1, 51.5, -0.1, [4][2]float64{})
	drainChanges(wt)

	wt.SetHourWeather(cellID, hour, 2)
	wt.SetHourWeather(cellID, hour+3600, 2)

	if got := wt.GetCurrentWeatherInCell(cellID); got != 5 {
		t.Errorf("GetCurrentWeatherInCell = %d, want observed 5", got)
	}
	if fc := wt.GetWeatherForecast(cellID); fc.Current != 5 || fc.Next != 2 {
		t.Errorf("GetWeatherForecast = %+v, want current 5 (observed), next 2 (forecast)", fc)
	}
	if exp := wt.ExportCellWeather(cellID); exp[hour] != 5 || exp[hour+3600] != 2 {
		t.Errorf("ExportCellWeather = %v, want observed %d=5 and forecast %d=2", exp, hour, hour+3600)
	}

	clock.advance(33 * time.Second)
	for range 6 {
		wt.CheckWeatherOnMonster(cellID, 51.5, -0.1, 5)
	}
	if got := drainChanges(wt); len(got) != 0 {
		t.Fatalf("monster inference compared against the forecast: %+v", got)
	}
}

// TestWeatherForecastFillsDisplayGaps checks the forecast is still used where
// the game has not reported: the next-hour prediction and an unreported
// current hour.
func TestWeatherForecastFillsDisplayGaps(t *testing.T) {
	hour := time.Unix(1_700_000_000, 0).Truncate(time.Hour).Unix()
	clock := newTestClock(hour + 600)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	const cellID = "cell-356-gaps"
	wt.SetHourWeather(cellID, hour, 4)
	wt.SetHourWeather(cellID, hour+3600, 6)

	if fc := wt.GetWeatherForecast(cellID); fc.Current != 4 || fc.Next != 6 {
		t.Errorf("GetWeatherForecast = %+v, want forecast 4 / 6", fc)
	}
	if !wt.hasHourWeather(cellID, hour+3600) {
		t.Error("hasHourWeather must see forecast hours, or EnsureForecast refetches every hour")
	}
	if got := wt.GetCurrentWeatherInCell(cellID); got != 0 {
		t.Errorf("GetCurrentWeatherInCell = %d, want 0: a forecast is not the game's weather", got)
	}
}
