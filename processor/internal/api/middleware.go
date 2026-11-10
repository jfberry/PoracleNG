package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// isV1Path reports whether path is served by the frozen v1 surface
// (/api/tracking, /api/humans, /api/profiles and anything beneath them).
// /api/v2/humans is not v1.
func isV1Path(path string) bool {
	for _, p := range []string{"/api/tracking", "/api/humans", "/api/profiles"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// abortUnauthorized sends the 401: v1 keeps its legacy body, everything else
// (huma-served routes) gets an RFC 9457 problem+json body.
func abortUnauthorized(c *gin.Context) {
	if isV1Path(c.Request.URL.Path) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"status": "authError",
			"reason": "incorrect or missing api secret",
		})
		return
	}
	body, _ := json.Marshal(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "Unauthorized",
		"detail": "incorrect or missing api secret",
	})
	c.Abort()
	c.Data(http.StatusUnauthorized, "application/problem+json", body)
}

// RequireSecretGin returns a Gin middleware that validates X-Poracle-Secret.
// If apiSecret is empty, all requests are allowed.
func RequireSecretGin(apiSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiSecret == "" {
			c.Next()
			return
		}
		if c.GetHeader("X-Poracle-Secret") != apiSecret {
			abortUnauthorized(c)
			return
		}
		c.Next()
	}
}

// ConfigureTrustedProxies sets the reverse proxies whose X-Forwarded-For /
// X-Real-IP headers gin may honour when computing ClientIP. An empty list
// trusts no proxy, so ClientIP is always the TCP peer address and a client
// cannot spoof its IP via forwarded headers (#264). Entries are IPs or CIDR
// ranges; an invalid entry is returned as an error.
func ConfigureTrustedProxies(r *gin.Engine, proxies []string) error {
	if len(proxies) == 0 {
		return r.SetTrustedProxies(nil)
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		return fmt.Errorf("invalid [processor] trusted_proxies: %w", err)
	}
	return nil
}

// ParseIPRanges parses a list of IP addresses and/or CIDR ranges into
// prefixes. A bare IP becomes a single-address prefix. The error names the
// offending entry.
func ParseIPRanges(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if strings.Contains(e, "/") {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				return nil, fmt.Errorf("invalid IP/CIDR %q: %w", raw, err)
			}
			if p.Addr().Is4In6() {
				p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
			}
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("invalid IP/CIDR %q: %w", raw, err)
		}
		a = a.Unmap().WithZone("")
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

func prefixesContain(list []netip.Prefix, a netip.Addr) bool {
	for _, p := range list {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IPFilter returns a Gin middleware that enforces an IP whitelist and
// blacklist. Entries may be exact IPs or CIDR ranges (IPv4 or IPv6); they are
// parsed once here and an invalid entry is returned as an error.
// If the blacklist matches, the request is rejected; otherwise, if the
// whitelist is non-empty, only matching addresses are allowed.
//
// Both lists are checked against gin's ClientIP, which only honours
// X-Forwarded-For / X-Real-IP from peers listed in [processor]
// trusted_proxies (see ConfigureTrustedProxies). With no trusted proxies the
// TCP peer address is used.
func IPFilter(whitelist, blacklist []string) (gin.HandlerFunc, error) {
	white, err := ParseIPRanges(whitelist)
	if err != nil {
		return nil, fmt.Errorf("[processor] ip_whitelist: %w", err)
	}
	black, err := ParseIPRanges(blacklist)
	if err != nil {
		return nil, fmt.Errorf("[processor] ip_blacklist: %w", err)
	}
	if len(white) == 0 && len(black) == 0 {
		return func(c *gin.Context) { c.Next() }, nil
	}

	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		addr, perr := netip.ParseAddr(clientIP)
		if perr != nil {
			log.Warnf("API: rejected unparseable client IP %q for %s %s", clientIP, c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		addr = addr.Unmap().WithZone("")

		if prefixesContain(black, addr) {
			log.Warnf("API: rejected blacklisted IP %s for %s %s", addr, c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if len(white) > 0 && !prefixesContain(white, addr) {
			log.Warnf("API: rejected non-whitelisted IP %s for %s %s", addr, c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}, nil
}

// RequestLogger returns a Gin middleware that logs API requests and records
// per-route Prometheus metrics (replacing the old InstrumentAPI wrapper).
// It uses c.FullPath() for the route label so path parameters are collapsed.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)

		path := c.Request.URL.Path

		// Skip logging for high-volume webhook receiver endpoint
		if path == "/" {
			return
		}

		log.Infof("API: %s %s %d %s",
			c.Request.Method, path,
			c.Writer.Status(), duration.Round(time.Millisecond))

		// Record Prometheus metrics using the route template (e.g. /api/tracking/pokemon/:id)
		// so that path parameters don't create unbounded label cardinality.
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path // fallback for unmatched routes
		}
		method := c.Request.Method
		status := "ok"
		if c.Writer.Status() >= 400 {
			status = "error"
		}
		metrics.APIRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
		metrics.APIRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	}
}
