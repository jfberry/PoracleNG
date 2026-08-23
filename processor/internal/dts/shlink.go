package dts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/breaker"
	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// ShlinkShortener shortens URLs via a Shlink (https://shlink.io/) instance.
type ShlinkShortener struct {
	url     string
	apiKey  string
	domain  string
	client  *http.Client
	breaker *breaker.Gate
}

// NewShlinkShortener creates a ShlinkShortener pointing at the given Shlink server.
// If url or apiKey is empty, Shorten will always return the original URL.
func NewShlinkShortener(url, apiKey, domain string) *ShlinkShortener {
	return &ShlinkShortener{
		url:     url,
		apiKey:  apiKey,
		domain:  domain,
		client:  &http.Client{Timeout: 10 * time.Second},
		breaker: breaker.NewGate(breaker.Config{Name: "shlink"}),
	}
}

// shlinkRequest is the JSON body sent to Shlink's create short-URL endpoint.
type shlinkRequest struct {
	LongURL      string  `json:"longUrl"`
	FindIfExists bool    `json:"findIfExists"`
	Domain       *string `json:"domain"` // null if empty
}

// shlinkResponse is the relevant portion of Shlink's response.
type shlinkResponse struct {
	ShortURL string `json:"shortUrl"`
}

// Shorten sends a URL to Shlink and returns the short URL.
// On any error (network, non-200, parse) the original URL is returned.
func (s *ShlinkShortener) Shorten(longURL string) string {
	if s == nil || s.url == "" || s.apiKey == "" {
		metrics.ShlinkTotal.WithLabelValues("disabled").Inc()
		return longURL
	}

	// Open circuit: skip the marshal/request build entirely and fall back to the
	// long URL. (Half-open returns false so the single probe still runs.)
	if s.breaker.IsOpen() {
		metrics.ShlinkTotal.WithLabelValues("circuit_break").Inc()
		return longURL
	}
	start := time.Now()

	reqBody := shlinkRequest{
		LongURL:      longURL,
		FindIfExists: true,
	}
	if s.domain != "" {
		reqBody.Domain = &s.domain
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		log.Warnf("shlink: marshal request: %v", err)
		return longURL
	}

	endpoint := s.url + "/rest/v2/short-urls"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Warnf("shlink: create request: %v", err)
		return longURL
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", s.apiKey)

	// Guard the Shlink call with a circuit breaker so an outage doesn't make
	// every rendered message wait out the 10s timeout. The original URL is the
	// safe fallback for both an open circuit and any call failure.
	short := longURL
	berr := s.breaker.Do(func() error {
		resp, err := s.client.Do(req)
		if err != nil {
			log.Warnf("shlink: request failed: %v", err)
			metrics.ShlinkTotal.WithLabelValues("error").Inc()
			metrics.ShlinkDuration.Observe(time.Since(start).Seconds())
			return err
		}
		defer resp.Body.Close()

		metrics.ShlinkDuration.Observe(time.Since(start).Seconds())

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Warnf("shlink: unexpected status %d for %s", resp.StatusCode, longURL)
			metrics.ShlinkTotal.WithLabelValues("error").Inc()
			return fmt.Errorf("shlink: status %d", resp.StatusCode)
		}

		// Past this point Shlink responded with a 2xx — it is reachable. A bad
		// body just means we fall back to the long URL for this call; it is NOT
		// a connectivity failure, so return nil and don't let it trip the
		// breaker (which would suppress shortening globally for 30s).
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Warnf("shlink: read response: %v", err)
			metrics.ShlinkTotal.WithLabelValues("error").Inc()
			return nil
		}

		var result shlinkResponse
		if err := json.Unmarshal(respBytes, &result); err != nil {
			log.Warnf("shlink: parse response: %v", err)
			metrics.ShlinkTotal.WithLabelValues("error").Inc()
			return nil
		}

		if result.ShortURL == "" {
			metrics.ShlinkTotal.WithLabelValues("error").Inc()
			return nil
		}
		metrics.ShlinkTotal.WithLabelValues("ok").Inc()
		short = result.ShortURL
		return nil
	})
	if errors.Is(berr, breaker.ErrOpen) {
		metrics.ShlinkTotal.WithLabelValues("circuit_break").Inc()
	}
	return short
}

// shortenMarkerRe matches <S< ... >S> markers wrapping URLs to be shortened.
var shortenMarkerRe = regexp.MustCompile(`<S<(.+?)>S>`)

// ShortenMarkers replaces <S< ... >S> markers in text with shortened URLs.
// If shortener is nil, the markers are stripped and the raw URLs inside are preserved.
// If the text contains no markers, it is returned unchanged.
func ShortenMarkers(text string, shortener *ShlinkShortener) string {
	if !strings.Contains(text, "<S<") {
		return text
	}
	return ShortenMarkersWithCache(text, shortener, nil)
}

// ShortenMarkersWithCache is like ShortenMarkers but accepts an optional cache
// (map[string]string) to avoid redundant HTTP calls for the same long URL.
// When cache is non-nil, shortened results are stored and reused across calls.
// This is useful when many users receive the same template with identical URLs.
func ShortenMarkersWithCache(text string, shortener *ShlinkShortener, cache map[string]string) string {
	if !strings.Contains(text, "<S<") {
		return text
	}
	return shortenMarkerRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := shortenMarkerRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		rawURL := sub[1]
		if shortener == nil {
			return rawURL
		}
		if cache != nil {
			if cached, ok := cache[rawURL]; ok {
				metrics.ShlinkTotal.WithLabelValues("cache_hit").Inc()
				return cached
			}
		}
		short := shortener.Shorten(rawURL)
		if cache != nil {
			cache[rawURL] = short
		}
		return short
	})
}
