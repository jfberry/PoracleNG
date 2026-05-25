package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func newRule(uid int64, distance int, label string, areas []string) trackingUserData {
	return trackingUserData{
		HumanID:               "u1",
		UID:                   uid,
		Distance:              distance,
		OverrideLocationLabel: label,
		OverrideAreas:         areas,
	}
}

func newHuman(lat, lon float64, areas []string, locs map[string]*db.UserLocation) *db.Human {
	return &db.Human{
		ID: "u1", Enabled: true,
		Latitude: lat, Longitude: lon,
		Area:      areas,
		Locations: locs,
	}
}

func TestOverride_LocationAnchorsDistance(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, nil, map[string]*db.UserLocation{
		"home": {Label: "Home", Latitude: 51.5, Longitude: -0.1},
	})}
	rules := []trackingUserData{newRule(1, 500, "Home", nil)}
	// Event at (51.501, -0.1) is ~111m from Home — within 500m
	out := ValidateHumansGeneric(rules, 51.501, -0.1, nil, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("rule with location override should fire; got %d", len(out))
	}
}

func TestOverride_AreasReplaceHumanAreas(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, []string{"london"}, nil)}
	rules := []trackingUserData{newRule(1, 0, "", []string{"berlin"})}
	// Event is in "berlin" only. Without override, human.Area=london wouldn't match.
	out := ValidateHumansGeneric(rules, 52.5, 13.4, map[string]bool{"berlin": true}, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("override_areas should replace human.Area; got %d", len(out))
	}
}

func TestOverride_OrphanedLabelFallsThrough(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(51.5, -0.1, nil, nil)}
	rules := []trackingUserData{newRule(1, 500, "Home", nil)} // Home not in user.Locations
	// Falls through to human (51.5, -0.1); event close by → still fires
	out := ValidateHumansGeneric(rules, 51.501, -0.1, nil, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("orphaned override should silently fall through; got %d", len(out))
	}
}

func TestOverride_AreasFireWithoutHumanAreas(t *testing.T) {
	humans := map[string]*db.Human{"u1": newHuman(0, 0, nil, nil)} // no human areas
	rules := []trackingUserData{newRule(1, 0, "", []string{"berlin"})}
	out := ValidateHumansGeneric(rules, 52.5, 13.4, map[string]bool{"berlin": true}, false, humans, "test")
	if len(out) != 1 {
		t.Fatalf("override_areas alone should be enough; got %d", len(out))
	}
}
