package db

import "testing"

func TestDiffTracking_OverrideAreasByKey(t *testing.T) {
	existing := &LureTrackingAPI{UID: 7, LureID: 501, OverrideAreas: []string{"gent centrum"}}
	insert := &LureTrackingAPI{LureID: 501, OverrideAreas: []string{"gent_centrum"}}
	noMatch, dup, _, upd := DiffTracking(existing, insert)
	if noMatch || !dup || upd {
		t.Fatalf("spaced vs underscored override: noMatch=%v dup=%v upd=%v, want duplicate", noMatch, dup, upd)
	}

	existing = &LureTrackingAPI{UID: 7, LureID: 501, OverrideAreas: []string{"a", "b"}}
	insert = &LureTrackingAPI{LureID: 501, OverrideAreas: []string{"b", "a"}}
	if _, dup, _, _ := DiffTracking(existing, insert); dup {
		t.Fatal("reordered override_areas must not be a duplicate (order is kept)")
	}

	existing = &LureTrackingAPI{UID: 7, LureID: 501, OverrideAreas: []string{"a"}}
	insert = &LureTrackingAPI{LureID: 501, OverrideAreas: []string{"a", "b"}}
	if _, dup, _, _ := DiffTracking(existing, insert); dup {
		t.Fatal("different length override_areas must not be a duplicate")
	}

	existing = &LureTrackingAPI{UID: 7, LureID: 501}
	insert = &LureTrackingAPI{LureID: 501, OverrideAreas: []string{}}
	if _, dup, _, _ := DiffTracking(existing, insert); !dup {
		t.Fatal("nil vs empty override_areas should be a duplicate")
	}
}
