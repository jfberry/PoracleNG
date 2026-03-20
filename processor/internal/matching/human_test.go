package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestValidateHumansAreaMatch(t *testing.T) {
	human := &db.Human{
		ID: "user1", Name: "Test", Type: "discord:user",
		Enabled: true, Area: []string{"london", "uk"},
		CurrentProfileNo: 1,
	}

	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, Template: "1",
	}

	result := ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.5, -0.1,
		map[string]bool{"london": true, "paris": true},
		false,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 match, got %d", len(result))
	}
}

func TestValidateHumansNoAreaMatch(t *testing.T) {
	human := &db.Human{
		ID: "user1", Name: "Test", Type: "discord:user",
		Enabled: true, Area: []string{"berlin"},
		CurrentProfileNo: 1,
	}

	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, Template: "1",
	}

	result := ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.5, -0.1,
		map[string]bool{"london": true, "paris": true},
		false,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 matches, got %d", len(result))
	}
}

func TestValidateHumansStrictAreaRestriction(t *testing.T) {
	human := &db.Human{
		ID: "user1", Name: "Test", Type: "discord:user",
		Enabled: true, Area: []string{"london"},
		AreaRestriction:  []string{"central london"},
		CurrentProfileNo: 1,
	}

	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, Template: "1",
	}

	// Area matches but restriction doesn't
	result := ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.5, -0.1,
		map[string]bool{"london": true},
		true,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 matches (strict area restriction), got %d", len(result))
	}

	// Both match
	result = ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.5, -0.1,
		map[string]bool{"london": true, "central london": true},
		true,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 match, got %d", len(result))
	}
}

func TestValidateHumansDistance(t *testing.T) {
	human := &db.Human{
		ID: "user1", Name: "Test", Type: "discord:user",
		Enabled: true, Area: []string{"london"},
		Latitude: 51.5, Longitude: -0.1,
		CurrentProfileNo: 1,
	}

	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, Distance: 500, Template: "1",
	}

	// Within range
	result := ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.5001, -0.1,
		map[string]bool{"london": true},
		false,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 1 {
		t.Errorf("Expected 1 match (within distance), got %d", len(result))
	}

	// Out of range
	result = ValidateHumans(
		[]*db.MonsterTracking{monster},
		51.51, -0.1,
		map[string]bool{"london": true},
		false,
		map[string]*db.Human{"user1": human},
	)

	if len(result) != 0 {
		t.Errorf("Expected 0 matches (out of distance), got %d", len(result))
	}
}
