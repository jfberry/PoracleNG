package tracker

import "testing"

func TestRecentForms(t *testing.T) {
	r := NewRecentActivity()
	r.RecordForm(25, 598)
	r.RecordForm(25, 680)
	r.RecordForm(25, 0) // any-form placeholder: ignored
	got := r.RecentForms(25)
	if len(got) != 2 {
		t.Fatalf("RecentForms(25) = %v, want two entries [598 680]", got)
	}
	if len(r.RecentForms(999)) != 0 {
		t.Error("unknown pokemon should have no recent forms")
	}
}
