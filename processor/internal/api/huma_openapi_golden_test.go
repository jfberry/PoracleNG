package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// huma_openapi_golden_test.go is the golden OpenAPI spec test over the huma ops
// that live in package api (in-place /api/* + strict /api/v2/*). It builds a huma
// API with every package-api op registered, marshals the generated OpenAPI 3.1
// document, normalises it to canonical JSON (sorted keys, stable indent), and
// compares it byte-for-byte to the committed golden at testdata/openapi.golden.json.
// Any unintended spec change (a new/renamed path, a changed schema, a dropped
// security scheme) is then caught in CI.
//
// SCOPE — this golden does NOT cover the six autocreate ops (POST /autocreate/run,
// DELETE /autocreate/templates/{name}, GET /autocreate/templates/schema, GET/POST
// /autocreate/templates, POST /autocreate/templates/validate). Those register
// funcs depend on *discordbot.Bot, and api → discordbot → bot/commands → api is an
// import cycle, so they live in package cmd/processor and are guarded by a sibling
// golden there (cmd/processor/huma_autocreate_golden_test.go). The two goldens
// together cover the WHOLE huma surface the production server mounts.
//
// Regenerate the golden after an intentional spec change:
//
//	UPDATE_GOLDEN=1 go test ./internal/api/ -run TestOpenAPIGolden
//
// REGISTRATION APPROACH — test-calls-each (NOT an extracted RegisterAllHumaOps),
// scoped to the package-api ops (the six autocreate ops in cmd/processor are
// guarded by the sibling golden there, see SCOPE above).
// The production registration in cmd/processor/main.go is deeply entangled with
// server setup: the deps the ~50 Register* calls consume are constructed at very
// different points (proc → trackingDeps → roleDeps, whose SessionFunc closes over
// a *discordbot.Bot created hundreds of lines later → summaryDeps → configDeps →
// the command/resolve deps built only after bot init). Bundling all of that into
// one HumaDeps struct + a RegisterAllHumaOps helper would be a large, risky churn
// across the whole startup path for little gain, because the Register* functions
// only build handler CLOSURES — none of them touch their deps at registration
// time (the one read, RegisterV2TrackingSnapshot's len(v2SnapshotProviders), is
// satisfied by sharing one *TrackingDeps across the per-type registers below, and
// the one body precompute, RegisterConfigPoracleWeb's buildPoracleWebResponse,
// only reads zero-value fields). So this test registers each op directly with
// nil/zero stub deps. The trade-off is that this Register* list must be kept in
// sync with main.go by hand — see the pointer comment near the humaAPI setup in
// cmd/processor/main.go. The golden spec is the safety net: if main.go gains an
// endpoint this test doesn't, the golden won't change here, but the production
// /openapi.json will — and reviewers comparing the two will notice. More
// importantly, if a registered op's SCHEMA drifts, this test fails loudly.

const goldenRelPath = "testdata/openapi.golden.json"

// shouldUpdateGolden reports whether the golden file should be rewritten. We use
// an env var rather than a flag.Bool so it composes with `go test ./...` without
// every package needing to declare the flag (flag.Bool would panic on redefine
// if another test in the package also declared -update).
func shouldUpdateGolden() bool {
	return os.Getenv("UPDATE_GOLDEN") == "1"
}

// registerAllHumaOpsForTest registers every package-api huma op the production
// server mounts (cmd/processor/main.go), using nil/zero stub deps. The six
// autocreate ops are deliberately excluded — they live in package cmd/processor
// (import-cycle reasons, see the SCOPE note at the top of this file) and are
// guarded by cmd/processor/huma_autocreate_golden_test.go. The handlers are never
// invoked during spec generation, so stubs that don't panic at registration time
// suffice. Keep this in lockstep with main.go's package-api Register* calls.
func registerAllHumaOpsForTest(humaAPI huma.API) {
	noop := func() error { return nil }

	// Reload (in-place). Summary/description strings MUST mirror the per-call-site
	// text in cmd/processor/main.go so the golden reflects production /docs.
	const (
		stateReloadSummary = "Reload tracking state from the database"
		stateReloadDesc    = "Reloads tracking state (all registered humans plus every tracking rule across all types) from MySQL into the " +
			"in-memory state snapshot, then atomically swaps it in. Existing geofence data (GeoJSON files / Koji) is preserved and NOT " +
			"re-fetched — use /geofence/reload for that. Returns {\"status\":\"ok\"} once the swap completes."

		geofenceReloadSummary = "Full reload including geofences from disk + Koji"
		geofenceReloadDesc    = "Performs a FULL reload: re-reads geofence GeoJSON files from disk and re-fetches Koji geofences, rebuilds the " +
			"spatial index, then reloads tracking state (humans + all tracking rules) from MySQL and atomically swaps the new snapshot in. " +
			"Heavier than /reload (which skips the geofence step). Returns {\"status\":\"ok\"} on success."

		dtsReloadSummary = "Reload DTS templates and partials from disk"
		dtsReloadDesc    = "Reloads DTS message templates and Handlebars partials from config/dts.json and config/dts/ (falling back to the " +
			"bundled fallbacks/ defaults), rebuilding the in-memory template set. Does NOT touch tracking state or geofences. " +
			"Returns {\"status\":\"ok\"} once templates are reloaded."
	)
	RegisterReload(humaAPI, "post-reload", http.MethodPost, "/reload", stateReloadSummary, stateReloadDesc, noop)
	RegisterReload(humaAPI, "get-reload", http.MethodGet, "/reload", stateReloadSummary, stateReloadDesc, noop)
	RegisterReload(humaAPI, "post-geofence-reload", http.MethodPost, "/geofence/reload", geofenceReloadSummary, geofenceReloadDesc, noop)
	RegisterReload(humaAPI, "get-geofence-reload", http.MethodGet, "/geofence/reload", geofenceReloadSummary, geofenceReloadDesc, noop)

	// Weather, stats, geocode.
	RegisterWeather(humaAPI, nil)
	RegisterStatsRarity(humaAPI, func() map[int][]int { return nil })
	RegisterStatsShiny(humaAPI, func() map[int]tracker.ShinyStats { return nil })
	RegisterStatsShinyPossible(humaAPI, func() map[string]any { return nil })
	RegisterGeocode(humaAPI, nil)

	// poracle-test.
	RegisterTest(humaAPI, bot.TestProcessor(nil))

	// Geofence data reads + tile-URL endpoints.
	RegisterGeofenceHash(humaAPI, nil)
	RegisterGeofenceGeoJSON(humaAPI, nil)
	RegisterGeofenceAll(humaAPI, nil)
	RegisterTileEndpoints(humaAPI, HumaTileDeps{})

	// Strict v2 tracking surface. One shared *TrackingDeps so the per-type
	// registers populate v2SnapshotProviders before the snapshot register reads
	// it — exactly as in main.go.
	trackingDeps := &TrackingDeps{}
	RegisterV2TrackingPokemon(humaAPI, trackingDeps)
	RegisterV2TrackingNest(humaAPI, trackingDeps)
	RegisterV2TrackingLure(humaAPI, trackingDeps)
	RegisterV2TrackingMaxbattle(humaAPI, trackingDeps)
	RegisterV2TrackingGym(humaAPI, trackingDeps)
	RegisterV2TrackingFort(humaAPI, trackingDeps)
	RegisterV2TrackingRaid(humaAPI, trackingDeps)
	RegisterV2TrackingEgg(humaAPI, trackingDeps)
	RegisterV2TrackingQuest(humaAPI, trackingDeps)
	RegisterV2TrackingInvasion(humaAPI, trackingDeps)
	RegisterV2TrackingIncident(humaAPI, trackingDeps)
	RegisterV2TrackingSnapshot(humaAPI, trackingDeps) // MUST follow per-type.

	// Strict v2 humans / locations / profiles / mutes.
	RegisterV2Humans(humaAPI, trackingDeps)
	RegisterV2Locations(humaAPI, trackingDeps)
	RegisterV2Profiles(humaAPI, trackingDeps)
	RegisterV2Mutes(humaAPI, trackingDeps)

	// Strict v2 roles.
	RegisterV2Roles(humaAPI, &RoleDeps{})

	// Summary schedules.
	RegisterSummaries(humaAPI, &SummaryDeps{})
	RegisterSummarySet(humaAPI, &SummaryDeps{})

	// DTS reload + writes + reads. Production gates these behind
	// proc.dtsRenderer != nil; the golden spec captures the full surface so we
	// register them unconditionally with nil deps (registration builds closures
	// only).
	RegisterReload(humaAPI, "post-dts-reload", http.MethodPost, "/dts/reload", dtsReloadSummary, dtsReloadDesc, noop)
	RegisterReload(humaAPI, "get-dts-reload", http.MethodGet, "/dts/reload", dtsReloadSummary, dtsReloadDesc, noop)
	RegisterDTSWrites(humaAPI, nil, nil, nil, nil)
	RegisterDTSReads(humaAPI, nil, nil, "", "")

	// Config + masterdata.
	RegisterConfigSchema(humaAPI)
	RegisterConfigPoracleWeb(humaAPI, &config.Config{}) // precomputes a body from zero-value fields.
	RegisterConfigValues(humaAPI, ConfigDeps{})
	RegisterConfigSave(humaAPI, ConfigDeps{})
	RegisterConfigValidate(humaAPI, ConfigDeps{})
	RegisterMasterdataMonsters(humaAPI, nil, nil)
	RegisterMasterdataGrunts(humaAPI, nil)

	// Snapshot inspection + button actions.
	RegisterSnapshotGet(humaAPI, nil)
	RegisterButtonActions(humaAPI, nil)

	// Delivery (+ legacy alias). Summary/description strings MUST mirror the
	// per-call-site text in cmd/processor/main.go so the golden reflects
	// production /docs (canonical vs. alias).
	const (
		deliverMessagesSummary = "Deliver pre-rendered messages"
		deliverMessagesDesc    = "Canonical delivery endpoint. Accepts an array of pre-rendered delivery jobs — each job carries a destination " +
			"(target + type) and a `message` field holding the already-rendered platform payload (arbitrary JSON) — and dispatches them to the " +
			"delivery system. Jobs missing target or type are silently skipped. Returns {\"status\":\"ok\",\"queued\":N} where N is the number of " +
			"jobs accepted. Responds 503 when the delivery dispatcher is not configured."

		postMessageSummary = "Deliver pre-rendered messages (legacy alias)"
		postMessageDesc    = "Legacy/backward-compatibility alias of POST /deliverMessages — identical request body and behaviour (dispatches an " +
			"array of pre-rendered delivery jobs, skips jobs missing target/type, returns {\"status\":\"ok\",\"queued\":N}, 503 when the dispatcher " +
			"is unconfigured). Retained for older clients; new clients should use /deliverMessages."
	)
	RegisterDeliverMessages(humaAPI, "post-deliver-messages", "/deliverMessages", deliverMessagesSummary, deliverMessagesDesc, nil)
	RegisterDeliverMessages(humaAPI, "post-message", "/postMessage", postMessageSummary, postMessageDesc, nil)

	// Command + resolve.
	RegisterCommand(humaAPI, &bot.BotDeps{})
	RegisterResolve(humaAPI, ResolveDeps{})
}

// buildFullHumaSpec builds a fresh huma API with every op registered and returns
// the canonicalised OpenAPI JSON.
func buildFullHumaSpec(t *testing.T) []byte {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	registerAllHumaOpsForTest(humaAPI)

	raw, err := humaAPI.OpenAPI().MarshalJSON()
	if err != nil {
		t.Fatalf("marshal OpenAPI: %v", err)
	}
	return canonicalOpenAPIJSON(t, raw)
}

// canonicalOpenAPIJSON re-encodes JSON with Go's encoding/json, which sorts object keys
// (map[string]any) and applies a stable 2-space indent. This neutralises any
// map-ordering non-determinism in huma's marshaling so the golden is stable.
func canonicalOpenAPIJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal for canonicalisation: %v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("re-encode canonical JSON: %v", err)
	}
	return buf.Bytes()
}

func TestOpenAPIGolden(t *testing.T) {
	got := buildFullHumaSpec(t)
	goldenPath := filepath.FromSlash(goldenRelPath)

	if shouldUpdateGolden() {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to generate)", goldenPath, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("OpenAPI spec drifted from golden %s.\n"+
			"If this change is intentional, regenerate with:\n"+
			"    UPDATE_GOLDEN=1 go test ./internal/api/ -run TestOpenAPIGolden\n"+
			"then inspect the diff before committing.\n%s",
			goldenPath, firstDiffHint(want, got))
	}
}

// TestOpenAPIGoldenDeterministic guards against non-deterministic marshaling:
// two independent builds must produce byte-identical canonical specs. If huma
// ever introduces map-ordering instability that canonicalOpenAPIJSON doesn't absorb,
// this fails fast and locally rather than as flaky golden mismatches in CI.
func TestOpenAPIGoldenDeterministic(t *testing.T) {
	a := buildFullHumaSpec(t)
	b := buildFullHumaSpec(t)
	if !bytes.Equal(a, b) {
		t.Fatalf("OpenAPI marshaling is non-deterministic across builds:\n%s", firstDiffHint(a, b))
	}
}

// firstDiffHint returns a short, line-oriented hint pointing at the first
// differing line so a failing diff is readable without dumping the whole spec.
func firstDiffHint(want, got []byte) string {
	wl := bytes.Split(want, []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	n := min(len(gl), len(wl))
	for i := range n {
		if !bytes.Equal(wl[i], gl[i]) {
			return "first diff at line " + strconv.Itoa(i+1) + ":\n  - want: " + string(wl[i]) + "\n  + got:  " + string(gl[i])
		}
	}
	if len(wl) != len(gl) {
		return "specs differ in length: want " + strconv.Itoa(len(wl)) + " lines, got " + strconv.Itoa(len(gl)) + " lines"
	}
	return "(no line-level diff found)"
}
