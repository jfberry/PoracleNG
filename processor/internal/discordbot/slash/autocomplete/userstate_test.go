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
	if !strings.HasSuffix(got[0].Name, "[id:99]") {
		t.Errorf("suffix lost: %q", got[0].Name)
	}
}
