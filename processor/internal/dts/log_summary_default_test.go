package dts

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// TestLogSummaryNoDefaultWarningSkipsHelp verifies LogSummary does not emit the
// "no default template" warning for the platform-agnostic help type — help is
// selected by topic id (help/fort, help/track, …), not via the per-platform
// default-template fallback, so a missing default is expected, not a
// misconfiguration. A genuine alert type missing its default must still warn.
func TestLogSummaryNoDefaultWarningSkipsHelp(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)

	entries := []DTSEntry{
		{Type: "help", ID: "fort", Platform: "", Language: ""},                    // agnostic, no default → must NOT warn
		{Type: "monster", ID: "1", Platform: "discord", Language: ""},             // no default → must warn
		{Type: "raid", ID: "1", Platform: "discord", Language: "", Default: true}, // has default → no warn
	}
	ts, _ := newTestStore(t, entries)
	ts.LogSummary()

	var warnedHelp, warnedMonster bool
	for _, e := range hook.AllEntries() {
		if e.Level != logrus.WarnLevel || !strings.Contains(e.Message, "no default template") {
			continue
		}
		if strings.Contains(e.Message, `type="help"`) {
			warnedHelp = true
		}
		if strings.Contains(e.Message, `type="monster"`) {
			warnedMonster = true
		}
	}
	if warnedHelp {
		t.Error(`LogSummary warned "no default template" for help; help is topic-based and needs no default`)
	}
	if !warnedMonster {
		t.Error(`LogSummary should still warn "no default template" for monster (a real alert type missing its default)`)
	}
}
