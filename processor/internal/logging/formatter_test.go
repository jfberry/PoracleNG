package logging

import (
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

// LevelDesc had no entry for TraceLevel (6), so formatting a trace entry
// indexed past the end and panicked (#268). Every logrus level must format.
func TestPlainFormatterHandlesEveryLevel(t *testing.T) {
	f := &PlainFormatter{TimestampFormat: "2006-01-02 15:04:05", LevelDesc: levelDesc}
	for _, lvl := range log.AllLevels {
		out, err := f.Format(&log.Entry{Level: lvl, Time: time.Unix(0, 0), Message: "hi"})
		if err != nil {
			t.Fatalf("%s: %v", lvl, err)
		}
		if !strings.HasSuffix(string(out), " hi\n") {
			t.Errorf("%s: output %q", lvl, out)
		}
	}
	out, _ := f.Format(&log.Entry{Level: log.TraceLevel, Time: time.Unix(0, 0), Message: "x"})
	if !strings.HasPrefix(string(out), "TRCE ") {
		t.Errorf("trace prefix: %q", out)
	}
}

// A level outside the table must not panic either.
func TestPlainFormatterUnknownLevel(t *testing.T) {
	f := &PlainFormatter{TimestampFormat: "2006", LevelDesc: []string{"PANC"}}
	if _, err := f.Format(&log.Entry{Level: log.DebugLevel, Time: time.Unix(0, 0)}); err != nil {
		t.Fatal(err)
	}
}
