package delivery

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Many destinations fetching the same image at once make one download, and a
// later fetch is served from the cache.
func TestImageFetcherSharesDownloads(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		<-release
		_, _ = w.Write([]byte("png"))
	}))
	defer srv.Close()

	f := newImageFetcher(srv.Client())
	var wg sync.WaitGroup
	results := make([][]byte, 10)
	for i := range results {
		wg.Go(func() {
			data, err := f.fetch(srv.URL + "/tile.png")
			if err != nil {
				t.Errorf("fetch %d: %v", i, err)
			}
			results[i] = data
		})
	}
	// Let the goroutines pile up on the single in-flight download.
	for calls.Load() == 0 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()

	for i, r := range results {
		if string(r) != "png" {
			t.Errorf("fetch %d = %q, want png", i, r)
		}
	}
	if _, err := f.fetch(srv.URL + "/tile.png"); err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	// Goroutines that started after the first download finished hit the
	// cache, so whatever the interleaving the server sees one request.
	if n := calls.Load(); n != 1 {
		t.Errorf("server saw %d downloads, want 1", n)
	}
}

// A failed download is not cached: the next fetch tries again.
func TestImageFetcherDoesNotCacheFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("png"))
	}))
	defer srv.Close()

	f := newImageFetcher(srv.Client())
	if _, err := f.fetch(srv.URL); err == nil {
		t.Fatal("first fetch succeeded, want the 502 error")
	}
	data, err := f.fetch(srv.URL)
	if err != nil || string(data) != "png" {
		t.Fatalf("second fetch = (%q, %v), want (png, nil)", data, err)
	}
}
