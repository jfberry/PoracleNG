package tracker

import "testing"

func TestRecentCostumes(t *testing.T) {
	r := NewRecentActivity()
	r.RecordCostume(25, 1)
	r.RecordCostume(25, 8)
	r.RecordCostume(25, 0) // no-costume: ignored
	got := r.RecentCostumes(25)
	if len(got) != 2 {
		t.Fatalf("RecentCostumes(25) = %v, want [1 8]", got)
	}
	if r.RecentCostumes(999) != nil && len(r.RecentCostumes(999)) != 0 {
		t.Error("unknown pokemon should have no recent costumes")
	}
}
