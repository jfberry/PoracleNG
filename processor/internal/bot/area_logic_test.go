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
	areas := al.GetAvailableAreas(nil, false)

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
	areas := al.GetAvailableAreas([]string{"teamcity"}, false)
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

	areas := al.GetAvailableAreas([]string{"teamcity", "teamnature"}, false)
	if len(areas) != 4 {
		t.Fatalf("expected 4 areas for both communities, got %d", len(areas))
	}
}

func TestAreaGetAvailableAreas_WithSecurity_NoCommunities(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	areas := al.GetAvailableAreas(nil, false)
	if len(areas) != 0 {
		t.Fatalf("expected 0 areas with no communities, got %d", len(areas))
	}
}

// Admins bypass area_security: even with no community membership and area
// security enabled they see every fence — including the InternalZone fence
// that has UserSelectable=false. Mirrors PoracleJS area.js where
// isFromAdmin / targetIsAdmin skip both the userSelectable filter and the
// community-allowed-areas filter.
func TestAreaGetAvailableAreas_AdminBypass(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	areas := al.GetAvailableAreas(nil, true)
	if len(areas) != 5 {
		t.Fatalf("admin should see all 5 fences, got %d", len(areas))
	}
	// Confirm the non-user-selectable InternalZone is included for admin.
	var found bool
	for _, a := range areas {
		if a.Name == "InternalZone" {
			found = true
			break
		}
	}
	if !found {
		t.Error("admin bypass should include UserSelectable=false fences")
	}
}

// Admin can add an area that's not in any community's allowed set.
func TestAreaAddAreas_AdminBypassesCommunity(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	// As a non-admin with no community: park is not allowed.
	added, notFound, _ := al.AddAreas(nil, nil, []string{"park"}, false)
	if len(added) != 0 || len(notFound) != 1 {
		t.Fatalf("non-admin should be blocked: added=%v notFound=%v", added, notFound)
	}

	// As an admin with no community: park is allowed.
	added, notFound, _ = al.AddAreas(nil, nil, []string{"park"}, true)
	if len(added) != 1 || len(notFound) != 0 {
		t.Fatalf("admin should bypass: added=%v notFound=%v", added, notFound)
	}
}

// --- GetAvailableAreasMarked ---

func TestAreaGetAvailableAreasMarked(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	areas := al.GetAvailableAreasMarked(nil, []string{"downtown", "park"}, false)

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
	added, notFound, newList := al.AddAreas(nil, nil, []string{"downtown", "park"}, false)

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
	added, _, newList := al.AddAreas([]string{"downtown"}, nil, []string{"downtown", "park"}, false)

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
	added, notFound, _ := al.AddAreas(nil, nil, []string{"nonexistent", "downtown"}, false)

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
	added, notFound, _ := al.AddAreas(nil, []string{"teamcity"}, []string{"downtown", "park"}, false)

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

// Areas whose names contain underscores must be findable via either form,
// because the bot parser converts unquoted underscores to spaces. Mirrors
// PoracleJS behaviour and lets `!area add gent_centrum` (typed manually,
// unquoted) reach an area genuinely named "gent_centrum".
func TestAreaAddAreas_UnderscoreInName(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "City", UserSelectable: true},
	}
	al := NewAreaLogic(fences, noSecurityConfig())

	// Unquoted user input: parser strips underscore → "gent centrum".
	added, notFound, newList := al.AddAreas(nil, nil, []string{"gent centrum"}, false)
	if len(notFound) != 0 {
		t.Fatalf("space-form lookup should find the area, got notFound=%v", notFound)
	}
	if len(added) != 1 || added[0] != "Gent_centrum" {
		t.Fatalf("expected Gent_centrum added, got %v", added)
	}
	// Stored canonical lowercase form preserves the underscore.
	if len(newList) != 1 || newList[0] != "gent_centrum" {
		t.Errorf("expected stored as gent_centrum, got %v", newList)
	}

	// Quoted user input: parser keeps underscore → direct hit.
	added, notFound, _ = al.AddAreas(nil, nil, []string{"gent_centrum"}, false)
	if len(notFound) != 0 || len(added) != 1 {
		t.Errorf("underscore-form lookup should also work, got added=%v notFound=%v", added, notFound)
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

// Removing an area stored as "gent_centrum" should succeed whether the user
// typed it unquoted (parser → "gent centrum") or quoted (preserved as
// "gent_centrum").
func TestAreaRemoveAreas_UnderscoreInName(t *testing.T) {
	al := NewAreaLogic(nil, noSecurityConfig())

	// Unquoted form
	removed, remaining := al.RemoveAreas([]string{"gent_centrum", "downtown"}, []string{"gent centrum"})
	if len(removed) != 1 || removed[0] != "gent_centrum" {
		t.Errorf("space-form remove failed: removed=%v", removed)
	}
	if len(remaining) != 1 || remaining[0] != "downtown" {
		t.Errorf("expected only downtown remaining, got %v", remaining)
	}

	// Quoted form
	removed, _ = al.RemoveAreas([]string{"gent_centrum"}, []string{"gent_centrum"})
	if len(removed) != 1 {
		t.Errorf("underscore-form remove failed: removed=%v", removed)
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

// --- ValidateAndPrune ---

func TestAreaValidateAndPrune_NoSecurity(t *testing.T) {
	al := NewAreaLogic(testFences(), noSecurityConfig())
	valid, removed := al.ValidateAndPrune([]string{"downtown", "park", "harbor"}, nil)

	// With area security disabled, all areas are valid
	if len(valid) != 3 {
		t.Fatalf("expected 3 valid, got %d", len(valid))
	}
	if len(removed) != 0 {
		t.Fatalf("expected 0 removed, got %d", len(removed))
	}
}

func TestAreaValidateAndPrune_WithSecurity(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	// TeamCity allows Downtown and Uptown only
	valid, removed := al.ValidateAndPrune([]string{"downtown", "park", "harbor"}, []string{"teamcity"})

	if len(valid) != 1 {
		t.Fatalf("expected 1 valid, got %d: %v", len(valid), valid)
	}
	if valid[0] != "downtown" {
		t.Errorf("expected downtown valid, got %s", valid[0])
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d: %v", len(removed), removed)
	}
}

func TestAreaValidateAndPrune_NoCommunities(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	// No communities = no allowed areas = all removed
	valid, removed := al.ValidateAndPrune([]string{"downtown", "park"}, nil)

	if len(valid) != 0 {
		t.Fatalf("expected 0 valid, got %d", len(valid))
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
}

func TestAreaValidateAndPrune_MultipleCommunities(t *testing.T) {
	al := NewAreaLogic(testFences(), securityConfig())

	// Both communities = Downtown, Uptown, Park, Harbor all allowed
	valid, removed := al.ValidateAndPrune(
		[]string{"downtown", "park", "harbor", "unknown"},
		[]string{"teamcity", "teamnature"},
	)

	if len(valid) != 3 {
		t.Fatalf("expected 3 valid, got %d: %v", len(valid), valid)
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d: %v", len(removed), removed)
	}
	if removed[0] != "unknown" {
		t.Errorf("expected unknown removed, got %s", removed[0])
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

func TestAreaFindFence_UnderscoreInName(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "City", UserSelectable: true},
	}
	al := NewAreaLogic(fences, noSecurityConfig())

	// Space form (unquoted user input after parser)
	if f := al.FindFence("gent centrum"); f == nil || f.Name != "Gent_centrum" {
		t.Errorf("space-form FindFence failed: %+v", f)
	}
	// Underscore form (quoted, or fence config typed verbatim)
	if f := al.FindFence("gent_centrum"); f == nil || f.Name != "Gent_centrum" {
		t.Errorf("underscore-form FindFence failed: %+v", f)
	}
}
