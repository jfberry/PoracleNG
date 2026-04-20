package community

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

var testCommunities = []config.CommunityConfig{
	{
		Name:          "NewYork",
		AllowedAreas:  []string{"Manhattan", "Bronx", "Brooklyn"},
		LocationFence: []string{"WholeNewYork"},
		Telegram:      struct {
			Channels []string `toml:"channels" json:"channels"`
			Admins   []string `toml:"admins" json:"admins"`
		}{Admins: []string{"111", "222"}},
	},
	{
		Name:          "Chicago",
		AllowedAreas:  []string{"NorthSide", "SouthSide"},
		LocationFence: []string{"WholeChicago", "GreaterChicago"},
		Telegram:      struct {
			Channels []string `toml:"channels" json:"channels"`
			Admins   []string `toml:"admins" json:"admins"`
		}{Admins: []string{"333"}},
	},
	{
		Name:          "Boston",
		AllowedAreas:  []string{"Downtown", "Cambridge"},
		LocationFence: []string{"WholeBoston"},
	},
}

func TestFilterAreas_SingleCommunity(t *testing.T) {
	areas := []string{"manhattan", "bronx", "queens", "northside"}
	result := FilterAreas(testCommunities, []string{"newyork"}, areas)

	if len(result) != 2 {
		t.Fatalf("expected 2 areas, got %d: %v", len(result), result)
	}
	if result[0] != "manhattan" || result[1] != "bronx" {
		t.Errorf("expected [manhattan, bronx], got %v", result)
	}
}

func TestFilterAreas_MultipleCommunities(t *testing.T) {
	areas := []string{"manhattan", "northside", "downtown", "queens"}
	result := FilterAreas(testCommunities, []string{"newyork", "chicago"}, areas)

	if len(result) != 2 {
		t.Fatalf("expected 2 areas, got %d: %v", len(result), result)
	}
	// manhattan from newyork, northside from chicago
	if result[0] != "manhattan" || result[1] != "northside" {
		t.Errorf("expected [manhattan, northside], got %v", result)
	}
}

func TestFilterAreas_NoCommunity(t *testing.T) {
	areas := []string{"manhattan", "bronx"}
	result := FilterAreas(testCommunities, []string{}, areas)

	if len(result) != 0 {
		t.Errorf("expected 0 areas for no community, got %v", result)
	}
}

func TestFilterAreas_UnknownCommunity(t *testing.T) {
	areas := []string{"manhattan"}
	result := FilterAreas(testCommunities, []string{"london"}, areas)

	if len(result) != 0 {
		t.Errorf("expected 0 areas for unknown community, got %v", result)
	}
}

func TestFilterAreas_CaseInsensitive(t *testing.T) {
	areas := []string{"Manhattan", "BRONX", "queens"}
	result := FilterAreas(testCommunities, []string{"NEWYORK"}, areas)

	if len(result) != 2 {
		t.Fatalf("expected 2 areas, got %d: %v", len(result), result)
	}
}

func TestCalculateLocationRestrictions_Single(t *testing.T) {
	result := CalculateLocationRestrictions(testCommunities, []string{"newyork"})

	if len(result) != 1 || result[0] != "wholenewyork" {
		t.Errorf("expected [wholenewyork], got %v", result)
	}
}

func TestCalculateLocationRestrictions_Multiple(t *testing.T) {
	result := CalculateLocationRestrictions(testCommunities, []string{"newyork", "chicago"})

	if len(result) != 3 {
		t.Fatalf("expected 3 fences, got %d: %v", len(result), result)
	}
	// wholenewyork, wholechicago, greaterchicago
	expected := map[string]bool{"wholenewyork": true, "wholechicago": true, "greaterchicago": true}
	for _, r := range result {
		if !expected[r] {
			t.Errorf("unexpected fence %q in result %v", r, result)
		}
	}
}

func TestCalculateLocationRestrictions_NoDuplicates(t *testing.T) {
	// Both newyork and newyork — should not duplicate
	result := CalculateLocationRestrictions(testCommunities, []string{"newyork", "newyork"})

	if len(result) != 1 {
		t.Errorf("expected 1 fence (no duplicates), got %v", result)
	}
}

func TestCalculateLocationRestrictions_Empty(t *testing.T) {
	result := CalculateLocationRestrictions(testCommunities, []string{})

	if len(result) != 0 {
		t.Errorf("expected 0 fences, got %v", result)
	}
}

func TestAddCommunity_New(t *testing.T) {
	result := AddCommunity(testCommunities, []string{"newyork"}, "chicago")

	if len(result) != 2 {
		t.Fatalf("expected 2 communities, got %v", result)
	}
	if result[0] != "chicago" || result[1] != "newyork" {
		t.Errorf("expected sorted [chicago, newyork], got %v", result)
	}
}

func TestAddCommunity_AlreadyPresent(t *testing.T) {
	result := AddCommunity(testCommunities, []string{"newyork"}, "newyork")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork], got %v", result)
	}
}

func TestAddCommunity_Invalid(t *testing.T) {
	result := AddCommunity(testCommunities, []string{"newyork"}, "london")

	// london doesn't exist in config, should be filtered out
	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork] (london invalid), got %v", result)
	}
}

func TestAddCommunity_CaseInsensitive(t *testing.T) {
	result := AddCommunity(testCommunities, []string{}, "NewYork")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork], got %v", result)
	}
}

func TestRemoveCommunity(t *testing.T) {
	result := RemoveCommunity(testCommunities, []string{"chicago", "newyork"}, "newyork")

	if len(result) != 1 || result[0] != "chicago" {
		t.Errorf("expected [chicago], got %v", result)
	}
}

func TestRemoveCommunity_NotPresent(t *testing.T) {
	result := RemoveCommunity(testCommunities, []string{"newyork"}, "chicago")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork], got %v", result)
	}
}

func TestRemoveCommunity_FiltersInvalid(t *testing.T) {
	// If existing has an invalid community, it gets cleaned out
	result := RemoveCommunity(testCommunities, []string{"newyork", "london"}, "london")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork] (london invalid), got %v", result)
	}
}

func TestIsTelegramCommunityAdmin_IsAdmin(t *testing.T) {
	result := IsTelegramCommunityAdmin(testCommunities, "111")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork], got %v", result)
	}
}

func TestIsTelegramCommunityAdmin_MultipleAdmin(t *testing.T) {
	// 222 is admin of newyork only
	result := IsTelegramCommunityAdmin(testCommunities, "222")

	if len(result) != 1 || result[0] != "newyork" {
		t.Errorf("expected [newyork], got %v", result)
	}
}

func TestIsTelegramCommunityAdmin_ChicagoAdmin(t *testing.T) {
	result := IsTelegramCommunityAdmin(testCommunities, "333")

	if len(result) != 1 || result[0] != "chicago" {
		t.Errorf("expected [chicago], got %v", result)
	}
}

func TestIsTelegramCommunityAdmin_NotAdmin(t *testing.T) {
	result := IsTelegramCommunityAdmin(testCommunities, "999")

	if result != nil {
		t.Errorf("expected nil for non-admin, got %v", result)
	}
}

func TestIsTelegramCommunityAdmin_NoTelegramConfig(t *testing.T) {
	// Boston has no telegram admins
	result := IsTelegramCommunityAdmin(testCommunities, "111")

	// 111 is only admin of newyork, not boston
	for _, r := range result {
		if r == "boston" {
			t.Error("111 should not be admin of boston")
		}
	}
}
