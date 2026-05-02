package bot

import "testing"

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		first    string
		last     string
		username string
		title    string
		want     string
	}{
		{"user with full name and handle", "Alice", "Smith", "alice123", "", "Alice Smith [alice123]"},
		{"user with first name only", "Bob", "", "", "", "Bob"},
		{"user with first name and handle", "Bob", "", "bob42", "", "Bob [bob42]"},
		{"user with last name only", "", "Smith", "", "", "Smith"},
		{"user with handle only", "", "", "anon", "", "[anon]"},
		{"public group with title and handle", "", "", "mygroup", "My Group", "My Group [mygroup]"},
		{"private group with title only", "", "", "", "Private Group", "Private Group"},
		{"empty input", "", "", "", "", ""},
		{"non-ASCII stripped from first name", "Алиса", "Smith", "alice", "", "Smith [alice]"},
		{"emoji stripped from title", "", "", "", "🎮 Gaming Group", "Gaming Group"},
		{"trims whitespace from no-username path", "Alice", "", "", "", "Alice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DisplayName(tc.first, tc.last, tc.username, tc.title)
			if got != tc.want {
				t.Errorf("DisplayName(%q, %q, %q, %q) = %q, want %q",
					tc.first, tc.last, tc.username, tc.title, got, tc.want)
			}
		})
	}
}
