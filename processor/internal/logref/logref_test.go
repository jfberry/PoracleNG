package logref

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

func TestErrorfAttachesRefField(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)

	Errorf("enc-123", "something failed for %s", "user42")

	entries := hook.AllEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != logrus.ErrorLevel {
		t.Errorf("expected ErrorLevel, got %v", e.Level)
	}
	if e.Data["ref"] != "enc-123" {
		t.Errorf("expected ref=enc-123, got %v", e.Data["ref"])
	}
	if !strings.Contains(e.Message, "user42") {
		t.Errorf("message missing formatted args: %q", e.Message)
	}
}

func TestWarnfInfofDebugf(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)
	// Capture all levels for the test
	logrus.SetLevel(logrus.DebugLevel)

	Warnf("ref-w", "warn msg")
	Infof("ref-i", "info msg")
	Debugf("ref-d", "debug msg")

	entries := hook.AllEntries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	want := []struct {
		level logrus.Level
		ref   string
		msg   string
	}{
		{logrus.WarnLevel, "ref-w", "warn msg"},
		{logrus.InfoLevel, "ref-i", "info msg"},
		{logrus.DebugLevel, "ref-d", "debug msg"},
	}
	for i, w := range want {
		if entries[i].Level != w.level {
			t.Errorf("entry %d level: want %v, got %v", i, w.level, entries[i].Level)
		}
		if entries[i].Data["ref"] != w.ref {
			t.Errorf("entry %d ref: want %s, got %v", i, w.ref, entries[i].Data["ref"])
		}
		if entries[i].Message != w.msg {
			t.Errorf("entry %d message: want %q, got %q", i, w.msg, entries[i].Message)
		}
	}
}

func TestWithReturnsBoundEntry(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)

	l := With("ref-abc")
	l.Errorf("first")
	l.Errorf("second")

	entries := hook.AllEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.Data["ref"] != "ref-abc" {
			t.Errorf("entry %d ref: got %v", i, e.Data["ref"])
		}
	}
}
