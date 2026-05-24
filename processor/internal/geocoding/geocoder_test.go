package geocoding

import (
	"sync"
	"testing"
	"time"
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
		sem:      make(chan struct{}, 1),
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
