package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// At encounter 1 with a 1/10 rate, Golbat's vectors make " Trainer-é " shiny
// and "0123456789abcdef" not (see personalshiny's parity tests).
func personalShinyChecker(t *testing.T, humans map[string]*db.Human) *personalshiny.Checker {
	t.Helper()
	feed := `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "")
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	for i := 0; !r.Loaded(); i++ {
		if i > 2000 {
			t.Fatal("feed never loaded")
		}
		time.Sleep(time.Millisecond)
	}
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, SeenType: "wild"})
	return personalshiny.NewChecker(ev, humans)
}

func ids(users []webhook.MatchedUser) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.ID
	}
	return out
}

func TestSplitPersonalShiny(t *testing.T) {
	humans := map[string]*db.Human{
		"shiny": {ID: "shiny", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é "}}},
		"plain": {ID: "plain", Accounts: []db.PlayerAccount{{PlayerID: "0123456789abcdef"}}},
		"none":  {ID: "none"},
	}
	// "shiny" matched by two rules (pokemon matching keeps one entry per rule).
	matched := []webhook.MatchedUser{{ID: "shiny"}, {ID: "plain"}, {ID: "shiny"}, {ID: "none"}}

	shiny, others := splitPersonalShiny(matched, personalShinyChecker(t, humans))
	if got := ids(shiny); len(got) != 2 || got[0] != "shiny" || got[1] != "shiny" {
		t.Fatalf("shiny = %v, want both entries of the shiny user", got)
	}
	if got := ids(others); len(got) != 2 || got[0] != "plain" || got[1] != "none" {
		t.Fatalf("others = %v", got)
	}

	// Feature off / spawn not evaluated: nobody is split out.
	for name, c := range map[string]*personalshiny.Checker{"nil": nil, "unevaluated": personalshiny.NewChecker(nil, humans)} {
		shiny, others := splitPersonalShiny(matched, c)
		if len(shiny) != 0 || len(others) != len(matched) {
			t.Errorf("%s checker: shiny=%v others=%v", name, ids(shiny), ids(others))
		}
	}
}

func TestWithoutUsers(t *testing.T) {
	got := withoutUsers(
		[]webhook.MatchedUser{{ID: "a"}, {ID: "b"}, {ID: "a"}, {ID: "c"}},
		[]webhook.MatchedUser{{ID: "a"}},
	)
	if g := ids(got); len(g) != 2 || g[0] != "b" || g[1] != "c" {
		t.Fatalf("withoutUsers = %v", g)
	}
	if got := withoutUsers([]webhook.MatchedUser{{ID: "a"}}, []webhook.MatchedUser{{ID: "a"}}); len(got) != 0 {
		t.Fatalf("all removed = %v", ids(got))
	}
}

// Shiny users get their own job carrying the shiny enrichment; everyone else
// keeps the base enrichment. Covers both dispatch paths.
func TestDispatchPokemonAlert_PersonalShinySplit(t *testing.T) {
	for _, withDispatcher := range []bool{true, false} {
		ps, ch, _ := minimalProcessor(t)
		if !withDispatcher {
			ps.dispatcher = nil
		}
		base := map[string]any{"imgUrl": "normal"}
		shinyEnr := map[string]any{"imgUrl": "shiny"}
		shinyUser := webhook.MatchedUser{ID: "shiny", Type: "discord:user"}
		other := webhook.MatchedUser{ID: "other", Type: "discord:user"}

		ps.dispatchPokemonAlert(pokemonDispatchInput{
			encounterID: "enc-personal",
			matched:     []webhook.MatchedUser{shinyUser, other},
			enrichment:  base,
			shiny:       &shinyAlert{users: []webhook.MatchedUser{shinyUser}, enrichment: shinyEnr},
		})

		got := map[string]string{}
		for len(ch) > 0 {
			j := <-ch
			for _, u := range j.MatchedUsers {
				got[u.ID] = j.Enrichment["imgUrl"].(string)
			}
			if j.ReplyKey != "enc-personal" {
				t.Errorf("dispatcher=%v: ReplyKey %q lost on split job", withDispatcher, j.ReplyKey)
			}
		}
		if got["shiny"] != "shiny" || got["other"] != "normal" || len(got) != 2 {
			t.Errorf("dispatcher=%v: users→enrichment = %v", withDispatcher, got)
		}
	}
}
