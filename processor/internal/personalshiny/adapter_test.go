package personalshiny

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

var testPlayers = []string{"0123456789abcdef", "Player-ID:ABC/123", " Trainer-é "}

func accounts(ids ...string) []db.PlayerAccount {
	out := make([]db.PlayerAccount, len(ids))
	for i, id := range ids {
		out[i] = db.PlayerAccount{PlayerID: id}
	}
	return out
}

func playerIDs(as []db.PlayerAccount) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.PlayerID
	}
	return out
}

func loadedRates(t *testing.T, feed string) *Rates {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := NewRates(server.URL, "")
	if err := r.refreshShinyRates(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func wildPikachu() *webhook.PokemonWebhook {
	return &webhook.PokemonWebhook{EncounterID: "1", PokemonID: 25, Latitude: 33.45, Longitude: -112.07, SeenType: "wild"}
}

// Mirrors Golbat's TestPersonalShinyScan annotation block: same feed, same
// spawn (Phoenix, so the America/Phoenix overrides apply), same players.
func TestEvaluateMatchesGolbatAnnotate(t *testing.T) {
	r := loadedRates(t, testFeed)
	ev := r.Evaluate(wildPikachu())
	if ev == nil {
		t.Fatal("eligible wild pokemon not evaluated")
	}
	res := ev.For(accounts(testPlayers...))
	if got := playerIDs(res.Shiny); !slices.Equal(got, []string{" Trainer-é "}) {
		t.Fatalf("shiny = %v", got)
	}
	if got := playerIDs(res.Background); !slices.Equal(got, []string{"0123456789abcdef"}) {
		t.Fatalf("background = %v", got)
	}
}

// Mirrors Golbat's TestPersonalDrawOrder.
func TestPersonalDrawOrder(t *testing.T) {
	background := &shinyPrediction{Probability: 0.2, Supported: true}
	r := NewRates("x", "")
	r.snapshot.Store(&shinyRates{variants: map[shinyVariant]shinyRate{
		{25, 0, 0}: {Pokemon: 25, Prediction: shinyPrediction{Probability: 0.1, Supported: true}, Background: background},
		{26, 0, 0}: {Pokemon: 26, Prediction: shinyPrediction{Supported: false}, Background: background},
	}, backgroundMaximum: map[int16]uint32{25: background.cutoff(0), 26: background.cutoff(0)}})
	at := func(id int) Result {
		return r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: id, SeenType: "wild"}).For(accounts(testPlayers...))
	}
	// Shiny-capable: shiny is the first draw, background the second.
	if res := at(25); !slices.Equal(playerIDs(res.Shiny), []string{" Trainer-é "}) || !slices.Equal(playerIDs(res.Background), []string{"0123456789abcdef"}) {
		t.Fatalf("capable: %+v", res)
	}
	// Never shiny: no shiny draw, so the background is the first draw.
	if res := at(26); len(res.Shiny) != 0 || !slices.Equal(playerIDs(res.Background), []string{" Trainer-é "}) {
		t.Fatalf("not capable: %+v", res)
	}
	// backgroundRoll (Golbat's filter path) agrees with the annotate path.
	for _, id := range testPlayers {
		want := id == " Trainer-é "
		if got := backgroundRoll(false, newShinyPlayer(id), 1) < background.cutoff(0); got != want {
			t.Errorf("backgroundRoll %q = %v, want %v", id, got, want)
		}
	}
}

func TestEvaluateEligibility(t *testing.T) {
	r := loadedRates(t, testFeed)
	for name, mutate := range map[string]func(p *webhook.PokemonWebhook){
		"ditto":            func(p *webhook.PokemonWebhook) { p.PokemonID = 132 },
		"revealed ditto":   func(p *webhook.PokemonWebhook) { p.PokemonID = 132; p.DisplayPokemonID = 16 },
		"disguised":        func(p *webhook.PokemonWebhook) { p.DisplayPokemonID = 16 },
		"nearby stop":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_stop" },
		"nearby cell":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_cell" },
		"no seen type":     func(p *webhook.PokemonWebhook) { p.SeenType = "" },
		"costume 255":      func(p *webhook.PokemonWebhook) { p.Costume = 255 },
		"bad encounter id": func(p *webhook.PokemonWebhook) { p.EncounterID = "abc" },
	} {
		p := wildPikachu()
		mutate(p)
		if ev := r.Evaluate(p); ev != nil {
			t.Errorf("%s: evaluated, want nil", name)
		}
	}
	p := wildPikachu()
	p.SeenType = "encounter"
	p.DisplayPokemonID = 25 // display equal to species is not a disguise
	if r.Evaluate(p) == nil {
		t.Error("encounter with display == species: want eligible")
	}
}

func TestEvaluateUnloadedAndUnknownVariant(t *testing.T) {
	if NewRates("x", "").Evaluate(wildPikachu()) != nil {
		t.Fatal("evaluated before the feed loaded (must fail closed)")
	}
	var nilRates *Rates
	if nilRates.Evaluate(wildPikachu()) != nil {
		t.Fatal("nil Rates evaluated")
	}
	// Unknown variant: 1/512 shiny, no background (Golbat's default).
	r := loadedRates(t, `{"rates":[]}`)
	ev := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: 1, SeenType: "wild"})
	if ev == nil || !ev.shinyCapable || ev.shinyCutoff != defaultShinyCutoff || ev.backgroundCutoff != 0 {
		t.Fatalf("unknown variant evaluation = %+v", ev)
	}
}

func TestCheckerAllowsAndMemo(t *testing.T) {
	r := loadedRates(t, testFeed)
	humans := map[string]*db.Human{
		"shinyuser": {ID: "shinyuser", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "main"}}},
		"bguser":    {ID: "bguser", Accounts: []db.PlayerAccount{{PlayerID: "0123456789abcdef"}}},
		"channel":   {ID: "channel", Accounts: []db.PlayerAccount{{PlayerID: " Trainer-é ", Label: "group"}, {PlayerID: "0123456789abcdef"}}},
		"noaccts":   {ID: "noaccts"},
	}
	c := NewChecker(r.Evaluate(wildPikachu()), humans)
	for _, tc := range []struct {
		human string
		bits  int
		want  bool
	}{
		{"shinyuser", db.PersonalShiny, true},
		{"shinyuser", db.PersonalBackground, false},
		{"shinyuser", db.PersonalShiny | db.PersonalBackground, false},
		{"bguser", db.PersonalBackground, true},
		{"channel", db.PersonalShiny | db.PersonalBackground, true},
		{"noaccts", db.PersonalShiny, false},
		{"missing", db.PersonalShiny, false},
		{"shinyuser", db.PersonalShiny | 4, false}, // reserved bit: never matches
	} {
		if got := c.Allows(tc.human, tc.bits); got != tc.want {
			t.Errorf("Allows(%s, %d) = %v, want %v", tc.human, tc.bits, got, tc.want)
		}
	}
	// Same player ID on two humans: each sees its own label.
	a, _ := c.Result("shinyuser")
	b, _ := c.Result("channel")
	if a.Shiny[0].Label != "main" || b.Shiny[0].Label != "group" {
		t.Fatalf("labels leaked across humans: %+v / %+v", a.Shiny, b.Shiny)
	}
	if _, ok := c.Result("noaccts"); ok {
		t.Fatal("human without accounts reported as linked")
	}
	var nilChecker *Checker
	if nilChecker.Allows("shinyuser", db.PersonalShiny) {
		t.Fatal("nil checker allowed a personal rule")
	}
	// Feed not loaded: linked humans still report linked, but nothing matches.
	c = NewChecker(nil, humans)
	if c.Allows("shinyuser", db.PersonalShiny) {
		t.Fatal("matched without an evaluation")
	}
	if res, ok := c.Result("shinyuser"); !ok || len(res.Shiny) != 0 {
		t.Fatalf("unloaded Result = %+v, %v", res, ok)
	}
}

func TestShinyVariantAndBoosted(t *testing.T) {
	r := loadedRates(t, testFeed)
	if s, p, ok := r.ShinyVariant(25, 0, 0, 33.45, -112.07); !ok || !s || p != 0.05 {
		t.Fatalf("Phoenix override = %v %v %v", s, p, ok)
	}
	if s, p, ok := r.ShinyVariant(25, 0, 0, 51.5, 0); !ok || !s || p != 0.1 {
		t.Fatalf("base = %v %v %v", s, p, ok)
	}
	if _, _, ok := r.ShinyVariant(1, 0, 0, 0, 0); ok {
		t.Fatal("unlisted variant reported known")
	}
	if _, _, ok := NewRates("x", "").ShinyVariant(25, 0, 0, 0, 0); ok {
		t.Fatal("unloaded feed reported known")
	}
	got := r.Boosted(51.5, 0)
	if len(got) != 1 || got[0].PokemonID != 25 || got[0].OneIn != 10 {
		t.Fatalf("Boosted = %+v", got)
	}
}

func TestDisplayName(t *testing.T) {
	for _, tc := range []struct {
		a    db.PlayerAccount
		want string
	}{
		{db.PlayerAccount{PlayerID: "0123456789abcdef", Label: "main"}, "main"},
		{db.PlayerAccount{PlayerID: "0123456789abcdef"}, "…cdef"},
		{db.PlayerAccount{PlayerID: "abc"}, "abc"},
		{db.PlayerAccount{PlayerID: " Trainer-é "}, "…r-é "},
	} {
		if got := DisplayName(tc.a); got != tc.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", tc.a, got, tc.want)
		}
	}
}
