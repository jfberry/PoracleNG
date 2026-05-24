package autocomplete

import (
	"context"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func TestRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	r.Register("tracking", func(ctx context.Context, deps *bot.BotDeps, userID string, hint UserStateHint) ([]Choice, error) {
		return []Choice{{Label: "test", Value: "1"}}, nil
	})
	fn := r.Lookup("tracking")
	if fn == nil {
		t.Fatal("nil lister")
	}
	out, _ := fn(context.Background(), nil, "u", UserStateHint{})
	if len(out) != 1 {
		t.Errorf("got %d choices", len(out))
	}
}

func TestFilterAndCap(t *testing.T) {
	choices := []Choice{
		{Label: "Pikachu [id:1]", Value: "1"},
		{Label: "Bulbasaur [id:2]", Value: "2"},
	}
	got := FilterAndCap(choices, "pika")
	if len(got) != 1 {
		t.Errorf("expected 1 match, got %d", len(got))
	}
}

func TestFilterAndCapTruncates(t *testing.T) {
	longLabel := strings.Repeat("x", 200) + " [id:99]"
	choices := []Choice{{Label: longLabel, Value: "99"}}
	got := FilterAndCap(choices, "")
	if len(got[0].Name) > 100 {
		t.Errorf("not truncated: %d", len(got[0].Name))
	}
	if !strings.HasSuffix(got[0].Name, " [id:99]") {
		t.Errorf("suffix lost: %q", got[0].Name)
	}
}

// Regression: /untrack pokemon labels are "<rowtext> [id:N]" where the
// rowtext starts with the pokemon name. The old tail-only truncation hid
// the name under "…**" and left only the filter recitation visible.
// truncateChoiceLabel now preserves both the human-identifiable prefix
// AND the trailing [id:N] selector, eliding the middle.
func TestFilterAndCapPreservesPrefixAndIdSuffix(t *testing.T) {
	label := "Snover ** | iv: 90%-100% | cp: 0-9000 | level: 0-55 | stats: 0/0/0 - 15/15/15 | size: XXS-XXL [id:154]"
	got := FilterAndCap([]Choice{{Label: label, Value: "154"}}, "")
	if len(got) != 1 {
		t.Fatalf("got %d choices, want 1", len(got))
	}
	name := got[0].Name
	if len(name) > 100 {
		t.Errorf("not truncated: %d bytes", len(name))
	}
	if !strings.HasPrefix(name, "Snover") {
		t.Errorf("prefix lost: %q (want leading 'Snover')", name)
	}
	if !strings.HasSuffix(name, "[id:154]") {
		t.Errorf("suffix lost: %q", name)
	}
	if !strings.Contains(name, "…") {
		t.Errorf("expected an ellipsis marker in the middle: %q", name)
	}
}

// Profile labels use " [#N]" not " [id:N]" — the truncator should
// recognise both markers.
func TestFilterAndCapPreservesProfileSelector(t *testing.T) {
	label := "Profile " + strings.Repeat("very-long-name-", 10) + " [#3]"
	got := FilterAndCap([]Choice{{Label: label, Value: "3"}}, "")
	name := got[0].Name
	if !strings.HasPrefix(name, "Profile ") || !strings.HasSuffix(name, "[#3]") {
		t.Errorf("expected Profile…[#3], got %q", name)
	}
}

// Labels without a recognisable selector marker get the head-keep-tail-
// drop treatment (which is the historic shape but reversed).
func TestFilterAndCapNoMarkerKeepsHead(t *testing.T) {
	label := strings.Repeat("abcdefghij", 15) // 150 bytes, no marker
	got := FilterAndCap([]Choice{{Label: label, Value: "x"}}, "")
	name := got[0].Name
	if len(name) > 100 {
		t.Errorf("not truncated: %d", len(name))
	}
	if !strings.HasPrefix(name, "abcdefghij") {
		t.Errorf("head not preserved: %q", name)
	}
	if !strings.HasSuffix(name, "…") {
		t.Errorf("expected trailing ellipsis: %q", name)
	}
}
