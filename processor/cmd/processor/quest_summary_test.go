package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichQuestSummary_TemplateTypeAndFields covers superpowers/sdd
// task-5: the derived "questSummary" DTS test type. The testdata.json
// sample ("quest_summary"/"stardust" — the underscore wire spelling; see
// resolveDTSTypeFromRaw) carries a single reward group — three quests all
// rewarding 1500 Stardust — so this exercises a real multi-entry digest, not
// a single-quest edge case. enrichQuestSummary re-enriches each quest via
// questEnrichOne and builds the group view via buildQuestSummaryGroupView —
// the exact same helpers DispatchQuestSummary (the live scheduler) uses —
// so the resulting fields should match what a live summary digest would
// render.
func TestEnrichQuestSummary_TemplateTypeAndFields(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "quest_summary", "stardust")

	r, err := ps.enrichQuestSummary(raw, "en", false)
	if err != nil {
		t.Fatalf("enrichQuestSummary error: %v", err)
	}
	if r.templateType != "questSummary" {
		t.Errorf("templateType = %q, want %q", r.templateType, "questSummary")
	}
	if len(r.base) == 0 {
		t.Fatalf("base enrichment is empty, want populated map")
	}

	// rewardName is the group-shared field questSummaryFields
	// (dts_fields.go) documents as the template's header text — for a
	// stardust (type 3) reward it's "<amount> Stardust".
	if got, _ := r.base["rewardName"].(string); got != "1500 Stardust" {
		t.Errorf(`base["rewardName"] = %q, want "1500 Stardust"`, got)
	}
	if got, _ := r.base["rewardType"].(int); got != 3 {
		t.Errorf(`base["rewardType"] = %v, want 3`, r.base["rewardType"])
	}
	if got, _ := r.base["reward"].(int); got != 1500 {
		t.Errorf(`base["reward"] = %v, want 1500`, r.base["reward"])
	}

	// count/chunk/chunks are the pagination fields questSummaryFields
	// documents — this partial is a single un-chunked group.
	if got, _ := r.base["count"].(int); got != 3 {
		t.Errorf(`base["count"] = %v, want 3 (three quests in the sample)`, r.base["count"])
	}
	if got, _ := r.base["chunk"].(int); got != 1 {
		t.Errorf(`base["chunk"] = %v, want 1`, r.base["chunk"])
	}
	if got, _ := r.base["chunks"].(int); got != 1 {
		t.Errorf(`base["chunks"] = %v, want 1`, r.base["chunks"])
	}

	// quests is the per-pokestop array the {{#each quests}} block scope
	// (questSummaryBlockScopes, dts_fields.go) iterates. Each entry should
	// carry the same field set a regular quest template reads, plus withAR.
	quests, ok := r.base["quests"].([]map[string]any)
	if !ok {
		t.Fatalf("base[\"quests\"] = %v (%T), want []map[string]any", r.base["quests"], r.base["quests"])
	}
	if len(quests) != 3 {
		t.Fatalf("len(quests) = %d, want 3", len(quests))
	}

	var sawWithAR bool
	for _, q := range quests {
		if name, ok := q["pokestopName"].(string); !ok || name == "" {
			t.Errorf("quest entry missing pokestopName: %+v", q)
		}
		if qs, ok := q["questString"].(string); !ok || qs == "" {
			t.Errorf("quest entry missing translated questString: %+v", q)
		}
		if withAR, ok := q["withAR"].(bool); ok && withAR {
			sawWithAR = true
		}
	}
	if !sawWithAR {
		t.Errorf("expected at least one quest entry with withAR=true (the sample's third quest)")
	}
}

// TestEnrichForType_QuestSummary locks in the enrichForType dispatch added
// for this task: "questSummary" (and its raw webhook-type spelling
// "quest-summary") must resolve via enrichQuestSummary with the alias's
// TemplateType intact, matching the "incident"/"weatherchange" precedent
// from tasks 3-4.
func TestEnrichForType_QuestSummary(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "quest_summary", "stardust")

	r, err := ps.enrichForType("questSummary", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("questSummary", ...) error: %v`, err)
	}
	if r.templateType != "questSummary" {
		t.Errorf(`enrichForType("questSummary", ...).templateType = %q, want "questSummary"`, r.templateType)
	}

	r2, err := ps.enrichForType("quest-summary", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("quest-summary", ...) error: %v`, err)
	}
	if r2.templateType != "questSummary" {
		t.Errorf(`enrichForType("quest-summary", ...).templateType = %q, want "questSummary"`, r2.templateType)
	}

	// Other still-unimplemented derived names must keep erroring — this
	// task only wires "questSummary" (in addition to "incident" and
	// "weatherchange" from tasks 3-4).
	if _, err := ps.enrichForType("monsterChanged", raw, "en", false); err == nil {
		t.Errorf(`enrichForType("monsterChanged", ...) error = nil, want a "derived type not yet supported" error`)
	}
}

// TestEnrichWebhook_QuestSummary confirms the /api/dts/enrich-facing surface
// (EnrichWebhook) also resolves "questSummary" and returns the group's
// variables. newEnrichParityService doesn't wire a dtsRenderer, so this
// exercises the mergeEnrichment fallback branch rather than a full
// LayeredView — the base fields (asserted above via enrichQuestSummary
// directly) are what matters here; this test just proves the name resolves
// end-to-end through the public entry point without erroring.
func TestEnrichWebhook_QuestSummary(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "quest_summary", "stardust")

	vars, err := ps.EnrichWebhook("questSummary", raw, "en", "discord")
	if err != nil {
		t.Fatalf(`EnrichWebhook("questSummary", ...) error: %v`, err)
	}
	if got, _ := vars["rewardName"].(string); got != "1500 Stardust" {
		t.Errorf(`EnrichWebhook("questSummary", ...)["rewardName"] = %q, want "1500 Stardust"`, got)
	}
}

// TestProcessTestQuestSummary_EnqueuesQuestSummaryTemplate verifies the
// !poracle-test / /api/test dispatch path (wire type "quest_summary" — see
// resolveDTSTypeFromRaw): processTestQuestSummary must enqueue a RenderJob
// carrying TemplateType "questSummary", built via the shared
// renderJobFromEnrich helper like the other non-pokemon test handlers,
// grouping all three sample quests into one job.
func TestProcessTestQuestSummary_EnqueuesQuestSummaryTemplate(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "quest_summary", "stardust")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestQuestSummary(raw, target); err != nil {
		t.Fatalf("processTestQuestSummary error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if job.TemplateType != "questSummary" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "questSummary")
		}
		if job.AlertType != AlertTypeQuest {
			t.Errorf("job.AlertType = %q, want %q", job.AlertType, AlertTypeQuest)
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
		quests, ok := job.Enrichment["quests"].([]map[string]any)
		if !ok || len(quests) != 3 {
			t.Errorf("job.Enrichment[\"quests\"] = %v, want a 3-entry slice", job.Enrichment["quests"])
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}

// TestProcessTest_DispatchesQuestSummary verifies the top-level ProcessTest
// switch has an explicit "quest_summary" case (the wire spelling — see
// resolveDTSTypeFromRaw) routing to processTestQuestSummary. Source-grep
// because exercising ProcessTest end-to-end needs a fully-wired
// dtsRenderer/dispatcher (same convention as
// TestProcessTest_DispatchesIncident/TestProcessTest_DispatchesWeatherChange).
func TestProcessTest_DispatchesQuestSummary(t *testing.T) {
	src, err := os.ReadFile("test.go")
	if err != nil {
		t.Fatalf("read test.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(n, `case "quest_summary": return ps.processTestQuestSummary(raw, matchedUser)`) {
		t.Error(`test.go's ProcessTest switch missing: case "quest_summary": return ps.processTestQuestSummary(raw, matchedUser)`)
	}
}

// TestResolveDTSTypeFromRaw_QuestSummary locks in the wire-spelling→DTS-type
// mapping ProcessTest's CheckTemplate call depends on: without this explicit
// case, resolveDTSTypeFromRaw's default branch would return "quest_summary"
// unchanged, which doesn't match the registered "questSummary" DTS template
// type — CheckTemplate would fail to find it and every !poracle-test
// quest-summary,<id> / POST /api/test {"type":"quest_summary"} invocation
// would error out before ever reaching processTestQuestSummary.
func TestResolveDTSTypeFromRaw_QuestSummary(t *testing.T) {
	if got := resolveDTSTypeFromRaw("quest_summary", nil); got != "questSummary" {
		t.Errorf(`resolveDTSTypeFromRaw("quest_summary", nil) = %q, want "questSummary"`, got)
	}
}
