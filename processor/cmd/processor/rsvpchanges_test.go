package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichRsvpChanges_TemplateTypeAndFields covers superpowers/sdd task-7:
// the derived "rsvpChanges" DTS test type — the last of the derived types.
// The testdata.json sample ("rsvp_changes"/"level5" — the underscore wire
// spelling; see resolveDTSTypeFromRaw) is a raid webhook with its `rsvps`
// array populated with two safely-far-future timeslots (year 2100), the
// second being the latest. enrichRsvpChanges reuses enrichRaid wholesale for
// the base/perLang fields (webhook.RaidWebhook already carries `rsvps`
// natively) and adds extras["overrideCleanTTH"] — the latest future RSVP
// timeslot in seconds, mirroring ProcessRaid's own latestFutureTimeslotSec
// computation (see raid.go).
func TestEnrichRsvpChanges_TemplateTypeAndFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "rsvp_changes", "level5")

	r, err := ps.enrichRsvpChanges(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichRsvpChanges error: %v", err)
	}
	if r.templateType != "rsvpChanges" {
		t.Errorf("templateType = %q, want %q", r.templateType, "rsvpChanges")
	}
	if len(r.base) == 0 {
		t.Fatalf("base enrichment is empty, want the raid's populated map")
	}
	if got, _ := r.base["pokemon_id"].(int); got != 895 {
		t.Errorf(`base["pokemon_id"] = %v, want 895`, r.base["pokemon_id"])
	}

	// The sample's two RSVP timeslots are 4102444800000ms and 4102448400000ms
	// (year 2100) — both safely future no matter when this test runs. The
	// latest, ceil-converted to seconds, is 4102448400.
	const wantLatest int64 = 4102448400
	got, ok := r.extras["overrideCleanTTH"].(int64)
	if !ok {
		t.Fatalf(`extras["overrideCleanTTH"] = %v (%T), want int64`, r.extras["overrideCleanTTH"], r.extras["overrideCleanTTH"])
	}
	if got != wantLatest {
		t.Errorf(`extras["overrideCleanTTH"] = %d, want %d (the latest RSVP timeslot)`, got, wantLatest)
	}

	// enricher.Raid's own `rsvps` field should carry both future timeslots
	// through to the base enrichment (the compact template's RSVP list).
	rsvps, ok := r.base["rsvps"].([]map[string]any)
	if !ok {
		t.Fatalf(`base["rsvps"] = %v (%T), want []map[string]any`, r.base["rsvps"], r.base["rsvps"])
	}
	if len(rsvps) != 2 {
		t.Errorf("base[\"rsvps\"] has %d entries, want 2", len(rsvps))
	}
}

// TestEnrichForType_RsvpChanges locks in the enrichForType dispatch added
// for this task: "rsvpChanges" (and its raw webhook-type spelling
// "rsvp-changes") must resolve via enrichRsvpChanges with the alias's
// TemplateType intact, matching the incident/weatherchange/questSummary/
// monsterChanged precedent from tasks 3-6. This is the last derived type, so
// unlike the earlier tasks' tests there's no other still-unimplemented
// derived name left to assert still errors.
func TestEnrichForType_RsvpChanges(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "rsvp_changes", "level5")

	r, err := ps.enrichForType("rsvpChanges", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("rsvpChanges", ...) error: %v`, err)
	}
	if r.templateType != "rsvpChanges" {
		t.Errorf(`enrichForType("rsvpChanges", ...).templateType = %q, want "rsvpChanges"`, r.templateType)
	}
	if _, ok := r.extras["overrideCleanTTH"].(int64); !ok {
		t.Errorf(`enrichForType("rsvpChanges", ...).extras["overrideCleanTTH"] missing or wrong type: %v`, r.extras["overrideCleanTTH"])
	}

	r2, err := ps.enrichForType("rsvp-changes", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("rsvp-changes", ...) error: %v`, err)
	}
	if r2.templateType != "rsvpChanges" {
		t.Errorf(`enrichForType("rsvp-changes", ...).templateType = %q, want "rsvpChanges"`, r2.templateType)
	}
}

// TestEnrichWebhook_RsvpChanges confirms the /api/dts/enrich-facing surface
// (EnrichWebhook) returns the raid+RSVP variables for "rsvpChanges".
// newEnrichParityService doesn't wire a dtsRenderer, so this exercises the
// mergeEnrichment fallback branch of EnrichWebhook.
func TestEnrichWebhook_RsvpChanges(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "rsvp_changes", "level5")

	vars, err := ps.EnrichWebhook("rsvpChanges", raw, "en", "discord")
	if err != nil {
		t.Fatalf(`EnrichWebhook("rsvpChanges", ...) error: %v`, err)
	}
	if got, _ := vars["pokemon_id"].(int); got != 895 {
		t.Errorf(`vars["pokemon_id"] = %v, want 895`, vars["pokemon_id"])
	}
	if _, ok := vars["rsvps"]; !ok {
		t.Errorf(`vars["rsvps"] missing, want the RSVP timeslot list`)
	}
}

// TestProcessTestRsvpChanges_EnqueuesRsvpChangesTemplate verifies the
// !poracle-test / /api/test dispatch path (wire type "rsvp_changes" — see
// resolveDTSTypeFromRaw): processTestRsvpChanges must enqueue a RenderJob
// with TemplateType "rsvpChanges", a non-zero OverrideCleanTTH (the latest
// future RSVP timeslot), and EditKey/ReplyKey built from the same
// raidlife:{gymID}:{raidEnd} convention ProcessRaid uses (raid.go's
// raidReplyKeyFmt/raidEditKeyFmt) — so a test send lines up with any live
// raid/egg message already tracked for the same gym+end lifecycle.
func TestProcessTestRsvpChanges_EnqueuesRsvpChangesTemplate(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "rsvp_changes", "level5")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestRsvpChanges(raw, target); err != nil {
		t.Fatalf("processTestRsvpChanges error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if job.TemplateType != "rsvpChanges" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "rsvpChanges")
		}
		if job.IsPokemon {
			t.Errorf("job.IsPokemon = true, want false")
		}
		const wantLatest int64 = 4102448400
		if job.OverrideCleanTTH != wantLatest {
			t.Errorf("job.OverrideCleanTTH = %d, want %d", job.OverrideCleanTTH, wantLatest)
		}
		wantGymID := "c3a1f2ea8d03748fdbc7fa72c6a15772.16"
		wantReplyKey := "raidlife:" + wantGymID + ":1775478228"
		wantEditKey := "raid:" + wantGymID + ":1775478228"
		if job.ReplyKey != wantReplyKey {
			t.Errorf("job.ReplyKey = %q, want %q", job.ReplyKey, wantReplyKey)
		}
		if job.EditKey != wantEditKey {
			t.Errorf("job.EditKey = %q, want %q", job.EditKey, wantEditKey)
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestProcessTest_DispatchesRsvpChanges verifies the top-level ProcessTest
// switch has an explicit "rsvp_changes" case (the wire spelling — see
// resolveDTSTypeFromRaw) routing to processTestRsvpChanges. Source-grep
// because exercising ProcessTest end-to-end needs a fully-wired
// dtsRenderer/dispatcher (same convention as
// TestProcessTest_DispatchesMonsterChanged/Weatherchange/QuestSummary).
func TestProcessTest_DispatchesRsvpChanges(t *testing.T) {
	src, err := os.ReadFile("test.go")
	if err != nil {
		t.Fatalf("read test.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(n, `case "rsvp_changes": return ps.processTestRsvpChanges(raw, matchedUser)`) {
		t.Error(`test.go's ProcessTest switch missing: case "rsvp_changes": return ps.processTestRsvpChanges(raw, matchedUser)`)
	}
}

// TestResolveDTSTypeFromRaw_RsvpChanges locks in the wire-spelling→DTS-type
// mapping ProcessTest's CheckTemplate call depends on: without this explicit
// case, resolveDTSTypeFromRaw's default branch would return "rsvp_changes"
// unchanged, which doesn't match the registered "rsvpChanges" DTS template
// type — CheckTemplate would fail to find it and every
// !poracle-test rsvp-changes,<id> / POST /api/test {"type":"rsvp_changes"}
// invocation would error out before ever reaching processTestRsvpChanges.
func TestResolveDTSTypeFromRaw_RsvpChanges(t *testing.T) {
	if got := resolveDTSTypeFromRaw("rsvp_changes", nil); got != "rsvpChanges" {
		t.Errorf(`resolveDTSTypeFromRaw("rsvp_changes", nil) = %q, want "rsvpChanges"`, got)
	}
}
