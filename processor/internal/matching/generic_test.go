package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func TestValidateHumansGenericBasic(t *testing.T) {
	human := makeHuman("user1")
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 1 {
		t.Errorf("Expected 1, got %d", len(result))
	}
}

func TestValidateHumansGenericDisabled(t *testing.T) {
	human := makeHuman("user1")
	human.Enabled = false
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 for disabled human, got %d", len(result))
	}
}

func TestValidateHumansGenericAdminDisable(t *testing.T) {
	human := makeHuman("user1")
	human.AdminDisable = true
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 for admin-disabled human, got %d", len(result))
	}
}

func TestValidateHumansGenericWrongProfile(t *testing.T) {
	human := makeHuman("user1")
	human.CurrentProfileNo = 2
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 for wrong profile, got %d", len(result))
	}
}

func TestValidateHumansGenericDedup(t *testing.T) {
	human := makeHuman("user1")
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
		{HumanID: "user1", ProfileNo: 1, Template: "2"}, // same user
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 (deduped), got %d", len(result))
	}
}

func TestValidateHumansGenericStrictAreaRestriction(t *testing.T) {
	human := makeHuman("user1")
	human.AreaRestriction = []string{"restricted_zone"}
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	// Area matches but restriction doesn't
	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		true, humans, "lure",
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 for unmet area restriction, got %d", len(result))
	}

	// Both match
	result = ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea", "restricted_zone"},
		true, humans, "lure",
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 for met area restriction, got %d", len(result))
	}
}

func TestValidateHumansGenericDistanceCheck(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.5
	human.Longitude = 0.0
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Distance: 500, Template: "1"},
	}

	// Within distance
	result := ValidateHumansGeneric(
		trackings, 51.5001, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 for within distance, got %d", len(result))
	}

	// Too far
	result = ValidateHumansGeneric(
		trackings, 51.6, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 for out of distance, got %d", len(result))
	}
}

func TestValidateHumansGenericMultipleUsers(t *testing.T) {
	human1 := makeHuman("user1")
	human2 := makeHuman("user2")
	human2.Area = []string{"testarea"}
	humans := map[string]*db.Human{"user1": human1, "user2": human2}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
		{HumanID: "user2", ProfileNo: 1, Template: "1"},
	}

	result := ValidateHumansGeneric(
		trackings, 51.0, 0.0,
		[]string{"testarea"},
		false, humans, "lure",
	)

	if len(result) != 2 {
		t.Errorf("Expected 2, got %d", len(result))
	}
}

func TestAreaOverlap(t *testing.T) {
	tests := []struct {
		name     string
		human    []string
		matched  []string
		expected bool
	}{
		{"match", []string{"london"}, []string{"london", "paris"}, true},
		{"no match", []string{"berlin"}, []string{"london", "paris"}, false},
		{"exact match required", []string{"London"}, []string{"london"}, false},
		{"empty human areas", nil, []string{"london"}, false},
		{"empty matched areas", []string{"london"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := areaOverlap(tt.human, tt.matched)
			if got != tt.expected {
				t.Errorf("areaOverlap(%v, %v) = %v, want %v",
					tt.human, tt.matched, got, tt.expected)
			}
		})
	}
}

func TestNilState(t *testing.T) {
	// All matchers should handle nil state gracefully
	gymMatcher := &GymMatcher{}
	if result := gymMatcher.Match(&GymData{}, nil); result != nil {
		t.Errorf("GymMatcher with nil state should return nil")
	}

	invMatcher := &InvasionMatcher{}
	if result := invMatcher.Match(&InvasionData{}, nil); result != nil {
		t.Errorf("InvasionMatcher with nil state should return nil")
	}

	lureMatcher := &LureMatcher{}
	if result := lureMatcher.Match(&LureData{}, nil); result != nil {
		t.Errorf("LureMatcher with nil state should return nil")
	}

	nestMatcher := &NestMatcher{}
	if result := nestMatcher.Match(&NestData{}, nil); result != nil {
		t.Errorf("NestMatcher with nil state should return nil")
	}

	questMatcher := &QuestMatcher{}
	if result := questMatcher.Match(&QuestData{}, nil); result != nil {
		t.Errorf("QuestMatcher with nil state should return nil")
	}

	fortMatcher := &FortMatcher{}
	if result := fortMatcher.Match(&FortData{}, nil); result != nil {
		t.Errorf("FortMatcher with nil state should return nil")
	}
}

// Test with a more complex geofence to verify area matching works
func TestValidateHumansGenericMultipleAreas(t *testing.T) {
	fences := []geofence.Fence{
		{
			Name:             "AreaA",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0}, {52.0, -1.0}, {52.0, 0.0}, {50.0, 0.0},
			},
		},
		{
			Name:             "AreaB",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, 0.0}, {52.0, 0.0}, {52.0, 1.0}, {50.0, 1.0},
			},
		},
	}
	_ = geofence.NewSpatialIndex(fences)

	human := makeHuman("user1")
	human.Area = []string{"areab"} // only in area B
	humans := map[string]*db.Human{"user1": human}

	trackings := []trackingUserData{
		{HumanID: "user1", ProfileNo: 1, Template: "1"},
	}

	// Point in Area A only
	result := ValidateHumansGeneric(
		trackings, 51.0, -0.5,
		[]string{"areaa"},
		false, humans, "lure",
	)
	if len(result) != 0 {
		t.Errorf("Expected 0 for area mismatch, got %d", len(result))
	}

	// Point in Area B
	result = ValidateHumansGeneric(
		trackings, 51.0, 0.5,
		[]string{"areab"},
		false, humans, "lure",
	)
	if len(result) != 1 {
		t.Errorf("Expected 1 for matching area, got %d", len(result))
	}
}
