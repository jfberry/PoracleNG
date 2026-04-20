package store

import "testing"

// Compile-time check that MockHumanStore implements HumanStore.
var _ HumanStore = (*MockHumanStore)(nil)

func TestMockHumanStore_CreateAndGet(t *testing.T) {
	m := NewMockHumanStore()

	h := &Human{ID: "123", Type: "discord:user", Name: "Alice", Enabled: true}
	if err := m.Create(h); err != nil {
		t.Fatal(err)
	}

	got, err := m.Get("123")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected human, got nil")
	}
	if got.Name != "Alice" {
		t.Errorf("expected Alice, got %s", got.Name)
	}

	// Get returns a copy, not a reference to the stored record.
	got.Name = "Bob"
	got2, _ := m.Get("123")
	if got2.Name != "Alice" {
		t.Error("mock should return copies, not references")
	}
}

func TestMockHumanStore_GetNotFound(t *testing.T) {
	m := NewMockHumanStore()
	got, err := m.Get("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent human")
	}
}

func TestMockHumanStore_Delete(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", Name: "Alice"})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 1})

	if err := m.Delete("123"); err != nil {
		t.Fatal(err)
	}

	got, _ := m.Get("123")
	if got != nil {
		t.Error("expected nil after delete")
	}

	profiles, _ := m.GetProfiles("123")
	if len(profiles) != 0 {
		t.Error("expected profiles deleted")
	}
}

func TestMockHumanStore_SetEnabled(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", Enabled: false})

	m.SetEnabled("123", true)
	got, _ := m.Get("123")
	if !got.Enabled {
		t.Error("expected enabled=true")
	}
}

func TestMockHumanStore_SetLocation(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.SetLocation("123", 1, 51.5, -0.1)
	got, _ := m.Get("123")
	if got.Latitude != 51.5 || got.Longitude != -0.1 {
		t.Errorf("expected (51.5, -0.1), got (%f, %f)", got.Latitude, got.Longitude)
	}
}

func TestMockHumanStore_SetArea(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.SetArea("123", 1, []string{"downtown", "park"})
	got, _ := m.Get("123")
	if len(got.Area) != 2 {
		t.Fatalf("expected 2 areas, got %d", len(got.Area))
	}
}

func TestMockHumanStore_SetCommunity(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.SetCommunity("123", []string{"TeamA"}, []string{"zone1"})
	got, _ := m.Get("123")
	if len(got.CommunityMembership) != 1 || got.CommunityMembership[0] != "TeamA" {
		t.Errorf("expected TeamA community, got %v", got.CommunityMembership)
	}
	if len(got.AreaRestriction) != 1 || got.AreaRestriction[0] != "zone1" {
		t.Errorf("expected zone1 restriction, got %v", got.AreaRestriction)
	}
}

func TestMockHumanStore_ListByType(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "1", Type: "discord:user"})
	m.AddHuman(&Human{ID: "2", Type: "discord:user"})
	m.AddHuman(&Human{ID: "3", Type: "telegram:user"})

	list, _ := m.ListByType("discord:user")
	if len(list) != 2 {
		t.Fatalf("expected 2 discord users, got %d", len(list))
	}
}

func TestMockHumanStore_LookupWebhookByName(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "http://hook.url", Type: "webhook", Name: "MyHook"})

	id, _ := m.LookupWebhookByName("MyHook")
	if id != "http://hook.url" {
		t.Errorf("expected webhook URL, got %s", id)
	}

	id, _ = m.LookupWebhookByName("NonExistent")
	if id != "" {
		t.Error("expected empty for nonexistent webhook")
	}
}

func TestMockHumanStore_SwitchProfile(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123", CurrentProfileNo: 1, Area: []string{"a"}})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 1, Area: []string{"a"}})
	m.SeedProfile(Profile{ID: "123", ProfileNo: 2, Area: []string{"b", "c"}, Latitude: 10, Longitude: 20})

	found, _ := m.SwitchProfile("123", 2)
	if !found {
		t.Fatal("expected profile found")
	}
	got, _ := m.Get("123")
	if got.CurrentProfileNo != 2 {
		t.Errorf("expected profile 2, got %d", got.CurrentProfileNo)
	}
	if len(got.Area) != 2 {
		t.Errorf("expected 2 areas from profile 2, got %d", len(got.Area))
	}

	found, _ = m.SwitchProfile("123", 99)
	if found {
		t.Error("expected not found for nonexistent profile")
	}
}

func TestMockHumanStore_CallTracking(t *testing.T) {
	m := NewMockHumanStore()
	m.AddHuman(&Human{ID: "123"})

	m.Get("123")
	m.SetEnabled("123", true)
	m.SetLanguage("123", "de")

	if len(m.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(m.Calls))
	}
	if m.Calls[0] != "Get" || m.Calls[1] != "SetEnabled" || m.Calls[2] != "SetLanguage" {
		t.Errorf("unexpected call sequence: %v", m.Calls)
	}
}
