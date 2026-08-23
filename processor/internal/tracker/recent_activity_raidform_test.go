package tracker

import "testing"

func TestRecentRaidForms(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidForm(25, 598)
	r.RecordRaidForm(25, 680)
	r.RecordRaidForm(25, 0) // any-form placeholder: ignored
	if got := r.RecentRaidForms(25); len(got) != 2 {
		t.Fatalf("RecentRaidForms(25) = %v, want two entries", got)
	}
	if len(r.RecentRaidForms(999)) != 0 {
		t.Error("unknown boss should have no recent raid forms")
	}
	// Separate from spawn forms and from raid costumes.
	if len(r.RecentForms(25)) != 0 {
		t.Error("raid forms must not leak into spawn RecentForms")
	}
}
