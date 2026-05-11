package state

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// Helper to build a Human with the fields the index reads.
func mkHuman(id string, areas []string, restriction []string, lat, lon float64) *db.Human {
	return &db.Human{
		ID:              id,
		Enabled:         true,
		Area:            areas,
		AreaRestriction: restriction,
		Latitude:        lat,
		Longitude:       lon,
	}
}

func TestHumanGeoIndex_AreaOnly(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": mkHuman("u1", []string{"belgium", "antwerp"}, nil, 0, 0),
		"u2": mkHuman("u2", []string{"belgium"}, nil, 0, 0),
		"u3": mkHuman("u3", []string{"japan"}, nil, 0, 0),
	}
	idx := BuildHumanGeoIndex(humans, nil)

	got := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, false)
	if !got["u1"] || !got["u2"] {
		t.Errorf("expected u1,u2 applicable for belgium spawn, got %v", keysOf(got))
	}
	if got["u3"] {
		t.Errorf("u3 should not be applicable for belgium spawn")
	}
}

func TestHumanGeoIndex_MultipleMatchedAreas(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": mkHuman("u1", []string{"belgium"}, nil, 0, 0),
		"u2": mkHuman("u2", []string{"antwerp"}, nil, 0, 0),
		"u3": mkHuman("u3", []string{"belgium", "antwerp"}, nil, 0, 0),
	}
	idx := BuildHumanGeoIndex(humans, nil)
	got := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true, "antwerp": true}, false)
	if len(got) != 3 {
		t.Errorf("expected 3 applicable humans across both matched areas, got %v", keysOf(got))
	}
}

func TestHumanGeoIndex_DistanceOnly(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": mkHuman("u1", nil, nil, 1.0, 1.0),
	}
	perHumanMaxDist := map[string]int{"u1": 5000}
	idx := BuildHumanGeoIndex(humans, perHumanMaxDist)

	near := idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{}, false)
	if !near["u1"] {
		t.Errorf("u1 should be applicable for nearby spawn, got %v", keysOf(near))
	}
	far := idx.ApplicableHumans(10, 10, map[string]bool{}, false)
	if far["u1"] {
		t.Errorf("u1 should not be applicable for far spawn")
	}
}

func TestHumanGeoIndex_AreaPlusDistanceUnion(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": mkHuman("u1", []string{"belgium"}, nil, 1.0, 1.0),
	}
	perHumanMaxDist := map[string]int{"u1": 5000}
	idx := BuildHumanGeoIndex(humans, perHumanMaxDist)

	out := idx.ApplicableHumans(50, 50, map[string]bool{"belgium": true}, false)
	if !out["u1"] {
		t.Errorf("u1 area-applicable case: got %v", keysOf(out))
	}

	out = idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{"japan": true}, false)
	if !out["u1"] {
		t.Errorf("u1 distance-applicable case: got %v", keysOf(out))
	}

	out = idx.ApplicableHumans(50, 50, map[string]bool{"japan": true}, false)
	if out["u1"] {
		t.Errorf("u1 should NOT be applicable when out of area and out of distance")
	}
}

func TestHumanGeoIndex_StrictAreaRestriction(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": mkHuman("u1", []string{"belgium", "antwerp"}, []string{"belgium"}, 0, 0),
	}
	idx := BuildHumanGeoIndex(humans, nil)

	out := idx.ApplicableHumans(0, 0, map[string]bool{"antwerp": true}, false)
	if !out["u1"] {
		t.Errorf("strict off, antwerp spawn: u1 should be applicable")
	}

	out = idx.ApplicableHumans(0, 0, map[string]bool{"antwerp": true}, true)
	if out["u1"] {
		t.Errorf("strict on, antwerp spawn: u1 should NOT be applicable (restriction=belgium)")
	}

	out = idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, true)
	if !out["u1"] {
		t.Errorf("strict on, belgium spawn: u1 should be applicable")
	}
}

func TestHumanGeoIndex_DisabledHumansExcluded(t *testing.T) {
	h := mkHuman("u1", []string{"belgium"}, nil, 0, 0)
	h.Enabled = false
	humans := map[string]*db.Human{"u1": h}
	idx := BuildHumanGeoIndex(humans, nil)

	out := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, false)
	if out["u1"] {
		t.Errorf("disabled human should not be in index")
	}
}

// TestHumanGeoIndex_DistanceWithStrictAreaRestriction covers the rtree
// path's strict-mode check. A human discovered via the distance r-tree
// must ALSO have their AreaRestriction overlap matchedAreas in strict
// mode — same semantics as area-discovered humans.
func TestHumanGeoIndex_DistanceWithStrictAreaRestriction(t *testing.T) {
	humans := map[string]*db.Human{
		// u1: distance-based rules, restricted to "belgium" only
		"u1": mkHuman("u1", nil, []string{"belgium"}, 1.0, 1.0),
	}
	perHumanMaxDist := map[string]int{"u1": 5000}
	idx := BuildHumanGeoIndex(humans, perHumanMaxDist)

	// Spawn near (1,1), in matchedAreas={belgium} — strict-mode passes
	// (restriction covers belgium), and the rtree finds u1.
	out := idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{"belgium": true}, true)
	if !out["u1"] {
		t.Errorf("strict on, belgium spawn near u1: u1 should be applicable")
	}

	// Spawn near (1,1) but matchedAreas={japan} — strict-mode rejects
	// even though the rtree would otherwise find u1.
	out = idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{"japan": true}, true)
	if out["u1"] {
		t.Errorf("strict on, japan spawn (restriction=belgium): u1 should NOT be applicable")
	}

	// Same as above but strict OFF — u1 IS applicable via distance,
	// restriction is ignored.
	out = idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{"japan": true}, false)
	if !out["u1"] {
		t.Errorf("strict off, japan spawn (any area): u1 should be applicable via distance")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
