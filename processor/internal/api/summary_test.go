package api

import (
	"slices"
	"testing"
)

func TestSummaryAPI_KnownAlertTypesIncludesQuest(t *testing.T) {
	if !slices.Contains(knownSummaryAlertTypes, "quest") {
		t.Errorf("knownSummaryAlertTypes must include quest, got %v", knownSummaryAlertTypes)
	}
}
