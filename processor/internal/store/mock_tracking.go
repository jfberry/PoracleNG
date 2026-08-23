package store

import (
	"sync"
	"sync/atomic"
)

// MockTrackingStore is an in-memory implementation of TrackingStore[T] for testing.
// All inserted rows are stored in a flat list. SelectByIDProfile returns all rows
// (suitable for single-user tests).
//
// When a getID accessor is supplied via WithIDScope, the mock enforces the same
// (id, uid) ownership scoping the SQL store does: SelectByIDProfile returns only
// rows owned by id, and DeleteByUID(s) only affects rows whose owning id matches.
// This lets handler-layer tests verify that one human cannot touch another's uid.
type MockTrackingStore[T any] struct {
	mu        sync.Mutex
	rows      []T
	nextUID   atomic.Int64
	getUID    func(*T) int64
	setUID    func(*T, int64)
	getID     func(*T) string // nil = no id scoping (legacy single-user behaviour)
	getProfNo func(*T) int    // nil = no profile scoping (legacy single-profile behaviour)
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

// WithIDScope enables (id, uid) ownership scoping using the given owner-id
// accessor, then returns the store for chaining. Without it the mock behaves as
// a single-user flat list.
func (m *MockTrackingStore[T]) WithIDScope(getID func(*T) string) *MockTrackingStore[T] {
	m.getID = getID
	return m
}

// WithProfileScope enables profile filtering using the given profile-no accessor,
// then returns the store for chaining. Without it the mock ignores profileNo (the
// legacy single-profile behaviour), so SelectByIDProfile returns every owned row
// regardless of profile. With it, the mock mirrors the SQL store's
// `WHERE id=? AND profile_no=?` — required for tests that span profiles.
func (m *MockTrackingStore[T]) WithProfileScope(getProfNo func(*T) int) *MockTrackingStore[T] {
	m.getProfNo = getProfNo
	return m
}

func (m *MockTrackingStore[T]) SelectByIDProfile(id string, profileNo int) ([]T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]T, 0, len(m.rows))
	for i := range m.rows {
		if m.getID != nil && m.getID(&m.rows[i]) != id {
			continue
		}
		if m.getProfNo != nil && m.getProfNo(&m.rows[i]) != profileNo {
			continue
		}
		out = append(out, m.rows[i])
	}
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
		// Only delete a row when its uid is requested AND (if id scoping is on)
		// it is owned by id. A non-matching owner is preserved — this is the
		// ownership guard the SQL `WHERE id=? AND uid=?` enforces.
		matchUID := uidSet[m.getUID(&m.rows[i])]
		matchID := m.getID == nil || m.getID(&m.rows[i]) == id
		if matchUID && matchID {
			continue
		}
		kept = append(kept, m.rows[i])
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
