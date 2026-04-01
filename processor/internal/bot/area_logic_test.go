package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func testFences() []geofence.Fence {
	return []geofence.Fence{
		{Name: "Downtown", Group: "City", UserSelectable: true},
		{Name: "Uptown", Group: "City", UserSelectable: true},
		{Name: "Park", Group: "Nature", UserSelectable: true},
		{Name: "InternalZone", Group: "Admin", UserSelectable: false},
		{Name: "Harbor", Group: "Coast", UserSelectable: true},
	}
}

func noSecurityConfig() *config.Config {
	return &config.Config{
		Area: config.AreaConfig{Enabled: false},
	}
}

func securityConfig() *config.Config {
	return &config.Config{
		Area: config.AreaConfig{
			Enabled: true,
			Communities: []config.CommunityConfig{
				{
					Name:         "TeamCity",
					AllowedAreas: []string{"Downtown", "Uptown"},
				},
				{
					Name:         "TeamNature",
					AllowedAreas: []string{"Park", "Harbor"},
				},
			},
		},
	}
}

// --- GetAvailableAreas ---

func TestAreaGetAvailableAreas_NoSecurity(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	areas := al.GetAvailableAreas(nil)

	// Should return all user-selectable fences (4 of 5)
	if len(areas) != 4 {
		t.Fatalf("expected 4 available areas, got %d", len(areas))
	}
	names := make(map[string]bool)
	for _, a := range areas {
		names[a.Name] = true
	}
	if names["InternalZone"] {
		t.Error("non-selectable fence InternalZone should not be included")
	}
	for _, expected := range []string{"Downtown", "Uptown", "Park", "Harbor"} {
		if !names[expected] {
			t.Errorf("expected %s in available areas", expected)
		}
	}
}

func TestAreaGetAvailableAreas_WithSecurity(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	// TeamCity community should only see Downtown and Uptown
	areas := al.GetAvailableAreas([]string{"teamcity"})
	if len(areas) != 2 {
		t.Fatalf("expected 2 areas for TeamCity, got %d", len(areas))
	}
	names := make(map[string]bool)
	for _, a := range areas {
		names[a.Name] = true
	}
	if !names["Downtown"] || !names["Uptown"] {
		t.Errorf("expected Downtown and Uptown, got %v", names)
	}
}

func TestAreaGetAvailableAreas_WithSecurity_MultipleCommunities(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	areas := al.GetAvailableAreas([]string{"teamcity", "teamnature"})
	if len(areas) != 4 {
		t.Fatalf("expected 4 areas for both communities, got %d", len(areas))
	}
}

func TestAreaGetAvailableAreas_WithSecurity_NoCommunities(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	areas := al.GetAvailableAreas(nil)
	if len(areas) != 0 {
		t.Fatalf("expected 0 areas with no communities, got %d", len(areas))
	}
}

// --- GetAvailableAreasMarked ---

func TestAreaGetAvailableAreasMarked(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	areas := al.GetAvailableAreasMarked(nil, []string{"downtown", "park"})

	activeCount := 0
	for _, a := range areas {
		if a.IsActive {
			activeCount++
			if a.Name != "Downtown" && a.Name != "Park" {
				t.Errorf("unexpected active area: %s", a.Name)
			}
		}
	}
	if activeCount != 2 {
		t.Errorf("expected 2 active areas, got %d", activeCount)
	}
}

// --- AddAreas ---

func TestAreaAddAreas_Basic(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	added, notFound, newList := al.AddAreas(nil, nil, []string{"downtown", "park"})

	if len(added) != 2 {
		t.Fatalf("expected 2 added, got %d", len(added))
	}
	if len(notFound) != 0 {
		t.Fatalf("expected 0 notFound, got %d", len(notFound))
	}
	if len(newList) != 2 {
		t.Fatalf("expected 2 in newList, got %d", len(newList))
	}
}

func TestAreaAddAreas_DuplicatePrevention(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	added, _, newList := al.AddAreas([]string{"downtown"}, nil, []string{"downtown", "park"})

	// downtown already exists, only park should be added
	if len(added) != 1 {
		t.Fatalf("expected 1 added, got %d: %v", len(added), added)
	}
	if added[0] != "Park" {
		t.Errorf("expected Park to be added, got %s", added[0])
	}
	if len(newList) != 2 {
		t.Fatalf("expected 2 in newList, got %d", len(newList))
	}
}

func TestAreaAddAreas_InvalidArea(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	added, notFound, _ := al.AddAreas(nil, nil, []string{"nonexistent", "downtown"})

	if len(added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(added))
	}
	if len(notFound) != 1 {
		t.Fatalf("expected 1 notFound, got %d", len(notFound))
	}
	if notFound[0] != "nonexistent" {
		t.Errorf("expected notFound to contain nonexistent, got %s", notFound[0])
	}
}

func TestAreaAddAreas_WithSecurity(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())
	// TeamCity can only add Downtown and Uptown, not Park
	added, notFound, _ := al.AddAreas(nil, []string{"teamcity"}, []string{"downtown", "park"})

	if len(added) != 1 {
		t.Fatalf("expected 1 added, got %d: %v", len(added), added)
	}
	if added[0] != "Downtown" {
		t.Errorf("expected Downtown, got %s", added[0])
	}
	if len(notFound) != 1 {
		t.Fatalf("expected 1 notFound, got %d", len(notFound))
	}
}

// --- RemoveAreas ---

func TestAreaRemoveAreas_Basic(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	removed, remaining := al.RemoveAreas([]string{"downtown", "park", "harbor"}, []string{"park"})

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if removed[0] != "park" {
		t.Errorf("expected park removed, got %s", removed[0])
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(remaining))
	}
}

func TestAreaRemoveAreas_NotInList(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	removed, remaining := al.RemoveAreas([]string{"downtown", "park"}, []string{"harbor"})

	if len(removed) != 0 {
		t.Fatalf("expected 0 removed, got %d", len(removed))
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(remaining))
	}
}

// --- ResolveDisplayNames ---

func TestAreaResolveDisplayNames(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	names := al.ResolveDisplayNames([]string{"downtown", "PARK", "unknown"})

	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "Downtown" {
		t.Errorf("expected Downtown, got %s", names[0])
	}
	if names[1] != "Park" {
		t.Errorf("expected Park, got %s", names[1])
	}
	if names[2] != "unknown" {
		t.Errorf("expected unknown preserved, got %s", names[2])
	}
}

// --- FindFence ---

func TestAreaFindFence(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())

	f := al.FindFence("downtown")
	if f == nil {
		t.Fatal("expected to find Downtown fence")
	}
	if f.Name != "Downtown" {
		t.Errorf("expected Downtown, got %s", f.Name)
	}

	f = al.FindFence("nonexistent")
	if f != nil {
		t.Error("expected nil for nonexistent fence")
	}
}
