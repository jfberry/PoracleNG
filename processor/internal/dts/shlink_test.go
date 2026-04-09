package dts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortenMarkersNilShortener(t *testing.T) {
	text := "Check <S<https://example.com/very/long/url>S> now!"
	result := ShortenMarkers(text, nil)
	assert.Equal(t, "Check https://example.com/very/long/url now!", result)
}

func TestShortenMarkersMultiple(t *testing.T) {
	text := "A: <S<https://a.com>S> B: <S<https://b.com>S>"
	result := ShortenMarkers(text, nil)
	assert.Equal(t, "A: https://a.com B: https://b.com", result)
}

func TestShortenMarkersNoMarkers(t *testing.T) {
	text := "No markers here https://example.com"
	result := ShortenMarkers(text, nil)
	assert.Equal(t, text, result)
}

func TestShortenWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/rest/v2/short-urls", r.URL.Path)
		assert.Equal(t, "test-api-key", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req shlinkRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/very/long/url", req.LongURL)
		assert.True(t, req.FindIfExists)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shlinkResponse{ShortURL: "https://s.example.com/abc"})
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "test-api-key", "")
	result := s.Shorten("https://example.com/very/long/url")
	assert.Equal(t, "https://s.example.com/abc", result)
}

func TestShortenWithDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req shlinkRequest
		json.NewDecoder(r.Body).Decode(&req)
		require.NotNil(t, req.Domain)
		assert.Equal(t, "short.example.com", *req.Domain)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shlinkResponse{ShortURL: "https://short.example.com/xyz"})
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "short.example.com")
	result := s.Shorten("https://example.com/long")
	assert.Equal(t, "https://short.example.com/xyz", result)
}

func TestShortenNoDomainSendsNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		// domain should be null when empty string is provided
		assert.Equal(t, "null", string(raw["domain"]))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shlinkResponse{ShortURL: "https://s.com/x"})
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "")
	result := s.Shorten("https://example.com")
	assert.Equal(t, "https://s.com/x", result)
}

func TestShortenServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "")
	result := s.Shorten("https://example.com/original")
	assert.Equal(t, "https://example.com/original", result)
}

func TestShortenInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "")
	result := s.Shorten("https://example.com/original")
	assert.Equal(t, "https://example.com/original", result)
}

func TestShortenEmptyShortURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shlinkResponse{ShortURL: ""})
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "")
	result := s.Shorten("https://example.com/original")
	assert.Equal(t, "https://example.com/original", result)
}

func TestShortenNilShortener(t *testing.T) {
	var s *ShlinkShortener
	result := s.Shorten("https://example.com")
	assert.Equal(t, "https://example.com", result)
}

func TestShortenEmptyConfig(t *testing.T) {
	s := NewShlinkShortener("", "", "")
	result := s.Shorten("https://example.com")
	assert.Equal(t, "https://example.com", result)
}

func TestShortenMarkersWithShortener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req shlinkRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shlinkResponse{ShortURL: "https://s.com/" + req.LongURL[len(req.LongURL)-1:]})
	}))
	defer server.Close()

	s := NewShlinkShortener(server.URL, "key", "")
	text := "Visit <S<https://example.com/a>S> and <S<https://example.com/b>S>"
	result := ShortenMarkers(text, s)
	assert.Equal(t, "Visit https://s.com/a and https://s.com/b", result)
}
