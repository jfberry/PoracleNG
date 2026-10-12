package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func testChecker(t *testing.T, humans map[string]*db.Human) *personalshiny.Checker {
	t.Helper()
	feed := `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"background":{"probability":0.2,"supported":true}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "")
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	for i := 0; !r.Loaded(); i++ {
		if i > 2000 {
			t.Fatal("feed never loaded")
		}
		waitMillis(1)
	}
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 51.5, SeenType: "wild"})
	return personalshiny.NewChecker(ev, humans)
}

func TestAnnotatePersonal(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "main"}, {PlayerID: "0123456789abcdef"}}},
		"ch": {ID: "ch", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "group"}}},
		"u2": {ID: "u2"},
	}
	matched := []webhook.MatchedUser{{ID: "u1"}, {ID: "u1"}, {ID: "ch"}, {ID: "u2"}}
	// perUser is nil when PVP display is not configured; annotation must
	// still produce the fields.
	out := AnnotatePersonal(nil, matched, testChecker(t, humans))

	u1 := out["u1"]
	if u1["personalShiny"] != true || !slices.Equal(u1["personalShinyAccounts"].([]string), []string{"main"}) {
		t.Fatalf("u1 shiny fields = %v", u1)
	}
	if u1["personalBackground"] != true || !slices.Equal(u1["personalBackgroundAccounts"].([]string), []string{"…cdef"}) {
		t.Fatalf("u1 background fields = %v", u1)
	}
	if u1["hasLinkedAccounts"] != true {
		t.Fatal("u1 hasLinkedAccounts")
	}
	if !slices.Equal(out["ch"]["personalShinyAccounts"].([]string), []string{"group"}) {
		t.Fatalf("channel label leaked: %v", out["ch"])
	}
	if _, ok := out["u2"]; ok {
		t.Fatalf("user without accounts got personal fields: %v", out["u2"])
	}

	// Existing per-user maps are added to, not replaced.
	existing := map[string]map[string]any{"u1": {"pvpUserRanking": 5}}
	out = AnnotatePersonal(existing, matched, testChecker(t, humans))
	if out["u1"]["pvpUserRanking"] != 5 || out["u1"]["personalShiny"] != true {
		t.Fatalf("merge = %v", out["u1"])
	}

	// No evaluation (feed not loaded, or ineligible spawn): only
	// hasLinkedAccounts is known; the personal fields stay absent.
	out = AnnotatePersonal(nil, matched, personalshiny.NewChecker(nil, humans))
	if out["u1"]["hasLinkedAccounts"] != true {
		t.Fatalf("unevaluated hasLinkedAccounts = %v", out["u1"])
	}
	for _, k := range []string{"personalShiny", "personalShinyAccounts", "personalBackground", "personalBackgroundAccounts"} {
		if v, ok := out["u1"][k]; ok {
			t.Fatalf("unevaluated spawn set %s = %v", k, v)
		}
	}
	if _, ok := out["u2"]; ok {
		t.Fatalf("unevaluated: user without accounts got fields: %v", out["u2"])
	}

	// Nil checker (feature off): untouched.
	if got := AnnotatePersonal(nil, matched, nil); got != nil {
		t.Fatalf("nil checker = %v", got)
	}
}

func waitMillis(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }
