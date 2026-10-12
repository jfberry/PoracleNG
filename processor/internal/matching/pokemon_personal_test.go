package matching

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Feed and spawn as in personalshiny's Golbat parity test: at encounter 1,
// " Trainer-é " rolls shiny and "0123456789abcdef" rolls the background.
const personalFeed = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"background":{"probability":0.2,"supported":true}}]}`

func personalChecker(t *testing.T, humans map[string]*db.Human, loaded bool) *personalshiny.Checker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(personalFeed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "")
	if loaded {
		r.Start(context.Background())
		t.Cleanup(r.Stop)
		for i := 0; !r.Loaded(); i++ {
			if i > 2000 {
				t.Fatal("feed never loaded")
			}
			time.Sleep(time.Millisecond)
		}
	}
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 51.5, SeenType: "wild"})
	return personalshiny.NewChecker(ev, humans)
}

func personalPikachu(c *personalshiny.Checker, encountered bool) *ProcessedPokemon {
	p := &ProcessedPokemon{
		PokemonID: 25, IV: -1, RarityGroup: 1, TTHSeconds: 600, Latitude: 51.5, Longitude: 0,
		Encountered: encountered,
		PVPBestRank: map[int][]pvp.LeagueRank{}, PVPEvoData: map[int]map[int][]pvp.LeagueRank{},
		Personal: c,
	}
	if encountered {
		p.IV, p.CP, p.Level = 50, 500, 20
	}
	return p
}

func TestPersonalRulesMatch(t *testing.T) {
	shinyHuman := makeHuman("shiny")
	shinyHuman.Accounts = []db.PlayerAccount{{PlayerID: " Trainer-é "}}
	bgHuman := makeHuman("bg")
	bgHuman.Accounts = []db.PlayerAccount{{PlayerID: "0123456789abcdef"}}
	plain := makeHuman("plain")
	humans := map[string]*db.Human{"shiny": shinyHuman, "bg": bgHuman, "plain": plain}

	rule := func(id string, bits int) *db.MonsterTracking {
		m := makeMonster(id, 25)
		m.Personal = bits
		return m
	}
	rules := []*db.MonsterTracking{
		rule("shiny", db.PersonalShiny),                    // match
		rule("shiny", db.PersonalBackground),               // no
		rule("bg", db.PersonalBackground),                  // match
		rule("bg", db.PersonalShiny|db.PersonalBackground), // no
		rule("plain", db.PersonalShiny),                    // no accounts: no
		rule("plain", 0),                                   // ordinary: match
	}
	st := makeTestState(rules, humans)
	m := &PokemonMatcher{PVPQueryMaxRank: 100}

	matched, _ := m.Match(personalPikachu(personalChecker(t, humans, true), false), st)
	got := map[string]int{}
	for _, u := range matched {
		got[u.ID]++
	}
	if got["shiny"] != 1 || got["bg"] != 1 || got["plain"] != 1 || len(matched) != 3 {
		t.Fatalf("unencountered matches = %v (personal is not an encounter-only filter)", got)
	}

	// Feed not loaded: personal rules match nothing, ordinary rules still do.
	matched, _ = m.Match(personalPikachu(personalChecker(t, humans, false), true), st)
	if len(matched) != 1 || matched[0].ID != "plain" {
		t.Fatalf("unloaded feed matches = %+v", matched)
	}

	// No checker at all (feature off): same.
	matched, _ = m.Match(personalPikachu(nil, true), st)
	if len(matched) != 1 || matched[0].ID != "plain" {
		t.Fatalf("nil checker matches = %+v", matched)
	}
}
