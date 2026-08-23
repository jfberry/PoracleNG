package logging

import (
	"testing"

	log "github.com/sirupsen/logrus"
)

// TestResolveLevel covers logrus-native levels, the project's legacy
// Winston-style aliases (verbose/silly, inherited from PoracleJS), and the
// info fallback for empty/unknown input. logrus has no level between Debug
// and Info, so "verbose" collapses to Info; "silly" maps to Trace.
func TestResolveLevel(t *testing.T) {
	cases := []struct {
		in   string
		want log.Level
	}{
		{"trace", log.TraceLevel},
		{"debug", log.DebugLevel},
		{"info", log.InfoLevel},
		{"warn", log.WarnLevel},
		{"error", log.ErrorLevel},
		{"verbose", log.InfoLevel},   // legacy Winston alias
		{"silly", log.TraceLevel},    // legacy Winston alias
		{"VERBOSE", log.InfoLevel},   // case-insensitive
		{"  silly ", log.TraceLevel}, // trimmed
		{"", log.InfoLevel},          // empty → default
		{"nonsense", log.InfoLevel},  // unknown → default
	}
	for _, c := range cases {
		if got := resolveLevel(c.in); got != c.want {
			t.Errorf("resolveLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
