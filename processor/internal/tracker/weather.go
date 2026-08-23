package tracker

import (
	"strconv"
	"sync"
	"time"

	"github.com/golang/geo/s2"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/logref"
)

// WeatherChange represents a detected weather change event.
type WeatherChange struct {
	Longitude            float64      `json:"longitude"`
	Latitude             float64      `json:"latitude"`
	S2CellID             string       `json:"s2_cell_id"`
	GameplayCondition    int          `json:"gameplay_condition"`
	OldGameplayCondition int          `json:"old_gameplay_condition"`
	Updated              int64        `json:"updated"`
	Source               string       `json:"source"`
	Coords               [][2]float64 `json:"coords,omitempty"`
}

// localCellData holds locally inferred weather data per cell.
type localCellData struct {
	weatherFromBoost     [8]int
	currentHourTimestamp int64
	monsterWeather       int
	lastSeen             int64 // unix seconds of the last write; drives eviction
}

// controllerCellData holds weather data from weather webhooks.
type controllerCellData struct {
	lastCurrentWeatherCheck int64
	hourWeather             map[int64]int // hourTimestamp -> condition
	lastSeen                int64         // unix seconds of the last write; drives eviction
}

const (
	// weatherHistoryHours is how far back hourly entries stay reachable.
	//
	// Readers look back one hour, but they do it from two different clocks:
	// ExportCellWeather measures from wall clock, while UpdateFromWebhook
	// derives its previous-hour key from the webhook's own `updated` field.
	// Under a Golbat backlog the event clock lags the wall clock, so a
	// webhook stamped late in hour N can arrive once the sweep has already
	// advanced to N+1 and pruned the hour N-1 entry it needs to compare
	// against. That would read as hasPrevious=false and silently swallow a
	// genuine weather change.
	//
	// Two hours is therefore one hour of reader lookback plus one hour of
	// slack for event-time lag. Do not lower this to match the reader
	// literally; the extra hour is what absorbs the skew.
	weatherHistoryHours = 2

	// weatherCellIdleSecs is the default idle window: how long a cell
	// survives without a write before its state is dropped. Comfortably
	// longer than the default 8h AccuWeather forecast refresh.
	//
	// This is only the floor. forecast_refresh_interval is operator-settable
	// with no clamp, so WithForecastRefreshInterval raises the window when
	// the configured refresh would outlast it — see cellIdleSecs.
	weatherCellIdleSecs = 24 * 3600

	// weatherEvictInterval is the sweep cadence.
	weatherEvictInterval = 10 * time.Minute
)

// WeatherTracker manages per-S2-cell weather state.
// Port of weatherData.js.
type WeatherTracker struct {
	mu             sync.RWMutex
	controllerData map[string]*controllerCellData
	localData      map[string]*localCellData
	changes        chan WeatherChange

	// nowFunc is the clock used for cell liveness and eviction. Set once at
	// construction via WithClock and never reassigned: evictionLoop reads it
	// from its own goroutine, so a later write would be a data race.
	nowFunc func() time.Time

	// cellIdleSecs is how long a cell survives without a write. Sized at
	// construction so it always outlasts the configured forecast refresh.
	cellIdleSecs int64

	// onEvict is notified with the cell ids a sweep dropped, so components
	// keyed by the same cell ids can release their own per-cell state
	// instead of each re-deriving liveness. Called without wt.mu held.
	onEvict func(cellIDs []string)

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// WeatherOption configures a WeatherTracker at construction.
type WeatherOption func(*WeatherTracker)

// WithClock replaces the tracker's clock.
//
// An option rather than an assignable field: NewWeatherTracker starts
// evictionLoop before returning, and that goroutine reads nowFunc, so a clock
// handed over afterwards is an unsynchronized write. StatsTracker takes its
// clock at construction for the same reason.
func WithClock(nowFunc func() time.Time) WeatherOption {
	return func(wt *WeatherTracker) {
		if nowFunc != nil {
			wt.nowFunc = nowFunc
		}
	}
}

// WithForecastRefreshInterval sizes cell idle expiry against the AccuWeather
// refresh cadence, in hours.
//
// A cell whose only writes are forecast pushes is otherwise reclaimed whenever
// the configured refresh outlasts the idle window, taking
// lastCurrentWeatherCheck and the previous-hour entry with it — so the next
// real weather webhook sees hasPrevious=false and swallows a genuine change.
// Deriving the window here keeps that invariant true by construction rather
// than relying on the operator staying under an undocumented ceiling.
//
// Two refresh periods, so a single missed or delayed push does not strand the
// cell either.
func WithForecastRefreshInterval(hours int) WeatherOption {
	return func(wt *WeatherTracker) {
		if hours <= 0 {
			return
		}
		if want := int64(hours) * 3600 * 2; want > wt.cellIdleSecs {
			wt.cellIdleSecs = want
		}
	}
}

// NewWeatherTracker creates a new weather tracker.
func NewWeatherTracker(opts ...WeatherOption) *WeatherTracker {
	wt := &WeatherTracker{
		controllerData: make(map[string]*controllerCellData),
		localData:      make(map[string]*localCellData),
		changes:        make(chan WeatherChange, 100),
		nowFunc:        time.Now,
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
		cellIdleSecs:   weatherCellIdleSecs,
	}
	for _, opt := range opts {
		opt(wt)
	}
	go wt.evictionLoop()
	return wt
}

// Close stops the eviction loop and waits for it to exit. Safe to call more
// than once.
//
// Mirrors the stop/done/once trio used by mute.Sweeper, snapshots.Sweeper and
// expiringSet, so the loop can take its place in ProcessorService.Close's
// shutdown ordering rather than outliving the process's other components.
func (wt *WeatherTracker) Close() {
	if wt == nil {
		return
	}
	wt.once.Do(func() { close(wt.stop) })
	<-wt.done
}

func (wt *WeatherTracker) evictionLoop() {
	defer close(wt.done)
	ticker := time.NewTicker(weatherEvictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-wt.stop:
			return
		case <-ticker.C:
			wt.evict(wt.nowFunc().Unix())
		}
	}
}

// evict drops per-cell state that can no longer be read.
//
// Two things grow here. Per cell, hourWeather gained an entry every hour and
// nothing removed them, so a long-lived process accumulated entries
// proportional to (cells x uptime hours) — the actual leak. Separately, a cell
// that stops being scanned kept its struct forever, so a shifted scan area
// stranded state indefinitely.
func (wt *WeatherTracker) evict(now int64) {
	currentHour := now - (now % 3600)
	oldestHour := currentHour - weatherHistoryHours*3600
	idleBefore := now - wt.cellIdleSecs

	wt.mu.Lock()

	// A cell can hold both controller and local state, so membership is
	// tracked in a set rather than scanned. One sweep after a scan-area
	// shift can drop tens of thousands of cells, and this whole loop runs
	// under the write lock that every webhook needs.
	droppedSet := make(map[string]struct{})
	for cellID, cd := range wt.controllerData {
		if cd.lastSeen < idleBefore {
			delete(wt.controllerData, cellID)
			droppedSet[cellID] = struct{}{}
			continue
		}
		for ts := range cd.hourWeather {
			if ts < oldestHour {
				delete(cd.hourWeather, ts)
			}
		}
	}

	for cellID, ld := range wt.localData {
		if ld.lastSeen < idleBefore {
			delete(wt.localData, cellID)
			// Only report a cell once BOTH halves are gone. The two age out
			// independently: local inference can go quiet while weather
			// webhooks or forecast pushes keep the controller entry fresh.
			// Reporting on the local half alone hands a live cell to the
			// eviction callback, which discards its AccuWeather location key
			// and forecast timeout. The controller loop above has already
			// removed any idle controller entry, so a hit here means fresh.
			if _, stillControlled := wt.controllerData[cellID]; !stillControlled {
				droppedSet[cellID] = struct{}{}
			}
		}
	}

	var dropped []string
	if len(droppedSet) > 0 {
		dropped = make([]string, 0, len(droppedSet))
		for cellID := range droppedSet {
			dropped = append(dropped, cellID)
		}
	}

	onEvict := wt.onEvict
	wt.mu.Unlock()

	// Called outside the lock: the callback belongs to another component and
	// must not be able to deadlock the sweep by reaching back in.
	if onEvict != nil && len(dropped) > 0 {
		onEvict(dropped)
	}
}

// SetOnEvict registers a callback invoked after each sweep with the cell ids
// that were dropped. Call before the tracker starts seeing traffic.
func (wt *WeatherTracker) SetOnEvict(fn func(cellIDs []string)) {
	wt.mu.Lock()
	wt.onEvict = fn
	wt.mu.Unlock()
}

// Changes returns the channel that emits weather change events.
func (wt *WeatherTracker) Changes() <-chan WeatherChange {
	return wt.changes
}

// GetWeatherCellID returns the S2 level-10 cell ID for a lat/lon as a numeric string.
// This matches the format used by Golbat webhooks and the JS S2.keyToId().
func GetWeatherCellID(lat, lon float64) string {
	ll := s2.LatLngFromDegrees(lat, lon)
	cellID := s2.CellIDFromLatLng(ll).Parent(10)
	return strconv.FormatUint(uint64(cellID), 10)
}

// touchController returns the cell's controller state, creating it when
// absent, and stamps liveness.
//
// Every write path must go through this rather than inlining the
// create-if-missing block: eviction decides on lastSeen, so a path that
// stores data without stamping reads back correctly for a full idle window
// and then has its cells silently swept.
//
// Caller must hold wt.mu.
func (wt *WeatherTracker) touchController(cellID string) *controllerCellData {
	cd, ok := wt.controllerData[cellID]
	if !ok {
		cd = &controllerCellData{hourWeather: make(map[int64]int)}
		wt.controllerData[cellID] = cd
	}
	cd.lastSeen = wt.nowFunc().Unix()
	return cd
}

// touchLocal is touchController's counterpart for locally inferred state.
//
// Caller must hold wt.mu.
func (wt *WeatherTracker) touchLocal(cellID string) *localCellData {
	ld, ok := wt.localData[cellID]
	if !ok {
		ld = &localCellData{}
		wt.localData[cellID] = ld
	}
	ld.lastSeen = wt.nowFunc().Unix()
	return ld
}

// UpdateFromWebhook updates weather state from a direct weather webhook.
// Emits a change event if the weather has changed from the previous hour.
func (wt *WeatherTracker) UpdateFromWebhook(cellID string, condition int, timestamp int64, lat, lon float64, polygon [4][2]float64) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	cd := wt.touchController(cellID)

	hourTimestamp := timestamp - (timestamp % 3600)
	previousHourTimestamp := hourTimestamp - 3600

	// Check if weather changed from previous hour
	previousWeather, hasPrevious := cd.hourWeather[previousHourTimestamp]
	existingWeather, hasCurrentHour := cd.hourWeather[hourTimestamp]

	isNew := !hasCurrentHour || existingWeather != condition || cd.lastCurrentWeatherCheck < hourTimestamp
	changed := hasPrevious && previousWeather != condition && isNew

	logref.Debugf(cellID, "Weather webhook condition=%d hour=%d prevHour=%d hasPrev=%v prevWeather=%d hasCurrentHour=%v existingWeather=%d isNew=%v changed=%v",
		condition, hourTimestamp, previousHourTimestamp, hasPrevious, previousWeather, hasCurrentHour, existingWeather, isNew, changed)

	cd.hourWeather[hourTimestamp] = condition
	cd.lastCurrentWeatherCheck = timestamp

	if changed {
		// Send non-blocking
		select {
		case wt.changes <- WeatherChange{
			Longitude:            lon,
			Latitude:             lat,
			S2CellID:             cellID,
			GameplayCondition:    condition,
			OldGameplayCondition: previousWeather,
			Updated:              timestamp,
			Source:               "webhook",
			Coords:               polygon[:],
		}:
		default:
		}
	}
}

// GetCurrentWeatherInCell returns the current weather condition for a cell.
func (wt *WeatherTracker) GetCurrentWeatherInCell(cellID string) int {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	now := wt.nowFunc().Unix()
	currentHour := now - (now % 3600)

	cd := wt.controllerData[cellID]
	ld := wt.localData[cellID]

	var weather int
	if cd != nil && cd.lastCurrentWeatherCheck >= currentHour {
		weather = cd.hourWeather[currentHour]
	}
	// Local inference overrides if we have it for this hour
	if ld != nil && ld.currentHourTimestamp == currentHour {
		weather = ld.monsterWeather
	}
	return weather
}

// WeatherForecast holds current and next hour weather for a cell.
type WeatherForecast struct {
	Current int
	Next    int
}

// GetWeatherForecast returns the current and next hour weather for a cell.
func (wt *WeatherTracker) GetWeatherForecast(cellID string) WeatherForecast {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	now := wt.nowFunc().Unix()
	currentHour := now - (now % 3600)
	nextHour := currentHour + 3600

	var current, next int

	cd := wt.controllerData[cellID]
	ld := wt.localData[cellID]

	if cd != nil {
		current = cd.hourWeather[currentHour]
		next = cd.hourWeather[nextHour]
	}
	// Local inference overrides current hour
	if ld != nil && ld.currentHourTimestamp == currentHour {
		current = ld.monsterWeather
	}

	return WeatherForecast{Current: current, Next: next}
}

// GetNextHourTimestamp returns the timestamp of the next hour boundary.
func GetNextHourTimestamp() int64 {
	now := time.Now().Unix()
	return now - (now % 3600) + 3600
}

// ExportCellWeather returns weather data for a single cell, keyed by hour timestamp.
func (wt *WeatherTracker) ExportCellWeather(cellID string) map[int64]int {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	now := wt.nowFunc().Unix()
	currentHour := now - (now % 3600)

	result := make(map[int64]int)

	if cd := wt.controllerData[cellID]; cd != nil {
		for ts, condition := range cd.hourWeather {
			if ts >= currentHour-3600 {
				result[ts] = condition
			}
		}
	}
	// Override with local inference for current hour
	if ld := wt.localData[cellID]; ld != nil && ld.currentHourTimestamp == currentHour {
		result[currentHour] = ld.monsterWeather
	}

	return result
}

// SetHourWeather stores a weather condition for a specific hour in a cell.
// Used by AccuWeatherClient to store forecast data.
func (wt *WeatherTracker) SetHourWeather(cellID string, hourTimestamp int64, condition int) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	wt.touchController(cellID).hourWeather[hourTimestamp] = condition
}

// hasHourWeather checks if weather data exists for a specific hour in a cell.
func (wt *WeatherTracker) hasHourWeather(cellID string, hourTimestamp int64) bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	cd := wt.controllerData[cellID]
	if cd == nil {
		return false
	}
	_, ok := cd.hourWeather[hourTimestamp]
	return ok
}

// CheckWeatherOnMonster analyzes an incoming pokemon's weather boost to detect
// weather changes via vote-based inference.
// Port of weatherData.js:68-123.
func (wt *WeatherTracker) CheckWeatherOnMonster(cellID string, lat, lon float64, monsterWeather int) {
	now := wt.nowFunc().Unix()
	currentHour := now - (now % 3600)
	previousHour := currentHour - 3600

	wt.mu.Lock()
	defer wt.mu.Unlock()

	local := wt.touchLocal(cellID)
	controller := wt.touchController(cellID)

	// Only process if more than 30 seconds into the hour and monster has weather
	if now <= currentHour+30 || monsterWeather == 0 {
		return
	}

	if controller.lastCurrentWeatherCheck == 0 {
		controller.lastCurrentWeatherCheck = previousHour
	}

	currentWeather := controller.hourWeather[currentHour]

	// If observed weather agrees with up-to-date broadcast, reset counters
	if monsterWeather == currentWeather && controller.lastCurrentWeatherCheck >= currentHour {
		local.weatherFromBoost = [8]int{}
		return
	}

	if monsterWeather != currentWeather || (monsterWeather == currentWeather && controller.lastCurrentWeatherCheck < currentHour) {
		for i := range local.weatherFromBoost {
			if i == monsterWeather {
				local.weatherFromBoost[i]++
			} else {
				local.weatherFromBoost[i]--
			}
		}

		// Check if any weather type has enough votes (>4)
		changed := false
		for _, v := range local.weatherFromBoost {
			if v > 4 {
				changed = true
				break
			}
		}

		if changed {
			local.weatherFromBoost = [8]int{}

			// Determine the effective old weather: use previous hour's data if current
			// hour has no weather yet (which is normal at hour boundaries).
			oldWeather := currentWeather
			if oldWeather == 0 {
				oldWeather = controller.hourWeather[previousHour]
			}

			// Update state so subsequent pokemon in this hour don't re-trigger
			controller.hourWeather[currentHour] = monsterWeather
			controller.lastCurrentWeatherCheck = now

			// If we still have no prior weather, this is a first observation — not a change
			if oldWeather == 0 || oldWeather == monsterWeather {
				local.currentHourTimestamp = currentHour
				local.monsterWeather = monsterWeather
				logref.Infof(cellID, "Weather inferred as %d (no prior data or unchanged, no alert)", monsterWeather)
				return
			}

			if local.currentHourTimestamp != currentHour || local.monsterWeather != monsterWeather {
				local.currentHourTimestamp = currentHour
				local.monsterWeather = monsterWeather

				logref.Infof(cellID, "Boosted Pokemon! Force update of weather with weather %d", monsterWeather)

				// Use cell center instead of pokemon spawn point
				centerLat, centerLon := geo.GetCellCenter(lat, lon, 10)

				// Send non-blocking
				select {
				case wt.changes <- WeatherChange{
					Longitude:            centerLon,
					Latitude:             centerLat,
					S2CellID:             cellID,
					GameplayCondition:    monsterWeather,
					OldGameplayCondition: oldWeather,
					Updated:              now,
					Source:               "fromMonster",
					Coords:               geo.GetCellCoordsSlice(lat, lon, 10),
				}:
				default:
				}
			}
		}
	}
}

// hourCount returns how many hourly entries a cell holds.
func (wt *WeatherTracker) hourCount(cellID string) int {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	cd := wt.controllerData[cellID]
	if cd == nil {
		return 0
	}
	return len(cd.hourWeather)
}

// hasCell reports whether any state is held for a cell.
func (wt *WeatherTracker) hasCell(cellID string) bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	_, c := wt.controllerData[cellID]
	_, l := wt.localData[cellID]
	return c || l
}
