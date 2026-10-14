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
	r := personalshiny.NewRates(server.URL, "", 0)
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

// TestPersonalOnlyEvaluatedForValidatedHumans: personal bits are answered
// only for rules whose human passed human validation (enabled, profile,
// area/distance), so a webhook never hashes the accounts of humans the
// sighting could not alert anyway.
func TestPersonalOnlyEvaluatedForValidatedHumans(t *testing.T) {
	near := makeHuman("near")
	outside := makeHuman("outside")
	outside.Area = []string{"elsewhere"}
	far := makeHuman("far")
	far.Latitude, far.Longitude = 40.0, 10.0
	disabled := makeHuman("disabled")
	disabled.Enabled = false
	otherProfile := makeHuman("otherprofile")
	otherProfile.CurrentProfileNo = 2
	humans := map[string]*db.Human{"near": near, "outside": outside, "far": far, "disabled": disabled, "otherprofile": otherProfile}

	personalRule := func(id string) *db.MonsterTracking {
		r := makeMonster(id, 25)
		r.Personal = db.PersonalShiny
		return r
	}
	farRule := personalRule("far")
	farRule.Distance = 1000
	nearPVP := personalRule("near")
	nearPVP.PVPRankingLeague, nearPVP.PVPRankingWorst = 1500, 100
	nearPVP.Personal = db.PersonalBackground
	rules := []*db.MonsterTracking{
		personalRule("near"), nearPVP, personalRule("outside"), farRule,
		personalRule("disabled"), personalRule("otherprofile"),
	}
	st := makeTestState(rules, humans)

	asked := map[string]int{}
	m := &PokemonMatcher{PVPQueryMaxRank: 100}
	m.personalAllows = func(_ *ProcessedPokemon, humanID string, bits int) bool {
		asked[humanID]++
		return bits == db.PersonalShiny // background (the PVP rule) misses
	}
	p := personalPikachu(nil, true)
	p.PVPBestRank = map[int][]pvp.LeagueRank{1500: {{Rank: 10, CP: 1490, Caps: []int{50}}}}

	matched, _ := m.Match(p, st)
	if len(asked) != 1 || asked["near"] != 2 {
		t.Fatalf("personal evaluated for %v; want only near (basic + PVP rule)", asked)
	}
	if len(matched) != 1 || matched[0].ID != "near" || matched[0].PVPRankingLeague != 0 {
		t.Fatalf("matched = %+v; want near's basic rule only (PVP personal rule gated)", matched)
	}
}

// A known Ditto (pokemon_id 132 with a disguise) rolls on the third draw at
// 1/64 and never has a background. At encounter 6 it is shiny for
// "Player-ID:ABC/123" only (Golbat's pinned TestPersonalDittoShiny vector),
// even though the feed lists 132 as unsupported and gives the disguise
// rate 1 with a background.
func TestPersonalRulesKnownDitto(t *testing.T) {
	const feed = `{"rates":[{"pokemon":16,"form":0,"costume":0,"prediction":{"probability":1,"supported":true},"background":{"probability":1,"supported":true}}],"unsupportedPokemon":[132]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "", 0)
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	for i := 0; !r.Loaded(); i++ {
		if i > 2000 {
			t.Fatal("feed never loaded")
		}
		time.Sleep(time.Millisecond)
	}

	lucky := makeHuman("lucky")
	lucky.Accounts = []db.PlayerAccount{{PlayerID: "Player-ID:ABC/123"}}
	other := makeHuman("other")
	other.Accounts = []db.PlayerAccount{{PlayerID: " Trainer-é "}}
	humans := map[string]*db.Human{"lucky": lucky, "other": other}
	rule := func(id string, bits int) *db.MonsterTracking {
		m := makeMonster(id, 132)
		m.Personal = bits
		return m
	}
	st := makeTestState([]*db.MonsterTracking{
		rule("lucky", db.PersonalShiny),      // match
		rule("lucky", db.PersonalBackground), // a Ditto never has a background
		rule("other", db.PersonalShiny),      // not shiny for this player
		rule("other", db.PersonalBackground),
	}, humans)

	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "6", PokemonID: 132, DisplayPokemonID: 16, Latitude: 51.5, SeenType: "wild"})
	p := personalPikachu(personalshiny.NewChecker(ev, humans), true)
	p.PokemonID = 132
	matched, _ := (&PokemonMatcher{PVPQueryMaxRank: 100}).Match(p, st)
	if len(matched) != 1 || matched[0].ID != "lucky" {
		t.Fatalf("known Ditto matches = %+v; want lucky's personalshiny rule only", matched)
	}
}
