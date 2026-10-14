package personalshiny

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testFeed = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"overrides":{"America/Phoenix":{"probability":0.05,"supported":true}},"background":{"probability":0.2,"supported":true},"background_overrides":{"America/Phoenix":{"probability":0.15,"supported":true}}}]}`

func TestRefreshLoadsFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rates" || r.URL.Query().Get("compact") != "1" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("incorrect rates request %s %q", r.URL, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(testFeed))
	}))
	defer server.Close()
	r := NewRates(server.URL+"/", "test", 0)
	if r.Loaded() {
		t.Fatal("loaded before refresh")
	}
	if err := r.refreshShinyRates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Loaded() {
		t.Fatal("not loaded after refresh")
	}
	st := r.Status()
	if !st.Configured || !st.Loaded || st.Variants != 1 || st.LastError != "" || st.LastSuccess.IsZero() {
		t.Fatalf("status = %+v", st)
	}
}

func TestShinyRatesRejectMissingList(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"error":"down"}`, `{"rates":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		r := NewRates(server.URL, "", 0)
		good := &shinyRates{}
		r.snapshot.Store(good)
		if err := r.refreshShinyRates(context.Background()); err == nil {
			t.Errorf("%s: accepted", body)
		}
		if r.snapshot.Load() != good {
			t.Errorf("%s: replaced the last good snapshot", body)
		}
		if r.Status().LastError == "" {
			t.Errorf("%s: last error not recorded", body)
		}
		server.Close()
	}
}

func TestRefreshKeepsSnapshotOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()
	r := NewRates(server.URL, "", 0)
	good := &shinyRates{}
	r.snapshot.Store(good)
	if err := r.refreshShinyRates(context.Background()); err == nil {
		t.Fatal("502 accepted")
	}
	if r.snapshot.Load() != good {
		t.Fatal("replaced the last good snapshot")
	}
}

func TestRefreshDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testFeed)) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.String(), http.StatusFound)
	}))
	defer redirect.Close()
	r := NewRates(redirect.URL, "secret", 0)
	if err := r.refreshShinyRates(context.Background()); err == nil {
		t.Fatal("redirect followed (and would leak the bearer token)")
	}
}

func TestNilAndUnconfiguredRates(t *testing.T) {
	var nilRates *Rates
	if nilRates.Loaded() || nilRates.Status().Configured {
		t.Fatal("nil Rates must report unconfigured and unloaded")
	}
	nilRates.Start(context.Background()) // must not panic
	nilRates.Stop()
	r := NewRates("", "", 0)
	r.Start(context.Background()) // no URL: no goroutine
	r.Stop()
	if r.Status().Configured {
		t.Fatal("empty URL reported as configured")
	}
}

func TestCutoff(t *testing.T) {
	for _, tc := range []struct {
		p    shinyPrediction
		fb   uint32
		want uint32
	}{
		{shinyPrediction{Probability: 1.0 / 512, Supported: true}, defaultShinyCutoff, 32768},
		{shinyPrediction{Probability: 0.04, Supported: true}, defaultShinyCutoff, 671089},
		{shinyPrediction{Probability: 0.5, Supported: false}, defaultShinyCutoff, 0},
		{shinyPrediction{Probability: -1, Supported: true}, defaultShinyCutoff, defaultShinyCutoff},
		{shinyPrediction{Probability: 2, Supported: true}, 0, 0},
	} {
		if got := tc.p.cutoff(tc.fb); got != tc.want {
			t.Errorf("cutoff(%+v, %d) = %d, want %d", tc.p, tc.fb, got, tc.want)
		}
	}
}

// Mirrors Golbat's TestShinyRatesUnsupportedSpecies. Poracle: the
// shinyMaximum assertion is not ported (Poracle has no scan-filter path, so
// shinyMaximum is not copied).
func TestShinyRatesUnsupportedSpecies(t *testing.T) {
	// 931 has no entry but is listed as never shiny; 25 has no entry and is not.
	r := loadedRates(t, `{"rates":[],"unsupportedPokemon":[132,931]}`)
	s := &shinyScan{rates: r.snapshot.Load(), zone: r.zone}
	rate931, rate25 := s.rate(931, 2601, 0), s.rate(25, 0, 0)
	if cutoff, capable := s.shinyCutoff(rate931, 0, 0), rate931.capable(); capable || cutoff != 0 {
		t.Errorf("unsupported species: cutoff %d capable %v", cutoff, capable)
	}
	if cutoff, capable := s.shinyCutoff(rate25, 0, 0), rate25.capable(); !capable || cutoff != defaultShinyCutoff {
		t.Errorf("unlisted species: cutoff %d capable %v", cutoff, capable)
	}
}

// Mirrors Golbat's TestShinyRatesNormalize.
func TestShinyRatesNormalize(t *testing.T) {
	phoenix := func(p shinyPrediction) map[string]shinyPrediction {
		return map[string]shinyPrediction{"America/Phoenix": p}
	}
	base := shinyPrediction{Probability: 1.0 / 512, Supported: true}
	background := shinyPrediction{Probability: 0.04, Supported: true}
	// As in the live feed: every override matches its base, here once only
	// after rounding to a cutoff. Nothing needs a timezone.
	rates := newShinyRates([]shinyRate{
		{Pokemon: 1, Prediction: base, Overrides: phoenix(shinyPrediction{Probability: 0.0019531, Supported: true})},
		{Pokemon: 2, Prediction: base, Overrides: phoenix(base),
			Background: &background, BackgroundOverrides: phoenix(background)},
		// Not capable: overrides cannot change that, so none are kept.
		{Pokemon: 3, Prediction: shinyPrediction{Supported: true}, Overrides: phoenix(base)},
	}, nil, nil)
	if rates.zoned {
		t.Error("snapshot with only no-op overrides is zoned")
	}
	for v, c := range rates.variants {
		if c.shinyOverrides != nil || c.backgroundOverrides != nil {
			t.Errorf("%v kept overrides %+v", v, c)
		}
	}
	if c := rates.variants[shinyVariant{2, 0, 0}]; c.shiny != defaultShinyCutoff || c.background != background.cutoff(0) {
		t.Errorf("cutoffs %+v", c)
	}
	if m := rates.maximum[3]; m != 0 {
		t.Errorf("not-capable species maximum %d", m)
	}

	// A real override survives, marks the snapshot, and raises the maximum.
	rates = newShinyRates([]shinyRate{
		{Pokemon: 1, Prediction: base, Overrides: map[string]shinyPrediction{"America/Phoenix": {Probability: 0.25, Supported: true}, "America/Denver": base}},
		{Pokemon: 2, Prediction: base, BackgroundOverrides: phoenix(shinyPrediction{Probability: 0.1, Supported: true})},
	}, nil, nil)
	if !rates.zoned {
		t.Error("snapshot with effective overrides is not zoned")
	}
	if c := rates.variants[shinyVariant{1, 0, 0}]; len(c.shinyOverrides) != 1 || c.shinyOverrides["America/Phoenix"] != shinyScale/4 {
		t.Errorf("shiny overrides %v", c.shinyOverrides)
	}
	if rates.maximum[1] != shinyScale/4 || rates.backgroundMaximum[2] == 0 {
		t.Errorf("maxima %v %v", rates.maximum, rates.backgroundMaximum)
	}
	// Poracle: the zone resolver is the Rates' (geo.TimezoneName).
	zone := NewRates("", "", 0).zone
	s := &shinyScan{rates: rates, zone: zone}
	if got := s.shinyCutoff(rates.variants[shinyVariant{1, 0, 0}], 0, 0); got != defaultShinyCutoff {
		t.Errorf("default cutoff %d", got)
	}
	if zone(33.45, -112.07) != "America/Phoenix" {
		t.Skip("timezone data changed")
	}
	if got := s.shinyCutoff(rates.variants[shinyVariant{1, 0, 0}], 33.45, -112.07); got != shinyScale/4 {
		t.Errorf("Phoenix cutoff %d", got)
	}
}

// Mirrors Golbat's TestShinyRatesFallback. Poracle: the shinyMaximum
// assertion is not ported (shinyMaximum is not copied).
func TestShinyRatesFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback string
		want     uint32
	}{
		{"omitted", ``, defaultShinyCutoff},
		{"published", `,"fallback":{"probability":0.00390625,"assumed":true,"supported":true}`, shinyScale / 256},
		{"zero", `,"fallback":{"probability":0,"supported":true}`, 0},
	} {
		r := loadedRates(t, `{"rates":[{"pokemon":1,"form":0,"costume":0,"prediction":{"probability":2,"supported":true}}],"unsupportedPokemon":[931]`+tc.fallback+`}`)
		s := &shinyScan{rates: r.snapshot.Load(), zone: r.zone}
		// Unlisted variants, and listed ones with an invalid probability,
		// take the fallback; a zero fallback leaves them not capable.
		for _, rate := range []shinyCutoffs{s.rate(25, 0, 0), s.rate(1, 0, 0)} {
			if rate.shiny != tc.want || rate.capable() != (tc.want > 0) {
				t.Errorf("%s: cutoffs %+v", tc.name, rate)
			}
		}
		if rate := s.rate(931, 0, 0); rate.capable() {
			t.Errorf("%s: unsupported species capable", tc.name)
		}
	}
}

// Mirrors Golbat's TestShinyRatesMaxBytes. Poracle: the limit is NewRates'
// maxBytes (config [shiny_rates] max_bytes).
func TestShinyRatesMaxBytes(t *testing.T) {
	body := `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true}}]}`
	for _, tc := range []struct {
		name     string
		limit    int64
		chunked  bool
		tooLarge bool
	}{
		{"fits exactly", int64(len(body)), false, false},
		{"over by one", int64(len(body)) - 1, false, true},
		{"over by one, no length", int64(len(body)) - 1, true, true},
		{"unset uses the default", 0, false, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.chunked {
				w.(http.Flusher).Flush()
			}
			_, _ = w.Write([]byte(body))
		}))
		r := NewRates(server.URL, "", tc.limit)
		good := &shinyRates{}
		r.snapshot.Store(good)
		err := r.refreshShinyRates(context.Background())
		if got := errors.Is(err, errShinyRatesTooLarge); got != tc.tooLarge {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if tc.tooLarge && r.snapshot.Load() != good {
			t.Errorf("%s: replaced the last good snapshot", tc.name)
		}
		server.Close()
	}
}
