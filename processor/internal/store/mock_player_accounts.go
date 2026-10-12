package store

import (
	"strings"
	"sync"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

// MockPlayerAccountStore is an in-memory PlayerAccountStore for tests.
type MockPlayerAccountStore struct {
	mu      sync.Mutex
	nextUID int64
	rows    map[string][]db.PlayerAccount
}

func NewMockPlayerAccountStore() *MockPlayerAccountStore {
	return &MockPlayerAccountStore{rows: make(map[string][]db.PlayerAccount)}
}

func (m *MockPlayerAccountStore) List(humanID string) ([]db.PlayerAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]db.PlayerAccount(nil), m.rows[humanID]...), nil
}

func (m *MockPlayerAccountStore) Add(humanID, playerID, label string) error {
	if err := ValidatePlayerID(playerID); err != nil {
		return err
	}
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows[humanID] {
		if a.PlayerID == playerID {
			return ErrDuplicateAccount
		}
		if label != "" && strings.EqualFold(a.Label, label) {
			return ErrDuplicateAccountLabel
		}
	}
	if len(m.rows[humanID]) >= personalshiny.MaxPlayers {
		return ErrTooManyAccounts
	}
	m.nextUID++
	m.rows[humanID] = append(m.rows[humanID], db.PlayerAccount{
		UID: m.nextUID, ID: humanID, PlayerID: playerID, Label: label, CreatedAt: time.Now(),
	})
	return nil
}

func (m *MockPlayerAccountStore) Remove(humanID, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.rows[humanID]
	for i, a := range list {
		if a.PlayerID == playerID {
			m.rows[humanID] = append(list[:i:i], list[i+1:]...)
			return nil
		}
	}
	return ErrAccountNotFound
}

func (m *MockPlayerAccountStore) SetLabel(humanID, playerID, label string) error {
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, a := range m.rows[humanID] {
		if a.PlayerID == playerID {
			idx = i
		} else if label != "" && strings.EqualFold(a.Label, label) {
			return ErrDuplicateAccountLabel
		}
	}
	if idx < 0 {
		return ErrAccountNotFound
	}
	m.rows[humanID][idx].Label = label
	return nil
}

func (m *MockPlayerAccountStore) Resolve(humanID, ref string) (*db.PlayerAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows[humanID] {
		if a.PlayerID == ref {
			return &a, nil
		}
	}
	for _, a := range m.rows[humanID] {
		if a.Label != "" && strings.EqualFold(a.Label, ref) {
			return &a, nil
		}
	}
	return nil, nil
}
