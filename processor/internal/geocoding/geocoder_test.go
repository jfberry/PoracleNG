package geocoding

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/pokemon/poracleng/processor/internal/breaker"
)

type languageProvider struct {
	mu        sync.Mutex
	languages []string
}

func (p *languageProvider) Reverse(lat, lon float64, language string) (*Address, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.languages = append(p.languages, language)
	return &Address{City: language, CountryCode: "DE"}, nil
}

func (p *languageProvider) Forward(query string) ([]ForwardResult, error) { return nil, nil }

func (p *languageProvider) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.languages))
	copy(out, p.languages)
	return out
}

// forwardStubProvider returns canned forward-geocode results/errors so the
// Geocoder's logging behaviour can be exercised without a network call.
type forwardStubProvider struct {
	results []ForwardResult
	err     error
}

func (p *forwardStubProvider) Reverse(lat, lon float64, language string) (*Address, error) {
	return nil, nil
}
func (p *forwardStubProvider) Forward(query string) ([]ForwardResult, error) {
	return p.results, p.err
}

// newForwardTestGeocoder builds a Geocoder for exercising the forward path.
// Forward geocoding doesn't go through the circuit breaker, so a nil breaker
// is fine here — only the provider and logging are involved.
func newForwardTestGeocoder(p Provider) *Geocoder {
	return &Geocoder{
		provider: p,
		config:   Config{FailureThreshold: 5, CooldownMs: 1},
	}
}

func findWarn(hook *test.Hook, contains string) *logrus.Entry {
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, contains) {
			return e
		}
	}
	return nil
}

func TestForwardLogsProviderError(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	g := newForwardTestGeocoder(&forwardStubProvider{
		err: fmt.Errorf("nominatim: request failed: connection refused"),
	})

	if _, err := g.Forward("London"); err == nil {
		t.Fatal("expected the provider error to propagate")
	}
	entry := findWarn(hook, "London")
	if entry == nil {
		t.Fatalf("expected a warning mentioning the query; entries: %v", hook.AllEntries())
	}
	if !strings.Contains(entry.Message, "connection refused") {
		t.Errorf("warning should include the provider error, got %q", entry.Message)
	}
}

func TestForwardLogsEmptyResults(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	g := newForwardTestGeocoder(&forwardStubProvider{results: nil})

	results, err := g.Forward("Nowhereville")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
	if findWarn(hook, "Nowhereville") == nil {
		t.Fatalf("expected a warning about empty results; entries: %v", hook.AllEntries())
	}
}

func TestForwardSuccessNoWarning(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	g := newForwardTestGeocoder(&forwardStubProvider{
		results: []ForwardResult{{Latitude: 1, Longitude: 2}},
	})

	if _, err := g.Forward("Paris"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range hook.AllEntries() {
		if e.Level <= logrus.WarnLevel {
			t.Errorf("did not expect a warning on success, got %q", e.Message)
		}
	}
}

func TestGeocoderLanguageAffectsProviderAndCacheKey(t *testing.T) {
	cache, err := NewCache(t.TempDir(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	defer cache.Close()

	addrTmpl, err := CompileAddressTemplate("{{{city}}}")
	if err != nil {
		t.Fatalf("CompileAddressTemplate: %v", err)
	}
	provider := &languageProvider{}
	g := &Geocoder{
		provider: provider,
		cache:    cache,
		config: Config{
			CacheDetail:      3,
			FailureThreshold: 5,
			CooldownMs:       1,
		},
		addrTmpl: addrTmpl,
		breaker:  breaker.New[*Address](breaker.Config{Name: "test", Concurrency: 1}),
	}

	if got := g.GetAddressForLanguage(52.517, 13.389, "DE"); got.City != "de" {
		t.Fatalf("de city=%q, want de", got.City)
	}
	if got := g.GetAddressForLanguage(52.517, 13.389, "en"); got.City != "en" {
		t.Fatalf("en city=%q, want en", got.City)
	}
	if got := g.GetAddressForLanguage(52.517, 13.389, "de"); got.City != "de" {
		t.Fatalf("cached de city=%q, want de", got.City)
	}

	calls := provider.calls()
	if len(calls) != 2 {
		t.Fatalf("provider calls=%v, want two cache misses", calls)
	}
	if calls[0] != "de" || calls[1] != "en" {
		t.Fatalf("provider languages=%v, want [de en]", calls)
	}
}

func TestCacheKeyForLanguage(t *testing.T) {
	base := CacheKey(52.517, 13.389, 3)
	if got := CacheKeyForLanguage(52.517, 13.389, 3, ""); got != base {
		t.Fatalf("blank language key=%q, want %q", got, base)
	}
	if got := CacheKeyForLanguage(52.517, 13.389, 3, " DE "); got != "de:"+base {
		t.Fatalf("localized key=%q, want %q", got, "de:"+base)
	}
}
