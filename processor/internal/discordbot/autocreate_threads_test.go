package discordbot

import "testing"

func TestEncodeThreadJoinID(t *testing.T) {
	got := encodeThreadJoinID("12345", "67890")
	want := "poracle:thread:12345:67890:join"
	if got != want {
		t.Errorf("encodeThreadJoinID = %q, want %q", got, want)
	}
}

func TestDecodeThreadJoinID(t *testing.T) {
	tests := []struct {
		in            string
		wantMaster    string
		wantThread    string
		wantOK        bool
	}{
		{"poracle:thread:111:222:join", "111", "222", true},
		{"poracle:thread:111:222", "", "", false},
		{"poracle:thread::222:join", "", "", false},
		{"random:button", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		master, thread, ok := decodeThreadJoinID(tc.in)
		if ok != tc.wantOK || master != tc.wantMaster || thread != tc.wantThread {
			t.Errorf("decodeThreadJoinID(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, master, thread, ok, tc.wantMaster, tc.wantThread, tc.wantOK)
		}
	}
}
