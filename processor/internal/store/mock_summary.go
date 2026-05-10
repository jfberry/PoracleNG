package store

import (
	"encoding/json"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// MockSummaryScheduleStore is an in-memory SummaryScheduleStore for tests.
// It records every method call (for assertion) and matches the shape of
// MockHumanStore.
type MockSummaryScheduleStore struct {
	mu        sync.RWMutex
	schedules map[mockSummaryKey]SummarySchedule

	// Calls records method names invoked, mirroring MockHumanStore so
	// downstream tests can assert call ordering / counts.
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ParsedActiveHours == nil {
		s.ParsedActiveHours = parseMockActiveHours(s.ActiveHours)
	}
	m.schedules[mockSummaryKey{ID: s.ID, AlertType: s.AlertType}] = s
}

func (m *MockSummaryScheduleStore) record(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, method)
}

func (m *MockSummaryScheduleStore) Get(id, alertType string) (*SummarySchedule, error) {
	m.record("Get")
	m.mu.RLock()
	defer m.mu.RUnlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.schedules, mockSummaryKey{ID: id, AlertType: alertType})
	return nil
}

func (m *MockSummaryScheduleStore) ListByType(alertType string) ([]SummarySchedule, error) {
	m.record("ListByType")
	m.mu.RLock()
	defer m.mu.RUnlock()
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

// parseMockActiveHours mirrors the SQL store's parsing behaviour for
// mock callers that want a populated ParsedActiveHours field. Errors
// are silently dropped so test fixtures can omit the field.
func parseMockActiveHours(raw string) []db.ActiveHourEntry {
	if len(raw) <= 5 {
		return nil
	}
	var entries []db.ActiveHourEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		log.Debugf("MockSummaryScheduleStore: failed to parse active_hours: %v", err)
		return nil
	}
	return entries
}
