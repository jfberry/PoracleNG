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
		fallbackNotFound:     "Template not found: monster/discord/1/en",
		fallbackRenderError:  "Template render error: monster/discord/1/en",
		fallbackInvalidJSON:  "Template produced invalid JSON: monster/discord/1/en",
		fallbackLoadError:    "Template could not be loaded: monster/discord/1/en",
		fallbackCompileError: "Template compile error: monster/discord/1/en",
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

// A template that was selected but is broken must say so, not "not found":
// the operator has the right type/platform/id/language and needs to fix the
// template body (issue reported for a telegram monsterChanged entry whose
// content array had an unclosed block).
func TestRenderFallbackBrokenTemplate(t *testing.T) {
	cases := []struct {
		name  string
		entry DTSEntry
		want  string
	}{
		{
			"compile error",
			DTSEntry{Template: map[string]any{"content": []any{"{{#if original.iv}}", "was {{original.iv}}"}}},
			"Template compile error: ",
		},
		{
			"missing templateFile",
			DTSEntry{TemplateFile: "dts/does-not-exist.txt"},
			"Template could not be loaded: ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var entries []DTSEntry
			for _, typ := range []string{"monsterChanged", "raid"} {
				e := tc.entry
				e.Type, e.ID, e.Platform, e.Language, e.Default = typ, "1", "telegram", "en", true
				entries = append(entries, e)
			}
			r := newTestRenderer(t, entries)
			users := []webhook.MatchedUser{{ID: "u1", Type: "telegram:user", Template: "1", Language: "en"}}

			// Per-user path.
			got := fallbackContent(t, r.RenderPokemonChanged(fallbackTestEnrichment, nil, nil, nil, nil, users, nil, "ref", ""))
			if want := tc.want + "monsterChanged/telegram/1/en"; got != want {
				t.Errorf("per-user path: content = %q, want %q", got, want)
			}
			// Grouped path.
			got = fallbackContent(t, r.RenderAlert("raid", fallbackTestEnrichment, nil, nil, users, nil, "ref", ""))
			if want := tc.want + "raid/telegram/1/en"; got != want {
				t.Errorf("grouped path: content = %q, want %q", got, want)
			}
			if err := r.CheckTemplate("raid", "telegram", "1", "en"); err == nil || strings.Contains(err.Error(), "no DTS template found") {
				t.Errorf("CheckTemplate err = %v, want a broken-template error", err)
			}
		})
	}
}

// A broken monsterNoIv with no monster template to fall back to reports the
// compile error, not "not found" for monster.
func TestRenderFallbackBrokenMonsterNoIv(t *testing.T) {
	r := newTestRenderer(t, []DTSEntry{{
		Type: "monsterNoIv", ID: "1", Platform: "discord", Language: "en", Default: true,
		Template: map[string]any{"content": "{{#if a}}x"},
	}})
	users := []webhook.MatchedUser{{ID: "u1", Type: "discord:user", Template: "1", Language: "en"}}
	perUser := map[string]map[string]any{"u1": {}}
	got := fallbackContent(t, r.RenderPokemon(fallbackTestEnrichment, nil, perUser, nil, users, nil, false, "ref", ""))
	if want := "Template compile error: monsterNoIv/discord/1/en"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}
