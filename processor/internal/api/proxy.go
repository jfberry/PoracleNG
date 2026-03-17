package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	log "github.com/sirupsen/logrus"
)

// NewAlerterProxy returns a reverse proxy that forwards unhandled /api/ requests
// to the alerter. This lets external tools (PoracleWeb etc.) point at the processor
// and have tracking/config/human APIs transparently proxied through.
func NewAlerterProxy(alerterURL string) http.Handler {
	target, err := url.Parse(alerterURL)
	if err != nil {
		log.Fatalf("Invalid alerter URL %q: %s", alerterURL, err)
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warnf("Proxy to alerter failed: %s %s: %s", r.Method, r.URL.Path, err)
			http.Error(w, "alerter unavailable", http.StatusBadGateway)
		},
	}

	return proxy
}
