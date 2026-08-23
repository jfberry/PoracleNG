package main

import (
	"os"
	"strings"
	"testing"
)

// TestIncidentExpired covers the incident-expiration gate. Golbat re-emits a
// stop's unified webhook when its lure changes, which can carry a stale
// incident (e.g. a showcase whose window already ended). A known past
// expiration is dropped; a zero/unknown expiration is treated as not-expired
// (we can't prove it's stale).
func TestIncidentExpired(t *testing.T) {
	const now = int64(1_000_000)
	cases := []struct {
		name       string
		expiration int64
		want       bool
	}{
		{"active incident", now + 600, false},
		{"expiring exactly now", now, true},
		{"expired incident", now - 1, true},
		{"stale showcase", now - 3600, true},
		{"unknown expiration", 0, false},
		{"negative/unset", -1, false},
	}
	for _, c := range cases {
		if got := incidentExpired(c.expiration, now); got != c.want {
			t.Errorf("%s: incidentExpired(%d, %d) = %v, want %v",
				c.name, c.expiration, now, got, c.want)
		}
	}
}

// TestProcessInvasion_GatesOnExpiration verifies the gate is wired into the
// handler. Source-grep for the same reason as the other invasion tests.
func TestProcessInvasion_GatesOnExpiration(t *testing.T) {
	src, err := os.ReadFile("invasion.go")
	if err != nil {
		t.Fatalf("read invasion.go: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(normalized, "incidentExpired(expiration") {
		t.Fatal("invasion.go must call incidentExpired(expiration, ...) and return early — otherwise a stale re-emitted incident (e.g. a showcase whose window ended) still alerts")
	}
}

// TestProcessInvasion_SuppressesContentlessShowcase — Golbat also emits a bare
// display_type=9 invasion envelope for contests (no rankings); the real showcase
// arrives on the pokéstop webhook. The invasion handler must drop the empty one
// so trackers don't get a duplicate empty showcase card.
func TestProcessInvasion_SuppressesContentlessShowcase(t *testing.T) {
	src, err := os.ReadFile("invasion.go")
	if err != nil {
		t.Fatalf("read invasion.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	want := "displayType == showcaseDisplayType && len(inv.ShowcaseRankings) == 0"
	if !strings.Contains(n, want) {
		t.Fatalf("invasion.go must suppress content-less showcase invasions (%q)", want)
	}
}

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

	// The handler must detect the incident condition. displayType >= 7
	// matches PoracleJS and util.json (7=Gold-Stop, 8=Kecleon, 9=Showcase).
	wantCheck := `isIncident := gruntTypeID == 0 && displayType >= 7`
	if !strings.Contains(normalized, wantCheck) {
		t.Errorf("invasion.go: missing incident detection %q", wantCheck)
	}

	// The templateType variable must be set to "incident" for the incident path.
	wantIncident := `templateType = "incident"`
	if !strings.Contains(normalized, wantIncident) {
		t.Errorf("invasion.go: missing incident template assignment %q", wantIncident)
	}

	// AlertType must also split — incidents and grunt invasions are
	// distinct events at the same pokestop; if either ever grows
	// reply/edit support the tracker must distinguish them.
	wantAlert := `alertType = "incident"`
	if !strings.Contains(normalized, wantAlert) {
		t.Errorf("invasion.go: AlertType for incidents must be %q, not found", wantAlert)
	}

	// The RenderJob must use the computed templateType + alertType variables.
	wantTemplate := `TemplateType: templateType`
	if !strings.Contains(normalized, wantTemplate) {
		t.Errorf("invasion.go: TemplateType must reference %q variable", wantTemplate)
	}
	wantAlertVar := `AlertType: alertType`
	if !strings.Contains(normalized, wantAlertVar) {
		t.Errorf("invasion.go: AlertType must reference %q variable", wantAlertVar)
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

	// Default must be "invasion" (both template and alert type).
	wantTemplate := `templateType := "invasion"`
	if !strings.Contains(normalized, wantTemplate) {
		t.Errorf("invasion.go: default templateType must be %q", wantTemplate)
	}
	wantAlert := `alertType := "invasion"`
	if !strings.Contains(normalized, wantAlert) {
		t.Errorf("invasion.go: default alertType must be %q", wantAlert)
	}
}
