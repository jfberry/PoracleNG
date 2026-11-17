package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

const gateFallback = "https://example.com/fallback.png"

// newGateResolver returns a tile resolver against a tileserver handled by h.
func newGateResolver(t *testing.T, h http.HandlerFunc, deadlineMs int) *staticmap.Resolver {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	r := staticmap.New(staticmap.Config{
		Provider:              "tileservercache",
		ProviderURL:           srv.URL,
		FallbackURL:           gateFallback,
		TileserverConcurrency: 1,
		TileDeadlineMs:        deadlineMs,
	})
	t.Cleanup(r.Close)
	return r
}

// A value already waiting must win over an expired timer. With a bare
// select on both, Go picks at random, so a tile that arrived just as the
// deadline passed was thrown away about half the time.
func TestReceiveByPrefersReadyValueAfterDeadline(t *testing.T) {
	for range 200 {
		ch := make(chan string, 1)
		ch <- "tile"
		got, ok := receiveBy(ch, time.Now().Add(-time.Second))
		if !ok || got != "tile" {
			t.Fatalf("receiveBy = (%q, %v), want (\"tile\", true)", got, ok)
		}
	}
	if _, ok := receiveBy(make(chan string), time.Now().Add(20*time.Millisecond)); ok {
		t.Error("receiveBy on an empty channel reported a value")
	}
}

// An inline tile that fails must leave {{staticMap}} as the fallback, not
// unset (which sent the message with no image at all).
func TestResolveTilePendingInlineFailureAppliesFallback(t *testing.T) {
	r := newGateResolver(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, 5000)
	ps := &ProcessorService{}

	target := map[string]any{}
	pending := r.SubmitTileInline("monster", map[string]any{}, "staticMap", target, "ref")
	if img := ps.resolveTilePending(pending, 0, 0); img != nil {
		t.Errorf("bytes = %d, want nil", len(img))
	}
	if got := target["staticMap"]; got != gateFallback {
		t.Errorf("staticMap = %v, want the fallback", got)
	}
}

// When the deadline passes, the gate gives up: fallback applied, counted, and
// the in-flight tileserver request cancelled.
func TestResolveTilePendingTimeoutAbandonsRequest(t *testing.T) {
	var cancelled atomic.Bool
	entered := make(chan struct{}, 1)
	r := newGateResolver(t, func(_ http.ResponseWriter, req *http.Request) {
		// Consume the body: the server only watches for the client going
		// away (and cancels req.Context()) once the body has been read.
		_, _ = io.Copy(io.Discard, req.Body)
		entered <- struct{}{}
		<-req.Context().Done()
		cancelled.Store(true)
	}, 200)
	ps := &ProcessorService{}

	target := map[string]any{}
	pending := r.SubmitTile("monster", map[string]any{}, "staticMap", target, "ref")
	ps.resolveTilePending(pending, 0, 0)

	if got := target["staticMap"]; got != gateFallback {
		t.Errorf("staticMap = %v, want the fallback", got)
	}
	if n := ps.tileStats.timedOut.Load(); n != 1 {
		t.Errorf("timedOut = %d, want 1", n)
	}
	<-entered
	deadline := time.Now().Add(5 * time.Second)
	for !cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelled.Load() {
		t.Error("tileserver request was not cancelled after the gate gave up")
	}
}

// Under render-queue pressure the gate doesn't wait, but a tile that has
// already arrived is still used.
func TestResolveTilePendingPressureUsesReadyTile(t *testing.T) {
	ps := &ProcessorService{}

	ready := &staticmap.TilePending{Result: make(chan string, 1), Deadline: time.Now().Add(time.Minute), Fallback: gateFallback}
	ready.Result <- "https://tiles/ready.png"
	ps.resolveTilePending(ready, 90, 100)
	if n := ps.tileStats.skipped.Load(); n != 0 {
		t.Errorf("skipped = %d after a ready tile, want 0", n)
	}

	notReady := &staticmap.TilePending{Result: make(chan string, 1), Deadline: time.Now().Add(time.Minute), Fallback: gateFallback}
	start := time.Now()
	ps.resolveTilePending(notReady, 90, 100)
	if time.Since(start) > time.Second {
		t.Error("gate waited for the tile under render-queue pressure")
	}
	if n := ps.tileStats.skipped.Load(); n != 1 {
		t.Errorf("skipped = %d after a pending tile, want 1", n)
	}
}

// A per-user weather tile lives in the per-language map; the byte gating must
// see it there, or the tile bytes replace a template's own image.
func TestJobTileURL(t *testing.T) {
	cases := []struct {
		name string
		job  RenderJob
		want string
	}{
		{"base", RenderJob{Enrichment: map[string]any{"staticMap": "base"}}, "base"},
		{"per-language", RenderJob{
			Enrichment:        map[string]any{},
			PerLangEnrichment: map[string]map[string]any{"en": {"staticMap": "user"}},
		}, "user"},
		{"none", RenderJob{Enrichment: map[string]any{}}, ""},
	}
	for _, tc := range cases {
		if got := jobTileURL(tc.job); got != tc.want {
			t.Errorf("%s: jobTileURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTileShortfall(t *testing.T) {
	if got := tileShortfall(staticmap.Stats{}, 0, 0); got != "" {
		t.Errorf("no shortfall = %q, want empty", got)
	}
	got := tileShortfall(staticmap.Stats{Expired: 3, QueueFull: 1}, 2, 0)
	if want := " gaveup:2 expired:3 queuefull:1"; got != want {
		t.Errorf("tileShortfall = %q, want %q", got, want)
	}
}

// newTileModeProcessor loads a DTS store whose `monster` template has no
// tile and whose `monsterChanged` template uses {{staticMap}}.
func newTileModeProcessor(t *testing.T) *ProcessorService {
	t.Helper()
	dir := t.TempDir()
	entries := []map[string]any{
		{"type": "monster", "id": "1", "platform": "discord", "language": "", "default": true,
			"template": map[string]any{"embed": map[string]any{"title": "{{name}}"}}},
		{"type": "monsterChanged", "id": "1", "platform": "discord", "language": "", "default": true,
			"template": map[string]any{"embed": map[string]any{"title": "{{name}}", "image": map[string]any{"url": "{{staticMap}}"}}}},
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dts.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	renderer, err := dts.NewRenderer(dts.RendererConfig{
		ConfigDir: dir, FallbackDir: t.TempDir(), DefaultLocale: "en", DefaultTemplateName: "1",
	})
	if err != nil {
		t.Fatalf("dts.NewRenderer: %v", err)
	}
	return &ProcessorService{
		dtsRenderer: renderer,
		enricher:    &enrichment.Enricher{},
		cfg:         &config.Config{General: config.GeneralConfig{Locale: "en"}},
	}
}

// Prior-only recipients are rendered with monsterChanged, so the tile mode must
// check that template for them, not monster.
func TestPokemonTileModeChecksChangeTemplate(t *testing.T) {
	ps := newTileModeProcessor(t)
	user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Language: "en"}
	change := &tracker.EncounterChange{Type: tracker.ChangeWeatherBoost}

	matchedOnly := ps.tileModeFor(pokemonTileAudiences([]webhook.MatchedUser{user}, nil, change, true), "ref")
	if matchedOnly != enrichment.TileModeSkip {
		t.Errorf("matched users only: mode = %d, want skip (monster has no tile)", matchedOnly)
	}

	withPrior := ps.tileModeFor(pokemonTileAudiences(nil, []webhook.MatchedUser{user}, change, true), "ref")
	if withPrior != enrichment.TileModeURL {
		t.Errorf("prior-only user: mode = %d, want url (monsterChanged uses the tile)", withPrior)
	}

	// An encountered-reveal change doesn't render monsterChanged at all.
	reveal := &tracker.EncounterChange{Type: tracker.ChangeEncountered}
	if got := ps.tileModeFor(pokemonTileAudiences(nil, []webhook.MatchedUser{user}, reveal, true), "ref"); got != enrichment.TileModeSkip {
		t.Errorf("encountered change: mode = %d, want skip", got)
	}
}

// With no monsterNoIv template the renderer uses monster, so the tile mode
// follows monster rather than answering a conservative "uses the tile" for
// the missing template.
func TestPokemonTileModeNoIvFallsBackToMonster(t *testing.T) {
	ps := newTileModeProcessor(t)
	user := webhook.MatchedUser{ID: "u1", Type: "discord:user", Language: "en"}

	got := ps.tileModeFor(pokemonTileAudiences([]webhook.MatchedUser{user}, nil, nil, false), "ref")
	if got != enrichment.TileModeSkip {
		t.Errorf("unencountered, no monsterNoIv template: mode = %d, want skip (monster has no tile)", got)
	}
}
