package store

import (
	"sync"
	"sync/atomic"
)

// MockTrackingStore is an in-memory implementation of TrackingStore[T] for testing.
// All inserted rows are stored in a flat list. SelectByIDProfile returns all rows
// (suitable for single-user tests).
type MockTrackingStore[T any] struct {
	mu      sync.Mutex
	rows    []T
	nextUID atomic.Int64
	getUID  func(*T) int64
	setUID  func(*T, int64)
}

// NewMockTrackingStore creates a MockTrackingStore with the given UID accessor functions.
func NewMockTrackingStore[T any](getUID func(*T) int64, setUID func(*T, int64)) *MockTrackingStore[T] {
	m := &MockTrackingStore[T]{
		getUID: getUID,
		setUID: setUID,
	}
	m.nextUID.Store(1)
	return m
}

func (m *MockTrackingStore[T]) SelectByIDProfile(id string, profileNo int) ([]T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]T, len(m.rows))
	copy(out, m.rows)
	return out, nil
}

func (m *MockTrackingStore[T]) Insert(row *T) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	uid := m.nextUID.Add(1) - 1
	m.setUID(row, uid)
	m.rows = append(m.rows, *row)
	return uid, nil
}

func (m *MockTrackingStore[T]) DeleteByUIDs(id string, uids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uidSet := make(map[int64]bool, len(uids))
	for _, u := range uids {
		uidSet[u] = true
	}
	var kept []T
	for i := range m.rows {
		if !uidSet[m.getUID(&m.rows[i])] {
			kept = append(kept, m.rows[i])
		}
	}
	m.rows = kept
	return nil
}

func (m *MockTrackingStore[T]) DeleteByUID(id string, uid int64) error {
	return m.DeleteByUIDs(id, []int64{uid})
}

// AllRows returns all stored rows (for test assertions).
func (m *MockTrackingStore[T]) AllRows() []T {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]T, len(m.rows))
	copy(out, m.rows)
	return out
}
