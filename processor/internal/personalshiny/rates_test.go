package personalshiny

import (
	"context"
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
	r := NewRates(server.URL+"/", "test")
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
		r := NewRates(server.URL, "")
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
	r := NewRates(server.URL, "")
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
	r := NewRates(redirect.URL, "secret")
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
	r := NewRates("", "")
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

// Mirrors Golbat's TestShinyRatesUnsupportedSpecies.
func TestShinyRatesUnsupportedSpecies(t *testing.T) {
	// 931 has no entry but is listed as never shiny; 25 has no entry and is not.
	r := loadedRates(t, `{"rates":[],"unsupportedPokemon":[132,931]}`)
	s := &shinyScan{rates: r.snapshot.Load(), zone: r.zone}
	rate931, ok931 := s.rate(931, 2601, 0)
	rate25, ok25 := s.rate(25, 0, 0)
	if cutoff, capable := s.shinyCutoff(rate931, ok931, 0, 0); capable || cutoff != 0 {
		t.Errorf("unsupported species: cutoff %d capable %v", cutoff, capable)
	}
	if cutoff, capable := s.shinyCutoff(rate25, ok25, 0, 0); !capable || cutoff != defaultShinyCutoff {
		t.Errorf("unlisted species: cutoff %d capable %v", cutoff, capable)
	}
}
