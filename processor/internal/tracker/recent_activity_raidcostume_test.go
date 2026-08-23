package tracker

import "testing"

func TestRecentRaidCostumes(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidCostume(25, 1)
	r.RecordRaidCostume(25, 12)
	r.RecordRaidCostume(25, 0) // no-costume: ignored
	if got := r.RecentRaidCostumes(25); len(got) != 2 {
		t.Fatalf("RecentRaidCostumes(25) = %v, want two entries", got)
	}
	if len(r.RecentRaidCostumes(999)) != 0 {
		t.Error("unknown boss should have no recent raid costumes")
	}
	// Separate from spawn costumes.
	if len(r.RecentCostumes(25)) != 0 {
		t.Error("raid costumes must not leak into the spawn RecentCostumes bucket")
	}
}
