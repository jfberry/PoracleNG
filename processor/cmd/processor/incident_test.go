package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichIncident_TemplateTypeAndFields covers superpowers/sdd task-3: the
// "incident" DTS test type. Incidents (Gold Pokéstop, Kecleon, Pokémon
// contest) ride the same webhook.InvasionWebhook payload as invasions but
// must render under the "incident" DTS template, not "invasion". The
// incident-only alias fields (incidentTypeName, incidentEmoji, color — see
// internal/api/dts_fields.go's incidentOnlyFields) resolve from gruntName /
// gruntTypeEmoji / gruntTypeColor at LayeredView build time (already covered
// by internal/dts/layered_view_test.go's TestLayeredView_IncidentAliases);
// this test only needs to confirm enrichIncident/enrichForType populate the
// underlying perLang/base fields those aliases read from.
func TestEnrichIncident_TemplateTypeAndFields(t *testing.T) {
	ps := newEnrichParityService(t)

	for _, testID := range []string{"kecleon", "goldstop", "pokemoncontest"} {
		t.Run(testID, func(t *testing.T) {
			raw := loadTestdataSample(t, "incident", testID)

			r, err := ps.enrichIncident(raw, "en", false)
			if err != nil {
				t.Fatalf("enrichIncident error: %v", err)
			}
			if r.templateType != "incident" {
				t.Errorf("templateType = %q, want %q", r.templateType, "incident")
			}
			if len(r.base) == 0 {
				t.Errorf("base enrichment is empty, want populated map")
			}
			// gruntName is what the "incidentTypeName" DTS alias resolves
			// from (see internal/dts/view.go's "incident" alias table).
			if r.perLang == nil || r.perLang["gruntName"] == "" || r.perLang["gruntName"] == nil {
				t.Errorf("perLang[gruntName] = %v, want a non-empty translated event name", r.perLang["gruntName"])
			}
		})
	}
}

// TestEnrichForType_Incident locks in the enrichForType dispatch added for
// this task: unlike the other Derived aliases (monsterChanged, rsvpChanges,
// questSummary, weatherchange — still "not yet supported" until later tasks),
// "incident" IS parsed from a raw webhook and must resolve successfully with
// the alias's TemplateType ("incident") intact.
func TestEnrichForType_Incident(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "incident", "kecleon")

	r, err := ps.enrichForType("incident", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("incident", ...) error: %v`, err)
	}
	if r.templateType != "incident" {
		t.Errorf(`enrichForType("incident", ...).templateType = %q, want "incident"`, r.templateType)
	}

	// Other derived names must still error — this task only wires "incident".
	if _, err := ps.enrichForType("monsterChanged", raw, "en", false); err == nil {
		t.Errorf(`enrichForType("monsterChanged", ...) error = nil, want a "derived type not yet supported" error`)
	}
}

// TestProcessTestIncident_EnqueuesIncidentTemplate verifies the !poracle-test
// / /api/test dispatch path: processTestIncident must enqueue a RenderJob
// carrying TemplateType "incident" (not "invasion"), built via the shared
// renderJobFromEnrich helper like the other non-pokemon test handlers.
func TestProcessTestIncident_EnqueuesIncidentTemplate(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "incident", "kecleon")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestIncident(raw, target); err != nil {
		t.Fatalf("processTestIncident error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if job.TemplateType != "incident" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "incident")
		}
		if job.AlertType != "incident" {
			t.Errorf("job.AlertType = %q, want %q", job.AlertType, "incident")
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestProcessTest_DispatchesIncident verifies the top-level ProcessTest
// switch has an explicit "incident" case routing to processTestIncident.
// Source-grep because exercising ProcessTest end-to-end needs a fully-wired
// dtsRenderer/dispatcher (same convention as TestProcessShowcase_Wiring in
// showcase_test.go and the invasion.go wiring tests in invasion_test.go).
func TestProcessTest_DispatchesIncident(t *testing.T) {
	src, err := os.ReadFile("test.go")
	if err != nil {
		t.Fatalf("read test.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(n, `case "incident": return ps.processTestIncident(raw, matchedUser)`) {
		t.Error(`test.go's ProcessTest switch missing: case "incident": return ps.processTestIncident(raw, matchedUser)`)
	}
}
