package db

import "testing"

func TestIsSummary(t *testing.T) {
	cases := []struct {
		clean   int
		clean_  bool
		edit    bool
		summary bool
	}{
		{0, false, false, false},
		{1, true, false, false},
		{2, false, true, false},
		{3, true, true, false},
		{4, false, false, true},
		{5, true, false, true},
		{6, false, true, true},
		{7, true, true, true},
	}
	for _, c := range cases {
		if IsClean(c.clean) != c.clean_ || IsEdit(c.clean) != c.edit || IsSummary(c.clean) != c.summary {
			t.Errorf("clean=%d: got IsClean=%v IsEdit=%v IsSummary=%v, want %v %v %v",
				c.clean, IsClean(c.clean), IsEdit(c.clean), IsSummary(c.clean),
				c.clean_, c.edit, c.summary)
		}
	}
}
