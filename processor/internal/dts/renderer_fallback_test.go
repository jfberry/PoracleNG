package dts

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// The user-visible fallback must say WHY the alert couldn't be rendered:
// a missing template, a template that failed to execute, and a template
// that produced invalid JSON need different fixes from the operator.

func fallbackContent(t *testing.T, jobs []webhook.DeliveryJob) string {
	t.Helper()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg := parseMessage(t, jobs[0].Message)
	content, _ := msg["content"].(string)
	return content
}

var fallbackTestEnrichment = map[string]any{
	"latitude":  51.0,
	"longitude": 13.0,
	"tth":       map[string]any{"hours": 0, "minutes": 30, "seconds": 0, "totalSeconds": 1800},
}

func TestRenderFallbackReasons(t *testing.T) {
	cases := []struct {
		name     string
		template any
		want     string // reason prefix; the type/platform/id/lang suffix is checked per path
	}{
		{"render error", map[string]any{"content": "{{#if}}x{{/if}}"}, "Template render error: "},
		{"invalid JSON", `{"content": "unterminated`, "Template produced invalid JSON: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRenderer(t, []DTSEntry{
				{Type: "raid", ID: "1", Platform: "discord", Default: true, Template: tc.template},
				{Type: "monster", ID: "1", Platform: "discord", Default: true, Template: tc.template},
			})
			r.SetErrorNoticer(func(string, string) {})
			users := []webhook.MatchedUser{{ID: "u1", Type: "discord:user", Template: "1", Language: "en"}}

			// Grouped path (RenderAlert).
			got := fallbackContent(t, r.RenderAlert("raid", fallbackTestEnrichment, nil, nil, users, nil, "ref", ""))
			if want := tc.want + "raid/discord/1/en"; got != want {
				t.Errorf("grouped path: content = %q, want %q", got, want)
			}
			// Per-user path (non-nil per-user enrichment).
			perUser := map[string]map[string]any{"u1": {}}
			got = fallbackContent(t, r.RenderPokemon(fallbackTestEnrichment, nil, perUser, nil, users, nil, true, "ref", ""))
			if want := tc.want + "monster/discord/1/en"; got != want {
				t.Errorf("per-user path: content = %q, want %q", got, want)
			}
		})
	}
}

func TestRenderFallbackTemplateNotFound(t *testing.T) {
	r := newTestRenderer(t, []DTSEntry{{
		Type: "egg", ID: "1", Platform: "discord", Default: true, Template: map[string]any{"content": "x"},
	}})
	users := []webhook.MatchedUser{{ID: "chan1", Type: "discord:channel", Template: "1", Language: "en"}}
	got := fallbackContent(t, r.RenderAlert("raid", fallbackTestEnrichment, nil, nil, users, nil, "ref", ""))
	if !strings.HasPrefix(got, "Template not found: raid/discord/") {
		t.Errorf("content = %q, want a Template not found message", got)
	}
}

func TestFallbackMessageReasonText(t *testing.T) {
	cases := map[fallbackReason]string{
		fallbackNotFound:    "Template not found: monster/discord/1/en",
		fallbackRenderError: "Template render error: monster/discord/1/en",
		fallbackInvalidJSON: "Template produced invalid JSON: monster/discord/1/en",
	}
	for reason, want := range cases {
		raw := fallbackMessageRaw(reason, "monster", "discord", "1", "en")
		if c, _ := parseMessage(t, raw)["content"].(string); c != want {
			t.Errorf("fallbackMessageRaw(%v) content = %q, want %q", reason, c, want)
		}
		str := fallbackMessage(reason, "monster", "discord", "1", "en")
		if c, _ := parseMessage(t, []byte(str))["content"].(string); c != want {
			t.Errorf("fallbackMessage(%v) content = %q, want %q", reason, c, want)
		}
	}
}
