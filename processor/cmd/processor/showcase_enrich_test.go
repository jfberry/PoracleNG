package main

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// TestEnrichForType_Showcase is the direct repro for the reported bug: POST
// /api/dts/enrich (which flows through enrichForType) returned 500
// "unsupported webhook type: showcase" because the non-derived switch had no
// showcase case — even though the dtsmap types map, the ?dtsType=showcase
// testdata bucket, and the showcase DTS field set all expose it. enrichForType
// must now enrich showcase like every other DTS type name: its own
// webhook.ShowcaseWebhook shape, rendered under templateType "showcase"
// (leaderboard + featured focus), sharing the exact enrichment core the live
// ProcessShowcase / processTestShowcase paths use.
func TestEnrichForType_Showcase(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "showcase", "type")

	r, err := ps.enrichForType("showcase", raw, "en", false)
	if err != nil {
		t.Fatalf(`enrichForType("showcase", ...) error: %v`, err)
	}
	if r.templateType != "showcase" {
		t.Errorf(`templateType = %q, want "showcase"`, r.templateType)
	}
	if len(r.base) == 0 {
		t.Errorf("base enrichment is empty, want populated map")
	}
	if r.perLang == nil {
		t.Fatal("perLang is nil, want the showcase leaderboard + focus fields")
	}

	// Leaderboard fields (InvasionTranslate -> translateShowcaseRankings).
	if present, _ := r.perLang["showcasePresent"].(bool); !present {
		t.Errorf("perLang[showcasePresent] = %v, want true (the sample carries showcase_rankings)", r.perLang["showcasePresent"])
	}
	entries, ok := r.perLang["showcase"].([]map[string]any)
	if !ok || len(entries) == 0 {
		t.Errorf("perLang[showcase] = %v (%T), want a non-empty contestant list", r.perLang["showcase"], r.perLang["showcase"])
	}

	// Focus fields (ShowcaseFocusTranslate) — the sample features a Type focus.
	if present, _ := r.perLang["showcaseFocusPresent"].(bool); !present {
		t.Errorf("perLang[showcaseFocusPresent] = %v, want true (the sample carries showcase_focus)", r.perLang["showcaseFocusPresent"])
	}
}

// TestEnrichWebhook_Showcase covers the exact code path the API endpoint POST
// /api/dts/enrich runs (EnrichWebhook -> enrichForType -> flatten to
// variables). Before the fix this returned the "unsupported webhook type:
// showcase" error the editor surfaced as a 500; now it must return a populated
// variable map like every other DTS type.
func TestEnrichWebhook_Showcase(t *testing.T) {
	ps := newEnrichParityService(t)
	raw := loadTestdataSample(t, "showcase", "type")

	vars, err := ps.EnrichWebhook("showcase", raw, "en", "discord")
	if err != nil {
		t.Fatalf(`EnrichWebhook("showcase", ...) error: %v`, err)
	}
	if len(vars) == 0 {
		t.Errorf("EnrichWebhook returned no variables, want a populated map")
	}
}

// TestProcessTestShowcase_EnqueuesShowcaseTemplate guards the refactor of
// processTestShowcase onto the shared enrichShowcase + renderJobFromEnrich
// path: the enqueued RenderJob must still carry AlertType "incident" (showcases
// track/rate-limit/block as incidents) and TemplateType "showcase" (the
// dedicated leaderboard display), with the showcase leaderboard data present in
// per-language enrichment.
func TestProcessTestShowcase_EnqueuesShowcaseTemplate(t *testing.T) {
	ps := newEnrichParityService(t)
	ps.renderCh = make(chan RenderJob, 1)

	raw := loadTestdataSample(t, "showcase", "type")
	target := webhook.MatchedUser{ID: "42", Language: "en"}

	if err := ps.processTestShowcase(raw, target); err != nil {
		t.Fatalf("processTestShowcase error: %v", err)
	}

	select {
	case job := <-ps.renderCh:
		if job.AlertType != "incident" {
			t.Errorf("job.AlertType = %q, want %q (showcases track/limit as incidents)", job.AlertType, "incident")
		}
		if job.TemplateType != "showcase" {
			t.Errorf("job.TemplateType = %q, want %q", job.TemplateType, "showcase")
		}
		if len(job.MatchedUsers) != 1 || job.MatchedUsers[0].ID != "42" {
			t.Errorf("job.MatchedUsers = %+v, want single target 42", job.MatchedUsers)
		}
		lang := job.PerLangEnrichment["en"]
		if present, _ := lang["showcasePresent"].(bool); !present {
			t.Errorf("job perLang[en][showcasePresent] = %v, want true", lang["showcasePresent"])
		}
	default:
		t.Fatal("expected a RenderJob to be enqueued on renderCh")
	}
}
