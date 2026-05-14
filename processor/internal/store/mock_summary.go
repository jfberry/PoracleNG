package store

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// MockSummaryScheduleStore is an in-memory SummaryScheduleStore for tests.
// It records every method call (for assertion) and matches the shape of
// MockHumanStore.
//
// Locking: `dataMu` guards `schedules` and the inject-error fields;
// `callsMu` independently guards `Calls`. The two mutexes are
// intentionally separate so the record path doesn't acquire one lock,
// release it, then re-acquire the data lock — that pattern is fragile
// if a future refactor inlines record() and creates a sequential
// Lock→Lock on the same goroutine.
type MockSummaryScheduleStore struct {
	dataMu    sync.RWMutex
	schedules map[mockSummaryKey]SummarySchedule
	// listErr, when non-nil, is returned by ListByType. Used by tests
	// that need to exercise the loader's error-tolerance path.
	listErr error

	callsMu sync.Mutex
	// Calls records method names invoked, mirroring MockHumanStore so
	// downstream tests can assert call ordering / counts. Guarded by
	// callsMu, NOT dataMu.
	Calls []string
}

type mockSummaryKey struct {
	ID        string
	AlertType string
}

// NewMockSummaryScheduleStore creates an empty mock store.
func NewMockSummaryScheduleStore() *MockSummaryScheduleStore {
	return &MockSummaryScheduleStore{
		schedules: make(map[mockSummaryKey]SummarySchedule),
	}
}

// Seed inserts a schedule directly without recording a call. Useful for
// arranging fixtures before exercising the system under test.
func (m *MockSummaryScheduleStore) Seed(s SummarySchedule) {
	m.dataMu.Lock()
	defer m.dataMu.Unlock()
	if s.ParsedActiveHours == nil {
		s.ParsedActiveHours = parseMockActiveHours(s.ActiveHours)
	}
	m.schedules[mockSummaryKey{ID: s.ID, AlertType: s.AlertType}] = s
}

// InjectListError makes the next (and every subsequent) ListByType
// call return the given error. Used by tests that need to exercise the
// loader's error-tolerance path. Pass nil to clear.
func (m *MockSummaryScheduleStore) InjectListError(err error) {
	m.dataMu.Lock()
	defer m.dataMu.Unlock()
	m.listErr = err
}

func (m *MockSummaryScheduleStore) record(method string) {
	m.callsMu.Lock()
	defer m.callsMu.Unlock()
	m.Calls = append(m.Calls, method)
}

func (m *MockSummaryScheduleStore) Get(id, alertType string) (*SummarySchedule, error) {
	m.record("Get")
	m.dataMu.RLock()
	defer m.dataMu.RUnlock()
	s, ok := m.schedules[mockSummaryKey{ID: id, AlertType: alertType}]
	if !ok {
		return nil, nil
	}
	cp := s
	if s.ParsedActiveHours != nil {
		cp.ParsedActiveHours = append([]db.ActiveHourEntry(nil), s.ParsedActiveHours...)
	}
	return &cp, nil
}

func (m *MockSummaryScheduleStore) Set(id, alertType, activeHoursJSON string) error {
	m.record("Set")
	m.dataMu.Lock()
	defer m.dataMu.Unlock()
	m.schedules[mockSummaryKey{ID: id, AlertType: alertType}] = SummarySchedule{
		ID:                id,
		AlertType:         alertType,
		ActiveHours:       activeHoursJSON,
		ParsedActiveHours: parseMockActiveHours(activeHoursJSON),
	}
	return nil
}

func (m *MockSummaryScheduleStore) Delete(id, alertType string) error {
	m.record("Delete")
	m.dataMu.Lock()
	defer m.dataMu.Unlock()
	delete(m.schedules, mockSummaryKey{ID: id, AlertType: alertType})
	return nil
}

func (m *MockSummaryScheduleStore) ListByType(alertType string) ([]SummarySchedule, error) {
	m.record("ListByType")
	m.dataMu.RLock()
	defer m.dataMu.RUnlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]SummarySchedule, 0)
	for k, s := range m.schedules {
		if k.AlertType != alertType {
			continue
		}
		cp := s
		if s.ParsedActiveHours != nil {
			cp.ParsedActiveHours = append([]db.ActiveHourEntry(nil), s.ParsedActiveHours...)
		}
		out = append(out, cp)
	}
	return out, nil
}

// parseMockActiveHours wraps db.ParseActiveHours, dropping errors so
// test fixtures can omit the field.
func parseMockActiveHours(raw string) []db.ActiveHourEntry {
	entries, err := db.ParseActiveHours(raw)
	if err != nil {
		log.Debugf("MockSummaryScheduleStore: failed to parse active_hours: %v", err)
		return nil
	}
	return entries
}
