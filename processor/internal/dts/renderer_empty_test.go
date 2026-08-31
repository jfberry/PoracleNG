package dts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestRenderedMessageIsEmpty(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		empty bool
	}{
		{"empty object", `{}`, true},
		{"empty content", `{"content":""}`, true},
		{"whitespace content", `{"content":"  \n\t"}`, true},
		{"non-content keys only", `{"parse_mode":"Markdown","webpage_preview":true,"content":""}`, true},
		{"empty embed object", `{"embed":{}}`, true},
		{"empty embeds array", `{"embeds":[]}`, true},
		{"location false only", `{"location":false,"content":""}`, true},
		{"text content", `{"content":"hi"}`, false},
		{"embed with fields", `{"embed":{"title":"x"}}`, false},
		{"embeds with entry", `{"embeds":[{"title":"x"}]}`, false},
		{"components present", `{"components":[{"type":1}]}`, false},
		{"telegram sticker", `{"sticker":"CAAD123","content":""}`, false},
		{"telegram photo", `{"photo":"https://example.com/x.png"}`, false},
		{"telegram location true", `{"location":true,"content":""}`, false},
		{"telegram venue", `{"venue":{"title":"a","address":"b"},"content":""}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderedMessageIsEmpty(json.RawMessage(tc.raw))
			if got != tc.empty {
				t.Errorf("renderedMessageIsEmpty(%s) = %v, want %v", tc.raw, got, tc.empty)
			}
		})
	}
}

// A template that renders to valid-but-empty JSON (e.g. {"content":""})
// must not reach delivery — Discord rejects it with 50006 ("Cannot send an
// empty message") and enough of those in a row auto-disable the target.
// Like the invalid-JSON case, the renderer substitutes a visible fallback
// message and fires the operator noticer.

func TestRenderAlertEmptyRenderFallsBack(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "raid",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": ""},
		},
	}
	r := newTestRenderer(t, entries)

	var noticeKeys []string
	var noticeMsgs []string
	r.SetErrorNoticer(func(key, msg string) {
		noticeKeys = append(noticeKeys, key)
		noticeMsgs = append(noticeMsgs, msg)
	})

	enrichment := map[string]any{
		"latitude":  51.0,
		"longitude": 13.0,
		"tth":       map[string]any{"hours": 0, "minutes": 30, "seconds": 0, "totalSeconds": 1800},
	}
	users := []webhook.MatchedUser{
		{ID: "chan1", Name: "TestChannel", Type: "discord:channel", Template: "1", Language: "en"},
	}

	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "raid-ref", "")

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg := parseMessage(t, jobs[0].Message)
	content, _ := msg["content"].(string)
	if strings.TrimSpace(content) == "" {
		t.Errorf("empty render reached the delivery job; expected fallback content, got %q", string(jobs[0].Message))
	}
	if !strings.Contains(content, "raid/discord/1/en") {
		t.Errorf("fallback content should identify the template, got %q", content)
	}

	if len(noticeKeys) != 1 {
		t.Fatalf("expected 1 notice, got %d (%v)", len(noticeKeys), noticeKeys)
	}
	if noticeKeys[0] != "dts.empty:raid:discord:en:1" {
		t.Errorf("unexpected notice key %q", noticeKeys[0])
	}
	if !strings.Contains(noticeMsgs[0], "empty message") {
		t.Errorf("notice should mention empty message, got %q", noticeMsgs[0])
	}
}

func TestRenderPokemonEmptyRenderFallsBack(t *testing.T) {
	entries := []DTSEntry{
		{
			Type:     "monster",
			ID:       "1",
			Platform: "discord",
			Default:  true,
			Template: map[string]any{"content": "{{missingField}}"},
		},
	}
	r := newTestRenderer(t, entries)

	var noticeKeys []string
	r.SetErrorNoticer(func(key, msg string) {
		noticeKeys = append(noticeKeys, key)
	})

	enrichment := map[string]any{
		"latitude":  51.0,
		"longitude": 13.0,
		"tth":       map[string]any{"hours": 1, "minutes": 0, "seconds": 0, "totalSeconds": 3600},
	}
	users := []webhook.MatchedUser{
		{ID: "user1", Name: "TestUser", Type: "discord:user", Template: "1", Language: "en"},
	}

	// Non-nil per-user enrichment forces the per-user render path (nil would
	// take the grouped path, which TestRenderAlertEmptyRenderFallsBack covers).
	perUser := map[string]map[string]any{"user1": {}}

	jobs := r.RenderPokemon(enrichment, nil, perUser, nil, users, nil, true, "test-ref", "")

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	msg := parseMessage(t, jobs[0].Message)
	content, _ := msg["content"].(string)
	if strings.TrimSpace(content) == "" {
		t.Errorf("empty render reached the delivery job; expected fallback content, got %q", string(jobs[0].Message))
	}

	if len(noticeKeys) != 1 {
		t.Fatalf("expected 1 notice, got %d (%v)", len(noticeKeys), noticeKeys)
	}
	if noticeKeys[0] != "dts.empty:monster:discord:en:1" {
		t.Errorf("unexpected notice key %q", noticeKeys[0])
	}
}
