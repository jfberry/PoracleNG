package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newFilteredEngine builds a gin engine wired exactly like main.go:
// trusted proxies first, then the IP filter.
func newFilteredEngine(t *testing.T, trusted, white, black []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := ConfigureTrustedProxies(r, trusted); err != nil {
		t.Fatalf("ConfigureTrustedProxies: %v", err)
	}
	f, err := IPFilter(white, black)
	if err != nil {
		t.Fatalf("IPFilter: %v", err)
	}
	r.Use(f)
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

func doReq(r *gin.Engine, remote, xff string) int {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestIPFilter_SpoofedXFFIgnoredWithoutTrustedProxies(t *testing.T) {
	r := newFilteredEngine(t, nil, []string{"10.0.0.5"}, nil)
	if got := doReq(r, "203.0.113.9:1234", "10.0.0.5"); got != http.StatusForbidden {
		t.Fatalf("spoofed XFF from untrusted peer: got %d, want 403", got)
	}
	if got := doReq(r, "10.0.0.5:1234", ""); got != http.StatusOK {
		t.Fatalf("real whitelisted peer: got %d, want 200", got)
	}
}

func TestIPFilter_SpoofedXFFCannotEvadeBlacklist(t *testing.T) {
	r := newFilteredEngine(t, nil, nil, []string{"198.51.100.7"})
	if got := doReq(r, "198.51.100.7:1234", "1.2.3.4"); got != http.StatusForbidden {
		t.Fatalf("blacklisted peer with spoofed XFF: got %d, want 403", got)
	}
}

func TestIPFilter_XFFHonouredFromTrustedProxy(t *testing.T) {
	r := newFilteredEngine(t, []string{"172.16.0.0/12"}, []string{"10.0.0.5"}, nil)
	if got := doReq(r, "172.17.0.1:1234", "10.0.0.5"); got != http.StatusOK {
		t.Fatalf("XFF from trusted proxy: got %d, want 200", got)
	}
	if got := doReq(r, "172.17.0.1:1234", "10.9.9.9"); got != http.StatusForbidden {
		t.Fatalf("XFF (non-whitelisted) from trusted proxy: got %d, want 403", got)
	}
	// Untrusted peer still cannot spoof.
	if got := doReq(r, "203.0.113.9:1234", "10.0.0.5"); got != http.StatusForbidden {
		t.Fatalf("XFF from untrusted peer: got %d, want 403", got)
	}
}

func TestIPFilter_CIDRAndIPv6(t *testing.T) {
	r := newFilteredEngine(t, nil, []string{"10.0.0.0/8", "2001:db8::/32", "192.0.2.1"}, []string{"10.66.0.0/16"})
	cases := []struct {
		remote string
		want   int
	}{
		{"10.1.2.3:1", http.StatusOK},
		{"11.1.2.3:1", http.StatusForbidden},
		{"10.66.1.1:1", http.StatusForbidden}, // blacklist wins
		{"192.0.2.1:1", http.StatusOK},
		{"192.0.2.2:1", http.StatusForbidden},
		{"[2001:db8::1]:1", http.StatusOK},
		{"[2001:db9::1]:1", http.StatusForbidden},
	}
	for _, tc := range cases {
		if got := doReq(r, tc.remote, ""); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.remote, got, tc.want)
		}
	}
}

func TestIPFilter_IPv4MappedIPv6MatchesIPv4Rule(t *testing.T) {
	r := newFilteredEngine(t, nil, []string{"10.0.0.0/8"}, nil)
	if got := doReq(r, "[::ffff:10.1.2.3]:1", ""); got != http.StatusOK {
		t.Fatalf("v4-mapped: got %d, want 200", got)
	}
}

func TestIPFilter_InvalidEntries(t *testing.T) {
	if _, err := IPFilter([]string{"10.0.0.0/33"}, nil); err == nil || !strings.Contains(err.Error(), "10.0.0.0/33") {
		t.Fatalf("expected error naming bad whitelist entry, got %v", err)
	}
	if _, err := IPFilter(nil, []string{"not-an-ip"}); err == nil || !strings.Contains(err.Error(), "not-an-ip") {
		t.Fatalf("expected error naming bad blacklist entry, got %v", err)
	}
	gin.SetMode(gin.TestMode)
	if err := ConfigureTrustedProxies(gin.New(), []string{"bogus"}); err == nil {
		t.Fatal("expected error for invalid trusted proxy")
	}
}
