package api

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// A Replaced/Updates length mismatch is a diff bug: it must be logged, and no
// previous attached (a positional pairing would be wrong), rather than
// silently dropping the tail.
func TestAttachPrevious_MisalignedLogsAndOmits(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	updated := make([]v2UpdatedRuleEnvelope[struct{}], 2)
	attachPrevious(updated, []v2RuleEnvelope[struct{}]{{UID: 7}}, "test")
	for i, u := range updated {
		if u.Previous != nil {
			t.Fatalf("updated[%d] got a previous despite the mismatch", i)
		}
	}
	var sawError bool
	for _, e := range hook.AllEntries() {
		if e.Level == log.ErrorLevel {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("mismatch was not logged at error level")
	}
}

func TestAttachPrevious_AlignedPairsByIndex(t *testing.T) {
	updated := make([]v2UpdatedRuleEnvelope[struct{}], 2)
	attachPrevious(updated, []v2RuleEnvelope[struct{}]{{UID: 1}, {UID: 2}}, "test")
	for i, u := range updated {
		if u.Previous == nil || u.Previous.UID != int64(i+1) {
			t.Fatalf("updated[%d].Previous = %+v", i, u.Previous)
		}
	}
}
