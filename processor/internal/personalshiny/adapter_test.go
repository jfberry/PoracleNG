package personalshiny

import (
	"context"
	"crypto/sha1" //nolint:gosec // see thirdDraw
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny/javarandom"
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
	r := NewRates(server.URL, "", 0)
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

// Mirrors Golbat's TestPersonalDrawOrder. Poracle: the filter path
// (shinyScan.match) is not copied; backgroundRoll stands in for it.
func TestPersonalDrawOrder(t *testing.T) {
	background := &shinyPrediction{Probability: 0.2, Supported: true}
	r := NewRates("x", "", 0)
	r.snapshot.Store(newShinyRates([]shinyRate{
		{Pokemon: 25, Prediction: shinyPrediction{Probability: 0.1, Supported: true}, Background: background},
		{Pokemon: 26, Prediction: shinyPrediction{Supported: false}, Background: background},
		{Pokemon: 29, Prediction: shinyPrediction{Probability: 0, Supported: true}, Background: background},
	}, nil, nil))
	at := func(id int) Result {
		return r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: id, SeenType: "wild"}).For(accounts(testPlayers...))
	}
	// Shiny-capable: shiny is the first draw, background the second.
	if res := at(25); !slices.Equal(playerIDs(res.Shiny), []string{" Trainer-é "}) || !slices.Equal(playerIDs(res.Background), []string{"0123456789abcdef"}) {
		t.Fatalf("capable: %+v", res)
	}
	// Never shiny, whether unsupported or at probability 0: no shiny draw,
	// so the background is the first draw.
	for _, pokemon := range []int{26, 29} {
		if res := at(pokemon); len(res.Shiny) != 0 || !slices.Equal(playerIDs(res.Background), []string{" Trainer-é "}) {
			t.Fatalf("%d not capable: %+v", pokemon, res)
		}
	}
	// backgroundRoll (Golbat's filter path) agrees with the annotate path.
	for _, id := range testPlayers {
		want := id == " Trainer-é "
		if got := backgroundRoll(false, newShinyPlayer(id), 1) < background.cutoff(0); got != want {
			t.Errorf("backgroundRoll %q = %v, want %v", id, got, want)
		}
	}
}

// Mirrors Golbat's TestShinyCapabilityIgnoresTimezone, through Evaluate
// with the webhook's location.
func TestShinyCapabilityIgnoresTimezone(t *testing.T) {
	// Capability comes from the base rate; a timezone override changes only
	// the cutoff. Rolls as in TestPersonalDrawOrder.
	r := NewRates("x", "", 0)
	if z := r.zone(33.45, -112.07); z != "America/Phoenix" {
		t.Skipf("timezone data changed: %s", z)
	}
	background := &shinyPrediction{Probability: 0.2, Supported: true}
	phoenix := func(p shinyPrediction) map[string]shinyPrediction {
		return map[string]shinyPrediction{"America/Phoenix": p}
	}
	r.snapshot.Store(newShinyRates([]shinyRate{
		// Never shiny, even where an override claims otherwise.
		{Pokemon: 27, Prediction: shinyPrediction{Supported: false}, Overrides: phoenix(shinyPrediction{Probability: 1, Supported: true}), Background: background},
		// Shiny-capable everywhere, with no chance in Phoenix.
		{Pokemon: 28, Prediction: shinyPrediction{Probability: 1, Supported: true}, Overrides: phoenix(shinyPrediction{Supported: false}), Background: background},
	}, nil, nil))
	for _, tc := range []struct {
		pokemon           int
		lat, lon          float64
		shiny, background []string
	}{
		{27, 33.45, -112.07, nil, []string{" Trainer-é "}},
		{27, 0, 0, nil, []string{" Trainer-é "}},
		{28, 33.45, -112.07, nil, []string{"0123456789abcdef"}},
		{28, 0, 0, testPlayers, []string{"0123456789abcdef"}},
	} {
		res := r.Evaluate(&webhook.PokemonWebhook{EncounterID: "1", PokemonID: tc.pokemon, Latitude: tc.lat, Longitude: tc.lon, SeenType: "wild"}).For(accounts(testPlayers...))
		if got := playerIDs(res.Shiny); !slices.Equal(got, tc.shiny) {
			t.Errorf("%d at %v,%v: shiny %v", tc.pokemon, tc.lat, tc.lon, got)
		}
		if got := playerIDs(res.Background); !slices.Equal(got, tc.background) {
			t.Errorf("%d at %v,%v: background %v", tc.pokemon, tc.lat, tc.lon, got)
		}
	}
}

func TestEvaluateEligibility(t *testing.T) {
	r := loadedRates(t, testFeed)
	for name, mutate := range map[string]func(p *webhook.PokemonWebhook){
		"undisguised 132": func(p *webhook.PokemonWebhook) { p.PokemonID = 132 },
		"disguised":       func(p *webhook.PokemonWebhook) { p.DisplayPokemonID = 16 },
		"disguised, not known as Ditto": func(p *webhook.PokemonWebhook) {
			p.PokemonID, p.DisplayPokemonID = 16, 19
		},
		"nearby Ditto": func(p *webhook.PokemonWebhook) {
			p.PokemonID, p.DisplayPokemonID, p.SeenType = 132, 16, "nearby_stop"
		},
		"nearby stop":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_stop" },
		"nearby cell":      func(p *webhook.PokemonWebhook) { p.SeenType = "nearby_cell" },
		"no seen type":     func(p *webhook.PokemonWebhook) { p.SeenType = "" },
		"costume 255":      func(p *webhook.PokemonWebhook) { p.Costume = 255 },
		"costume -1":       func(p *webhook.PokemonWebhook) { p.Costume = -1 },
		"costume 300":      func(p *webhook.PokemonWebhook) { p.Costume = 300 },
		"lure wild":        func(p *webhook.PokemonWebhook) { p.SeenType = "lure_wild" },
		"lure encounter":   func(p *webhook.PokemonWebhook) { p.SeenType = "lure_encounter" },
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
	p = wildPikachu()
	p.PokemonID, p.DisplayPokemonID = 132, 16
	if ev := r.Evaluate(p); ev == nil || !ev.ditto {
		t.Errorf("known Ditto: evaluation %+v, want a Ditto evaluation", ev)
	}
}

func TestEvaluateUnloadedAndUnknownVariant(t *testing.T) {
	if NewRates("x", "", 0).Evaluate(wildPikachu()) != nil {
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
	if c.Evaluated() || nilChecker.Evaluated() {
		t.Fatal("Evaluated without an evaluation")
	}
	if !NewChecker(r.Evaluate(wildPikachu()), humans).Evaluated() {
		t.Fatal("not Evaluated with a loaded feed and eligible spawn")
	}
}

func TestShinyVariantAndBoosted(t *testing.T) {
	r := loadedRates(t, testFeed)
	if s, p, ok := r.ShinyVariant(25, 0, 0, 33.45, -112.07); !ok || !s || p != onCutoff(0.05) {
		t.Fatalf("Phoenix override = %v %v %v", s, p, ok)
	}
	if s, p, ok := r.ShinyVariant(25, 0, 0, 51.5, 0); !ok || !s || p != onCutoff(0.1) {
		t.Fatalf("base = %v %v %v", s, p, ok)
	}
	if _, _, ok := r.ShinyVariant(1, 0, 0, 0, 0); ok {
		t.Fatal("unlisted variant reported known")
	}
	if _, _, ok := NewRates("x", "", 0).ShinyVariant(25, 0, 0, 0, 0); ok {
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

// Poracle-only: feed lookups for "form 0" fall back to the species' default
// (NORMAL) form, which the game keys under its own form id.
func TestShinyVariantFormDefaultFallback(t *testing.T) {
	r := loadedRates(t, `{"rates":[{"pokemon":25,"form":598,"costume":0,"prediction":{"probability":0.1,"supported":true},"overrides":{"Europe/London":{"probability":0.05,"supported":true}}}]}`)
	if _, _, ok := r.ShinyVariant(25, 0, 0, 51.5, 0); ok {
		t.Fatal("plain lookup of form 0 must not resolve (Golbat behaviour)")
	}
	if s, p, ok := r.ShinyVariantForm(25, 0, 598, 0, 51.5, 0); !ok || !s || p != onCutoff(0.05) {
		t.Fatalf("default-form fallback with override = %v %v %v", s, p, ok)
	}
	if s, p, ok := r.BaseShinyVariantForm(25, 0, 598, 0); !ok || !s || p != onCutoff(0.1) {
		t.Fatalf("base default-form fallback = %v %v %v", s, p, ok)
	}
	if _, _, ok := r.ShinyVariantForm(25, 0, 0, 0, 51.5, 0); ok {
		t.Fatal("no default form known must not resolve")
	}
	if _, _, ok := r.ShinyVariantForm(25, 7, 598, 0, 51.5, 0); ok {
		t.Fatal("explicit non-zero form must not fall back")
	}
	var nilRates *Rates
	if _, _, ok := nilRates.ShinyVariantForm(25, 0, 598, 0, 0, 0); ok {
		t.Fatal("nil rates known")
	}
	if _, _, ok := nilRates.BaseShinyVariantForm(25, 0, 598, 0); ok {
		t.Fatal("nil rates known")
	}
}

// An unsupported species (feed `unsupportedPokemon`) never rolls shiny and
// resolves as known + unsupported, for every form and costume.
func TestUnsupportedSpecies(t *testing.T) {
	r := loadedRates(t, `{"rates":[],"unsupportedPokemon":[931]}`)
	wild := &webhook.PokemonWebhook{EncounterID: "1", PokemonID: 931, Form: 2601, Latitude: 33.45, Longitude: -112.07, SeenType: "wild"}
	ev := r.Evaluate(wild)
	if ev == nil || ev.shinyCapable || ev.shinyCutoff != 0 || ev.backgroundCutoff != 0 {
		t.Fatalf("unsupported evaluation = %+v", ev)
	}
	// Golbat's three test players, many encounters: never shiny.
	for enc := 1; enc <= 2000; enc++ {
		wild.EncounterID = strconv.Itoa(enc)
		if res := r.Evaluate(wild).For(accounts(testPlayers...)); len(res.Shiny) != 0 || len(res.Background) != 0 {
			t.Fatalf("encounter %d: %+v", enc, res)
		}
	}
	for _, form := range []int{0, 2601} {
		if s, p, ok := r.ShinyVariant(931, form, 0, 33.45, -112.07); !ok || s || p != 0 {
			t.Fatalf("ShinyVariant form %d = %v %v %v", form, s, p, ok)
		}
		if s, _, ok := r.BaseShinyVariant(931, form, 0); !ok || s {
			t.Fatalf("BaseShinyVariant form %d = %v %v", form, s, ok)
		}
	}
	// Known for any form, so the default-form retry is not needed.
	if s, _, ok := r.ShinyVariantForm(931, 0, 2601, 0, 0, 0); !ok || s {
		t.Fatalf("ShinyVariantForm = %v %v", s, ok)
	}
	if s, _, ok := r.BaseShinyVariantForm(931, 0, 2601, 0); !ok || s {
		t.Fatalf("BaseShinyVariantForm = %v %v", s, ok)
	}
	if _, _, ok := r.ShinyVariant(25, 0, 0, 0, 0); ok {
		t.Fatal("unlisted species reported known")
	}
	if got := r.Boosted(33.45, -112.07); len(got) != 0 {
		t.Fatalf("Boosted = %+v", got)
	}
}

// onCutoff is a feed probability as the variant lookups report it: the
// 24-bit cutoff the roll compares against, over shinyScale.
func onCutoff(p float64) float64 {
	return float64(shinyPrediction{Probability: p, Supported: true}.cutoff(0)) / shinyScale
}

// Poracle-only: the variant lookups read the normalised cutoffs. supported
// is the base capability (overrides never change it), probability 0 is not
// capable, the feed fallback never makes an unlisted variant known, and
// Boosted compares cutoffs at the location with the 1/512 cutoff.
func TestShinyVariantCutoffs(t *testing.T) {
	r := loadedRates(t, `{"rates":[
		{"pokemon":27,"form":0,"costume":0,"prediction":{"probability":0.5,"supported":false},"overrides":{"America/Phoenix":{"probability":1,"supported":true}}},
		{"pokemon":28,"form":0,"costume":0,"prediction":{"probability":0.04,"supported":true},"overrides":{"America/Phoenix":{"probability":0,"supported":false}}},
		{"pokemon":29,"form":0,"costume":0,"prediction":{"probability":0,"supported":true}},
		{"pokemon":30,"form":0,"costume":0,"prediction":{"probability":0.001953125,"supported":true}},
		{"pokemon":31,"form":0,"costume":0,"prediction":{"probability":2,"supported":true}}
	],"fallback":{"probability":0.015625,"supported":true}}`)
	if r.zone(33.45, -112.07) != "America/Phoenix" {
		t.Skip("timezone data changed")
	}
	for _, tc := range []struct {
		pokemon   int
		lat, lon  float64
		supported bool
		oneIn     int
	}{
		{27, 33.45, -112.07, false, 0}, // override cannot make it capable
		{27, 51.5, 0, false, 0},
		{28, 33.45, -112.07, true, 0}, // capable, no chance here
		{28, 51.5, 0, true, 25},
		{29, 51.5, 0, false, 0}, // probability 0: not capable
		{30, 51.5, 0, true, 512},
		{31, 51.5, 0, true, 64}, // invalid probability takes the fallback
	} {
		s, p, ok := r.ShinyVariant(tc.pokemon, 0, 0, tc.lat, tc.lon)
		if !ok || s != tc.supported || OneIn(p) != tc.oneIn {
			t.Errorf("ShinyVariant(%d at %v,%v) = %v %v %v", tc.pokemon, tc.lat, tc.lon, s, p, ok)
		}
	}
	if s, p, ok := r.BaseShinyVariant(28, 0, 0); !ok || !s || OneIn(p) != 25 {
		t.Errorf("BaseShinyVariant(28) = %v %v %v", s, p, ok)
	}
	if s, _, ok := r.BaseShinyVariant(27, 0, 0); !ok || s {
		t.Errorf("BaseShinyVariant(27) = %v %v", s, ok)
	}
	// The feed fallback is a roll assumption, not a known rate.
	if _, _, ok := r.ShinyVariant(1, 0, 0, 51.5, 0); ok {
		t.Error("unlisted variant reported known")
	}
	// 27 is not capable, so its Phoenix override is not an event; 28 is
	// above 1/512 only outside Phoenix; 30 is exactly 1/512.
	got := r.Boosted(33.45, -112.07)
	if len(got) != 1 || got[0].PokemonID != 31 || got[0].OneIn != 64 {
		t.Errorf("Boosted Phoenix = %+v", got)
	}
	got = r.Boosted(51.5, 0)
	if len(got) != 2 || got[0].PokemonID != 28 || got[0].OneIn != 25 || got[1].PokemonID != 31 {
		t.Errorf("Boosted London = %+v", got)
	}
}

func TestOneIn(t *testing.T) {
	for _, tc := range []struct {
		p    float64
		want int
	}{
		{float64(defaultShinyCutoff) / shinyScale, 512},
		{onCutoff(1.0 / 25), 25},
		{onCutoff(0.04), 25},
		{onCutoff(1.0 / 64), 64},
		{onCutoff(0.1), 10},
		{0, 0},
	} {
		if got := OneIn(tc.p); got != tc.want {
			t.Errorf("OneIn(%v) = %d, want %d", tc.p, got, tc.want)
		}
	}
}

// thirdDraw is the Ditto shiny roll computed from scratch, independently of
// shinyPlayer: the third Next(24) of the personal generator.
func thirdDraw(encounter uint64, player string) uint32 {
	input := make([]byte, 8+len(player))
	binary.LittleEndian.PutUint64(input, encounter)
	copy(input[8:], player)
	digest := sha1.Sum(input) //nolint:gosec // the game seeds its generator with SHA-1; this is not a security use
	rng := javarandom.New(binary.LittleEndian.Uint64(digest[:8]))
	rng.Next(24)
	rng.Next(24)
	return rng.Next(24)
}

// Mirrors Golbat's TestPersonalDittoShiny, through Evaluate. Poracle: the
// webhook has no is_ditto; a known Ditto is pokemon_id 132 with its
// disguise in display_pokemon_id (see isKnownDitto). The filter path
// (shinyScan.match / matchDitto) is not copied.
func TestPersonalDittoShiny(t *testing.T) {
	// Pidgey disguises at rate 1 would be shiny and have a background for
	// everyone; a known Ditto must ignore that and the unsupportedPokemon
	// entry for 132, and use only the third draw at 1/64.
	r := loadedRates(t, `{"rates":[{"pokemon":16,"form":0,"costume":0,"prediction":{"probability":1,"supported":true},"background":{"probability":1,"supported":true}}],"unsupportedPokemon":[132]}`)
	ditto := &webhook.PokemonWebhook{PokemonID: dittoPokemonId, DisplayPokemonID: 16, SeenType: "wild"}
	if personalLookup(ditto) == 0 {
		t.Fatal("known Ditto is not eligible")
	}
	// Pinned: 6 is a Ditto shiny for Player-ID:ABC/123 only, 26 for
	// Trainer-é only. 25 is not, though Trainer-é's first two draws are
	// below 1/64; 15 is not, though 0123456789abcdef's first is.
	pinned := map[uint64][]string{1: nil, 6: {"Player-ID:ABC/123"}, 15: nil, 25: nil, 26: {" Trainer-é "}}
	for encounter := uint64(1); encounter <= 1000; encounter++ {
		var want []string
		for _, id := range testPlayers {
			if thirdDraw(encounter, id) < (1<<24)/64 {
				want = append(want, id)
			}
		}
		if pin, ok := pinned[encounter]; ok && !slices.Equal(want, pin) {
			t.Fatalf("encounter %d: generator gives %v, pinned %v", encounter, want, pin)
		}
		ditto.EncounterID = strconv.FormatUint(encounter, 10)
		res := r.Evaluate(ditto).For(accounts(testPlayers...))
		// No Ditto background has been observed: never reported.
		if got := playerIDs(res.Shiny); !slices.Equal(got, want) || res.Background != nil {
			t.Fatalf("encounter %d: shiny %v background %v, want shiny %v", encounter, got, res.Background, want)
		}
		for _, id := range testPlayers {
			if got := newShinyPlayer(id).dittoRoll(encounter) < dittoShinyCutoff; got != slices.Contains(want, id) {
				t.Fatalf("encounter %d: dittoRoll %q = %v", encounter, id, got)
			}
		}
	}
}

// Poracle-only: a known Ditto's variant lookups report its fixed 1/64 rate
// whatever the feed says (here unsupportedPokemon lists 132), so its alert
// shows shinyPossible and shinyRate 64. Unloaded feed: still unknown.
func TestShinyVariantDitto(t *testing.T) {
	r := loadedRates(t, `{"rates":[{"pokemon":132,"form":0,"costume":0,"prediction":{"probability":0,"supported":false}}],"unsupportedPokemon":[132]}`)
	for _, form := range []int{0, 2} {
		if s, p, ok := r.ShinyVariant(132, form, 0, 33.45, -112.07); !ok || !s || OneIn(p) != 64 {
			t.Errorf("ShinyVariant(132, %d) = %v %v %v", form, s, p, ok)
		}
		if s, p, ok := r.BaseShinyVariant(132, form, 0); !ok || !s || OneIn(p) != 64 {
			t.Errorf("BaseShinyVariant(132, %d) = %v %v %v", form, s, p, ok)
		}
	}
	if s, p, ok := r.ShinyVariantForm(132, 0, 0, 0, 0, 0); !ok || !s || OneIn(p) != 64 {
		t.Errorf("ShinyVariantForm(132) = %v %v %v", s, p, ok)
	}
	if got := r.Boosted(33.45, -112.07); len(got) != 0 {
		t.Errorf("Boosted lists the Ditto: %+v", got)
	}
	if _, _, ok := NewRates("x", "", 0).ShinyVariant(132, 0, 0, 0, 0); ok {
		t.Error("unloaded feed reported the Ditto known")
	}
}
