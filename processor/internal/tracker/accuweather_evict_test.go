package tracker

import (
	"sync"
	"testing"
	"time"
)

// TestWeatherTrackerNotifiesOnEvict asserts the sweep reports which cells it
// dropped, so components keyed by the same cell ids can release their own
// state instead of each re-deriving liveness.
func TestWeatherTrackerNotifiesOnEvict(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	var evicted []string
	wt.SetOnEvict(func(cellIDs []string) { evicted = append(evicted, cellIDs...) })

	wt.UpdateFromWebhook("cell-gone", 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})

	clock.advance(48 * time.Hour)
	wt.UpdateFromWebhook("cell-live", 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})
	wt.evict(clock.now().Unix())

	if len(evicted) != 1 || evicted[0] != "cell-gone" {
		t.Fatalf("onEvict got %v, want exactly [cell-gone]", evicted)
	}
}

// TestAccuWeatherForgetCells asserts the forecast client releases every
// per-cell map it owns. cellMutexes, cellLocations and cellForecasts share the
// WeatherTracker's keyspace but had no delete path at all, so a shifted scan
// area stranded one mutex, one location key and one forecastState per
// abandoned S2 cell for the life of the process.
func TestAccuWeatherForgetCells(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()

	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	aw.cellMutexes["cell-gone"] = nil
	aw.cellLocations["cell-gone"] = "12345"
	aw.cellForecasts["cell-gone"] = &forecastState{}
	aw.cellLocations["cell-live"] = "67890"

	aw.ForgetCells([]string{"cell-gone"})

	if _, ok := aw.cellMutexes["cell-gone"]; ok {
		t.Error("cellMutexes still holds the evicted cell")
	}
	if _, ok := aw.cellLocations["cell-gone"]; ok {
		t.Error("cellLocations still holds the evicted cell")
	}
	if _, ok := aw.cellForecasts["cell-gone"]; ok {
		t.Error("cellForecasts still holds the evicted cell")
	}
	if _, ok := aw.cellLocations["cell-live"]; !ok {
		t.Error("a live cell was dropped")
	}
}

// TestWeatherTrackerEvictReportsEachCellOnce pins that a cell holding both
// controller and local state is reported to onEvict exactly once, and that the
// dedup behind that stays linear in the number of cells dropped: a shifted
// scan area can drop tens of thousands in one sweep, all under wt.mu.
func TestWeatherTrackerEvictReportsEachCellOnce(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	const cells = 5000
	for i := range cells {
		id := encounterIDForTest(i)
		// Both maps, so the naive dedup would have to scan for each one.
		wt.UpdateFromWebhook(id, 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})
		wt.CheckWeatherOnMonster(id, 51.5, -0.1, 3)
	}

	var dropped []string
	wt.SetOnEvict(func(ids []string) { dropped = append(dropped, ids...) })

	clock.advance(48 * time.Hour)
	wt.evict(clock.now().Unix())

	if len(dropped) != cells {
		t.Fatalf("onEvict reported %d cells, want %d", len(dropped), cells)
	}
	seen := make(map[string]struct{}, len(dropped))
	for _, id := range dropped {
		if _, dup := seen[id]; dup {
			t.Fatalf("cell %s reported more than once", id)
		}
		seen[id] = struct{}{}
	}
}

// BenchmarkWeatherEvictDropsManyCells covers the sweep that follows a
// scan-area shift, where one pass drops every cell it holds. The whole loop
// runs under wt.mu, so its cost is a stall on every webhook path.
func BenchmarkWeatherEvictDropsManyCells(b *testing.B) {
	const cells = 10000

	for b.Loop() {
		b.StopTimer()
		clock := newTestClock(1_700_000_000)
		wt := NewWeatherTracker(WithClock(clock.now))
		for i := range cells {
			id := encounterIDForTest(i)
			wt.UpdateFromWebhook(id, 1, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})
			wt.CheckWeatherOnMonster(id, 51.5, -0.1, 3)
		}
		clock.advance(48 * time.Hour)
		b.StartTimer()

		wt.evict(clock.now().Unix())

		b.StopTimer()
		wt.Close()
		b.StartTimer()
	}
}

// TestForgetCellsSpareInFlightRequests pins the interaction between the
// eviction sweep and a forecast request already in progress.
//
// EnsureForecast releases aw.mu, blocks on the per-cell mutex and then on an
// HTTP call, and re-reads cellForecasts afterwards. Before eviction existed
// nothing could remove that entry, so the re-read was safe by construction.
// Once ForgetCells could delete it mid-request, the re-read became a nil
// dereference that panics the processor, and deleting the per-cell mutex let a
// second request for the same cell start against a fresh mutex and run
// concurrently.
func TestForgetCellsSpareInFlightRequests(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()
	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	const busy, idle = "cell-busy", "cell-idle"
	for _, id := range []string{busy, idle} {
		aw.cellMutexes[id] = &sync.Mutex{}
		aw.cellLocations[id] = "loc-" + id
		aw.cellForecasts[id] = &forecastState{}
	}

	// A request is working on `busy` right now.
	aw.markInFlight(busy)

	aw.ForgetCells([]string{busy, idle})

	if aw.cellForecasts[busy] == nil {
		t.Error("forecast state for an in-flight cell was evicted; the request will nil-deref on its next re-read")
	}
	if aw.cellMutexes[busy] == nil {
		t.Error("per-cell mutex for an in-flight cell was evicted; a second request would run concurrently")
	}
	if aw.cellForecasts[idle] != nil || aw.cellMutexes[idle] != nil || aw.cellLocations[idle] != "" {
		t.Error("an idle cell was not reclaimed")
	}

	// Releasing the hold completes the deferred eviction; see
	// TestForgetCellsReclaimsAfterInFlightRequestCompletes.
	aw.doneInFlight(busy)

	if aw.cellForecasts[busy] != nil || aw.cellMutexes[busy] != nil {
		t.Error("cell was not reclaimed after its request completed")
	}
}

// TestFetchForecastTailToleratesEvictedCell covers the second unguarded
// re-read: fetchForecast writes the refresh timeout back after its HTTP call,
// by which point the cell may legitimately be gone.
func TestFetchForecastTailToleratesEvictedCell(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()
	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	// No entry for this cell at all, as if eviction ran during the request.
	aw.storeForecastTimeout("cell-vanished", 12345)
}

// TestForgetCellsReclaimsAfterInFlightRequestCompletes closes the gap left by
// deferring an eviction: WeatherTracker reports a cell exactly once, at the
// moment it deletes it, so a cell skipped because a request was in flight is
// never offered to ForgetCells again.
//
// If that request then exits without reactivating the cell (quota exhausted,
// HTTP or decode failure, no usable forecast), nothing else reclaims it and
// its mutex, location key and forecast state stay resident for the life of the
// process. The hold itself has to trigger the reclaim when it is released.
func TestForgetCellsReclaimsAfterInFlightRequestCompletes(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()
	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	const cellID = "cell-abandoned"
	aw.cellMutexes[cellID] = &sync.Mutex{}
	aw.cellLocations[cellID] = "loc"
	aw.cellForecasts[cellID] = &forecastState{}

	aw.mu.Lock()
	aw.markInFlight(cellID)
	aw.mu.Unlock()

	// The sweep offers the cell once and is refused.
	aw.ForgetCells([]string{cellID})
	if aw.cellForecasts[cellID] == nil {
		t.Fatal("in-flight cell was evicted")
	}

	// The request fails and exits. No further sweep will ever name this cell,
	// so releasing the hold must be what reclaims it.
	aw.doneInFlight(cellID)

	if aw.cellForecasts[cellID] != nil {
		t.Error("forecast state leaked after the in-flight request finished")
	}
	if aw.cellMutexes[cellID] != nil {
		t.Error("per-cell mutex leaked after the in-flight request finished")
	}
	if aw.cellLocations[cellID] != "" {
		t.Error("location key leaked after the in-flight request finished")
	}
}

// TestForgetCellsDoesNotReclaimUnevictedCells guards the other direction: a
// request finishing on a cell nobody asked to evict must leave it alone.
func TestForgetCellsDoesNotReclaimUnevictedCells(t *testing.T) {
	wt := NewWeatherTracker()
	defer wt.Close()
	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	const cellID = "cell-live"
	aw.cellLocations[cellID] = "loc"
	aw.cellForecasts[cellID] = &forecastState{}

	aw.mu.Lock()
	aw.markInFlight(cellID)
	aw.mu.Unlock()
	aw.doneInFlight(cellID)

	if aw.cellForecasts[cellID] == nil || aw.cellLocations[cellID] == "" {
		t.Error("state for a cell that was never evicted was reclaimed")
	}
}

// TestDeferredEvictionSkippedWhenCellCameBack covers a deferred eviction that
// goes stale while it waits.
//
// fetchForecast calls SetHourWeather on success, which recreates the tracker
// cell and stamps it live. Reclaiming on that basis throws away the location
// key and the forecast timeout the request just paid AccuWeather for, so the
// next alert performs a fresh location lookup and a fresh 12-hour forecast:
// two more requests against a 500/day quota, for a cell that is demonstrably
// active.
func TestDeferredEvictionSkippedWhenCellCameBack(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()
	aw := NewAccuWeatherClient(AccuWeatherConfig{}, wt)

	const cellID = "cell-refreshed"
	aw.cellMutexes[cellID] = &sync.Mutex{}
	aw.cellLocations[cellID] = "loc"
	aw.cellForecasts[cellID] = &forecastState{}

	aw.mu.Lock()
	aw.markInFlight(cellID)
	aw.mu.Unlock()

	// The sweep drops the cell and is refused because a request holds it.
	aw.ForgetCells([]string{cellID})

	// The request succeeds: fetchForecast writes forecast hours, which
	// recreates the tracker cell, and records the refresh timeout.
	now := clock.now().Unix()
	wt.SetHourWeather(cellID, now-(now%3600)+3600, 3)
	aw.storeForecastTimeout(cellID, now+7200)

	aw.doneInFlight(cellID)

	if aw.cellLocations[cellID] == "" {
		t.Error("location key discarded for a cell the fetch brought back; next alert re-pays for the lookup")
	}
	if fs := aw.cellForecasts[cellID]; fs == nil {
		t.Error("forecast state discarded for a cell the fetch brought back")
	} else if fs.forecastTimeout == 0 {
		t.Error("forecast timeout lost; next alert re-fetches before the configured refresh")
	}
}

// TestEvictDoesNotReportCellsStillHeldByControllerData pins that a cell is
// reported to onEvict only once it is genuinely gone from the tracker.
//
// Local inference and controller data age out independently: a cell can stop
// receiving boosted-pokemon votes while weather webhooks (or AccuWeather
// forecast pushes) keep its controller entry fresh. Reporting it because only
// the local half expired hands a live cell to ForgetCells, which throws away
// its location key and forecast timeout and makes the next alert re-pay for
// both.
func TestEvictDoesNotReportCellsStillHeldByControllerData(t *testing.T) {
	clock := newTestClock(1_700_000_000)
	wt := NewWeatherTracker(WithClock(clock.now))
	defer wt.Close()

	const cellID = "cell-half-idle"

	var reported []string
	wt.SetOnEvict(func(ids []string) { reported = append(reported, ids...) })

	// Local inference recorded now, then left to go stale.
	wt.CheckWeatherOnMonster(cellID, 51.5, -0.1, 3)

	// Two days on, controller data is still arriving for the same cell.
	clock.advance(48 * time.Hour)
	wt.UpdateFromWebhook(cellID, 2, clock.now().Unix(), 51.5, -0.1, [4][2]float64{})

	wt.evict(clock.now().Unix())

	if len(reported) != 0 {
		t.Errorf("onEvict reported %v for a cell whose controller data is still live", reported)
	}
	if !wt.hasCell(cellID) {
		t.Error("cell was removed entirely despite fresh controller data")
	}
}
