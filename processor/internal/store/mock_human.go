package store

import (
	"fmt"
	"sync"
)

// MockHumanStore is an in-memory HumanStore for testing. It records calls
// and returns configured responses.
type MockHumanStore struct {
	mu      sync.RWMutex
	humans  map[string]*Human   // id -> Human
	profiles map[string][]Profile // id -> profiles

	// Calls records method names called (for assertion).
	Calls []string
}

// NewMockHumanStore creates a new empty MockHumanStore.
func NewMockHumanStore() *MockHumanStore {
	return &MockHumanStore{
		humans:   make(map[string]*Human),
		profiles: make(map[string][]Profile),
	}
}

// AddHuman seeds the mock with a human record.
func (m *MockHumanStore) AddHuman(h *Human) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.humans[h.ID] = h
}

// SeedProfile seeds the mock with a profile record.
func (m *MockHumanStore) SeedProfile(p Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[p.ID] = append(m.profiles[p.ID], p)
}

func (m *MockHumanStore) record(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, method)
}

func (m *MockHumanStore) Get(id string) (*Human, error) {
	m.record("Get")
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.humans[id]
	if !ok {
		return nil, nil
	}
	cp := *h
	return &cp, nil
}

func (m *MockHumanStore) Create(h *Human) error {
	m.record("Create")
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.humans[h.ID]; exists {
		return fmt.Errorf("human %s already exists", h.ID)
	}
	cp := *h
	m.humans[h.ID] = &cp
	return nil
}

func (m *MockHumanStore) Delete(id string) error {
	m.record("Delete")
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.humans, id)
	delete(m.profiles, id)
	return nil
}

func (m *MockHumanStore) SetEnabled(id string, enabled bool) error {
	m.record("SetEnabled")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Enabled = enabled
	}
	return nil
}

func (m *MockHumanStore) SetEnabledWithFails(id string) error {
	m.record("SetEnabledWithFails")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Enabled = true
		h.Fails = 0
	}
	return nil
}

func (m *MockHumanStore) SetAdminDisable(id string, disable bool) error {
	m.record("SetAdminDisable")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.AdminDisable = disable
	}
	return nil
}

func (m *MockHumanStore) SetLocation(id string, profileNo int, lat, lon float64) error {
	m.record("SetLocation")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Latitude = lat
		h.Longitude = lon
	}
	return nil
}

func (m *MockHumanStore) SetArea(id string, profileNo int, areas []string) error {
	m.record("SetArea")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Area = areas
	}
	return nil
}

func (m *MockHumanStore) SetLanguage(id string, lang string) error {
	m.record("SetLanguage")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Language = lang
	}
	return nil
}

func (m *MockHumanStore) SetCommunity(id string, communities []string, restrictions []string) error {
	m.record("SetCommunity")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.CommunityMembership = communities
		h.AreaRestriction = restrictions
	}
	return nil
}

func (m *MockHumanStore) SetBlockedAlerts(id string, alerts []string) error {
	m.record("SetBlockedAlerts")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.BlockedAlerts = alerts
	}
	return nil
}

func (m *MockHumanStore) SetName(id string, name string) error {
	m.record("SetName")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		h.Name = name
	}
	return nil
}

func (m *MockHumanStore) Update(id string, fields map[string]any) error {
	m.record("Update")
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.humans[id]; ok {
		for k, v := range fields {
			switch k {
			case "enabled":
				if vi, ok := v.(int); ok {
					h.Enabled = vi != 0
				}
			case "admin_disable":
				if vi, ok := v.(int); ok {
					h.AdminDisable = vi != 0
				}
			case "fails":
				if vi, ok := v.(int); ok {
					h.Fails = vi
				}
			case "name":
				if vs, ok := v.(string); ok {
					h.Name = vs
				}
			}
		}
	}
	return nil
}

func (m *MockHumanStore) ListByType(typ string) ([]*Human, error) {
	m.record("ListByType")
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Human
	for _, h := range m.humans {
		if h.Type == typ {
			cp := *h
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockHumanStore) ListByTypeEnabled(typ string) ([]*Human, error) {
	m.record("ListByTypeEnabled")
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Human
	for _, h := range m.humans {
		if h.Type == typ && !h.AdminDisable {
			cp := *h
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockHumanStore) ListByTypes(types []string) ([]*Human, error) {
	m.record("ListByTypes")
	m.mu.RLock()
	defer m.mu.RUnlock()
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	var result []*Human
	for _, h := range m.humans {
		if typeSet[h.Type] && !h.AdminDisable {
			cp := *h
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockHumanStore) ListAll() ([]*Human, error) {
	m.record("ListAll")
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Human, 0, len(m.humans))
	for _, h := range m.humans {
		cp := *h
		result = append(result, &cp)
	}
	return result, nil
}

func (m *MockHumanStore) LookupWebhookByName(name string) (string, error) {
	m.record("LookupWebhookByName")
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.humans {
		if h.Type == "webhook" && h.Name == name {
			return h.ID, nil
		}
	}
	return "", nil
}

func (m *MockHumanStore) CountByName(name string) (int, error) {
	m.record("CountByName")
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, h := range m.humans {
		if h.Name == name {
			count++
		}
	}
	return count, nil
}

func (m *MockHumanStore) GetProfiles(id string) ([]Profile, error) {
	m.record("GetProfiles")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[id], nil
}

func (m *MockHumanStore) SwitchProfile(id string, profileNo int) (bool, error) {
	m.record("SwitchProfile")
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles := m.profiles[id]
	for _, p := range profiles {
		if p.ProfileNo == profileNo {
			if h, ok := m.humans[id]; ok {
				h.CurrentProfileNo = profileNo
				h.Area = p.Area
				h.Latitude = p.Latitude
				h.Longitude = p.Longitude
			}
			return true, nil
		}
	}
	return false, nil
}

func (m *MockHumanStore) AddProfile(id string, name string, activeHours string) error {
	m.record("AddProfile")
	m.mu.Lock()
	defer m.mu.Unlock()
	newNo := 1
	for _, p := range m.profiles[id] {
		if p.ProfileNo >= newNo {
			newNo = p.ProfileNo + 1
		}
	}
	m.profiles[id] = append(m.profiles[id], Profile{
		ID:          id,
		ProfileNo:   newNo,
		Name:        name,
		ActiveHours: activeHours,
	})
	return nil
}

func (m *MockHumanStore) DeleteProfile(id string, profileNo int) error {
	m.record("DeleteProfile")
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles := m.profiles[id]
	var remaining []Profile
	for _, p := range profiles {
		if p.ProfileNo != profileNo {
			remaining = append(remaining, p)
		}
	}
	m.profiles[id] = remaining
	return nil
}

func (m *MockHumanStore) CopyProfile(id string, fromProfile, toProfile int) error {
	m.record("CopyProfile")
	return nil
}

func (m *MockHumanStore) CreateDefaultProfile(id, name string, areas []string, lat, lon float64) error {
	m.record("CreateDefaultProfile")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[id] = append(m.profiles[id], Profile{
		ID:        id,
		ProfileNo: 1,
		Name:      name,
		Area:      areas,
		Latitude:  lat,
		Longitude: lon,
	})
	return nil
}

func (m *MockHumanStore) UpdateProfileHours(id string, profileNo int, activeHours string) error {
	m.record("UpdateProfileHours")
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.profiles[id] {
		if p.ProfileNo == profileNo {
			m.profiles[id][i].ActiveHours = activeHours
			break
		}
	}
	return nil
}
