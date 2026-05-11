package db

import "testing"

func TestPartitionByHuman_GroupsByExtractor(t *testing.T) {
	raids := []RaidTracking{
		{ID: "u1", Level: 5},
		{ID: "u2", Level: 1},
		{ID: "u1", Level: 3},
	}
	rows := []*RaidTracking{&raids[0], &raids[1], &raids[2]}
	got := PartitionByHuman[RaidTracking](rows)
	if len(got["u1"]) != 2 {
		t.Errorf("u1 = %d rules, want 2", len(got["u1"]))
	}
	if len(got["u2"]) != 1 {
		t.Errorf("u2 = %d rules, want 1", len(got["u2"]))
	}
	if got["u1"][0] != &raids[0] {
		t.Errorf("pointer identity broken")
	}
}

func TestPartitionByHuman_EmptyIDSkipped(t *testing.T) {
	raids := []RaidTracking{{ID: ""}, {ID: "u1"}}
	rows := []*RaidTracking{&raids[0], &raids[1]}
	got := PartitionByHuman[RaidTracking](rows)
	if _, ok := got[""]; ok {
		t.Errorf("empty ID should not appear")
	}
	if len(got["u1"]) != 1 {
		t.Errorf("u1 = %d, want 1", len(got["u1"]))
	}
}
