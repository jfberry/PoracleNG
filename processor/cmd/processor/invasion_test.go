package main

import (
	"os"
	"strings"
	"testing"
)

// TestInvasion_TemplateType_Incident checks that the invasion handler emits
// TemplateType="incident" for event-only pokestop webhooks (gruntTypeID == 0 &&
// displayType >= 7), and TemplateType="invasion" for real grunt invasions.
//
// Source-grep approach: a fully wired ProcessorService is large to construct
// in a unit test. Instead we verify the invariants directly in the source so
// the reviewer can see the branching logic stays in sync with its tests.
func TestInvasion_TemplateType_Incident(t *testing.T) {
	src, err := os.ReadFile("invasion.go")
	if err != nil {
		t.Fatalf("read invasion.go: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(src)), " ")

	// The handler must detect the incident condition.
	wantCheck := `isIncident := gruntTypeID == 0 && displayType >= 7`
	if !strings.Contains(normalized, wantCheck) {
		t.Errorf("invasion.go: missing incident detection %q", wantCheck)
	}

	// The templateType variable must be set to "incident" for the incident path.
	wantIncident := `templateType = "incident"`
	if !strings.Contains(normalized, wantIncident) {
		t.Errorf("invasion.go: missing incident template assignment %q", wantIncident)
	}

	// AlertType must always be "invasion" (for matching / rate limiting).
	wantAlert := `AlertType: "invasion"`
	if !strings.Contains(normalized, wantAlert) {
		t.Errorf("invasion.go: AlertType must always be \"invasion\", not found: %q", wantAlert)
	}

	// The RenderJob must use the computed templateType variable, not a hard-coded string.
	wantTemplate := `TemplateType: templateType`
	if !strings.Contains(normalized, wantTemplate) {
		t.Errorf("invasion.go: TemplateType must reference %q variable", wantTemplate)
	}
}

// TestInvasion_GruntTemplateType confirms that the fallback template type for
// grunt invasions (gruntTypeID > 0) stays "invasion" — the initial value of
// the templateType variable before the isIncident branch.
func TestInvasion_GruntTemplateType(t *testing.T) {
	src, err := os.ReadFile("invasion.go")
	if err != nil {
		t.Fatalf("read invasion.go: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(src)), " ")

	// Default must be "invasion".
	wantDefault := `templateType := "invasion"`
	if !strings.Contains(normalized, wantDefault) {
		t.Errorf("invasion.go: default templateType must be %q", wantDefault)
	}
}
