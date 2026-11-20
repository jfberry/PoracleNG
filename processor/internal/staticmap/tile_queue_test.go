package staticmap

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testFallback = "https://example.com/fallback.png"

// blockingTileserver answers pregenerate POSTs with a tile ID. While block is
// open, requests wait on it (or on their own cancellation). entered receives
// one value per request as it arrives, and cancelled counts requests whose
// context ended while they waited.
type blockingTileserver struct {
	srv       *httptest.Server
	calls     atomic.Int32
	cancelled atomic.Int32
	entered   chan struct{}
	block     chan struct{}
}

func newBlockingTileserver(t *testing.T) *blockingTileserver {
	t.Helper()
	ts := &blockingTileserver{entered: make(chan struct{}, 16), block: make(chan struct{})}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the body: the server only watches for the client going
		// away (and cancels r.Context()) once the body has been read.
		_, _ = io.Copy(io.Discard, r.Body)
		ts.calls.Add(1)
		ts.entered <- struct{}{}
		select {
		case <-ts.block:
			_, _ = w.Write([]byte("tile-id"))
		case <-r.Context().Done():
			ts.cancelled.Add(1)
		}
	}))
	t.Cleanup(func() {
		ts.release()
		ts.srv.Close()
	})
	return ts
}

func (ts *blockingTileserver) release() {
	select {
	case <-ts.block:
	default:
		close(ts.block)
	}
}

func (ts *blockingTileserver) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-ts.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tileserver request never arrived")
	}
}

func newQueueResolver(t *testing.T, ts *blockingTileserver, cfg Config) *Resolver {
	t.Helper()
	cfg.Provider = "tileservercache"
	cfg.ProviderURL = ts.srv.URL
	cfg.FallbackURL = testFallback
	r := New(cfg)
	t.Cleanup(func() {
		ts.release() // let in-flight requests finish so Close doesn't wait out the deadline
		r.Close()
	})
	return r
}

// eventually polls cond until it holds or 5s pass.
func eventually(cond func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

func waitResult(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("tile request never resolved")
		return ""
	}
}

// A request the consumer abandoned while it was still queued must be resolved
// with the fallback without contacting the tileserver: nobody will use the
// tile, and generating it would only delay the requests behind it.
func TestAbandonedQueuedRequestSkipsTileserver(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := newQueueResolver(t, ts, Config{TileserverConcurrency: 1})

	first := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	ts.waitEntered(t) // the only worker is now busy with first

	second := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	second.Abandon()
	ts.release()

	if got := waitResult(t, first.Result); got == testFallback {
		t.Errorf("first tile = fallback, want the generated tile")
	}
	if got := waitResult(t, second.Result); got != testFallback {
		t.Errorf("abandoned tile = %q, want fallback", got)
	}
	if n := ts.calls.Load(); n != 1 {
		t.Errorf("tileserver called %d times, want 1 (abandoned request skipped)", n)
	}
	if s := r.GetStats(); s.Abandoned != 1 {
		t.Errorf("Stats.Abandoned = %d, want 1", s.Abandoned)
	}
}

// Abandoning a request mid-flight cancels the HTTP call, and those
// cancellations must not count toward tripping the circuit breaker: they are
// our own doing, not a tileserver fault.
func TestAbandonCancelsInFlightRequestWithoutTrippingBreaker(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := newQueueResolver(t, ts, Config{
		TileserverConcurrency:      1,
		TileserverFailureThreshold: 2,
		TileserverCooldownMs:       60000,
	})

	for i := range 4 {
		p := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
		ts.waitEntered(t)
		p.Abandon()
		if got := waitResult(t, p.Result); got != testFallback {
			t.Fatalf("request %d = %q, want fallback", i, got)
		}
	}

	if !eventually(func() bool { return ts.cancelled.Load() == 4 }) {
		t.Errorf("tileserver saw %d cancelled requests, want 4", ts.cancelled.Load())
	}
	if r.breaker.IsOpen() {
		t.Error("breaker opened on abandoned requests")
	}
	if s := r.GetStats(); s.Errors != 0 {
		t.Errorf("Stats.Errors = %d, want 0 for abandoned requests", s.Errors)
	}
}

// A request whose deadline passed while it was queued is resolved with the
// fallback without contacting the tileserver.
func TestExpiredQueuedRequestSkipsTileserver(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := newQueueResolver(t, ts, Config{TileserverConcurrency: 1, TileDeadlineMs: 20})

	p := r.newPending("monster", "", nil)
	p.Result = make(chan string, 1)
	time.Sleep(40 * time.Millisecond) // "queued" past the deadline
	r.processTile(tileRequest{pending: p, maptype: "monster", data: map[string]any{}, staticMapType: "staticMap"})

	if got := waitResult(t, p.Result); got != testFallback {
		t.Errorf("expired tile = %q, want fallback", got)
	}
	if n := ts.calls.Load(); n != 0 {
		t.Errorf("tileserver called %d times, want 0", n)
	}
	if s := r.GetStats(); s.Expired != 1 {
		t.Errorf("Stats.Expired = %d, want 1", s.Expired)
	}
}

// A tileserver too slow to answer within the deadline, for requests that had
// the whole deadline to themselves, is a real fault and still opens the
// breaker.
func TestSlowTileserverStillTripsBreaker(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := newQueueResolver(t, ts, Config{
		TileserverConcurrency:      2,
		TileDeadlineMs:             100,
		TileserverFailureThreshold: 2,
		TileserverCooldownMs:       60000,
	})

	a := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	b := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	waitResult(t, a.Result)
	waitResult(t, b.Result)

	if !r.breaker.IsOpen() {
		t.Error("breaker still closed after two requests timed out with their full deadline")
	}
}

// When the queue is full the request is resolved with the fallback at once.
func TestQueueFullResolvesFallback(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := newQueueResolver(t, ts, Config{TileserverConcurrency: 1, TileQueueSize: 1})

	r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	ts.waitEntered(t)                                               // worker busy
	r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "") // fills the queue
	full := r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")

	select {
	case got := <-full.Result:
		if got != testFallback {
			t.Errorf("queue-full tile = %q, want fallback", got)
		}
	default:
		t.Fatal("queue-full tile not resolved immediately")
	}
	if s := r.GetStats(); s.QueueFull != 1 {
		t.Errorf("Stats.QueueFull = %d, want 1", s.QueueFull)
	}
}

// Close resolves every still-queued request on every channel it uses, so a
// Both-mode consumer waiting for bytes isn't left hanging. (The worker may
// also pick the request up before it stops; either way both channels must get
// a value.)
func TestCloseResolvesQueuedBothRequest(t *testing.T) {
	ts := newBlockingTileserver(t)
	r := New(Config{Provider: "tileservercache", ProviderURL: ts.srv.URL, FallbackURL: testFallback, TileserverConcurrency: 1})

	r.SubmitTile("monster", map[string]any{}, "staticMap", nil, "")
	ts.waitEntered(t)
	both := r.SubmitTileBoth("monster", map[string]any{}, "staticMap", nil, "")
	ts.release()
	r.Close()

	waitResult(t, both.Result)
	select {
	case <-both.ResultImg:
	case <-time.After(5 * time.Second):
		t.Fatal("ResultImg never resolved on Close")
	}
}
