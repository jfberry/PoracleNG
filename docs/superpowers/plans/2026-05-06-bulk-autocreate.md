# Bulk Autocreate Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply a `channelTemplate.json` entry across many geofences in one operation, idempotent and triggerable via bot subcommand or API. Includes a separate Discord thread keep-alive sweeper to prevent any Poracle-managed thread from auto-archiving.

**Architecture:** A new `autocreate_sync.go` runner per-rule lives alongside the existing interactive `handleAutocreate`. Both call a shared `applyAutocreate` helper extracted in PR 2. Per-rule state (created categories, channels, threads) is persisted to `config/.cache/autocreate.json` so re-runs reconcile rather than duplicate. The runner is reconciliation-friendly: it diffs against both its cache AND the live Discord state. The thread keep-alive sweeper is independent — it scans the `humans` table for `discord:thread` rows and unarchives them via `s.ChannelEdit`.

**Tech Stack:** Go 1.26, MariaDB/MySQL via sqlx, discordgo v0.29.0, mailgun/raymond/v2 (jfberry fork) for Handlebars filter/params rendering, BurntSushi/toml for config.

---

## File Structure

**Modified:**
- `processor/internal/geofence/geofence.go` — add `Properties map[string]any` to `Fence`; capture extras in `parseGeoJSON`.
- `processor/internal/geofence/geofence_test.go` — add round-trip test.
- `processor/internal/discordbot/autocreate.go` — extract `applyAutocreate` from `handleAutocreate`. Wire `sync` subcommand routing.
- `processor/internal/config/config.go` — add `AutocreateConfig` and `[discord] thread_keep_alive_interval_hours`.
- `processor/internal/api/config_schema.go` — register schema entries for the new fields.
- `processor/cmd/processor/main.go` — start the keep-alive sweeper at boot; wire the `/api/autocreate/run` route.
- `processor/internal/i18n/locale/*.json` — `arg.dryrun`, `arg.reset`, `arg.removals`, `arg.force` keywords (en + de translated; placeholder for the rest).

**Created:**
- `processor/internal/discordbot/autocreate_sync.go` — bulk sync runner: per-rule mutex, reconcile pass, diff loop, removal cascade, summary builder.
- `processor/internal/discordbot/autocreate_cache.go` — load/save of `config/.cache/autocreate.json`.
- `processor/internal/discordbot/autocreate_filter.go` — Handlebars rendering of filter and params against fence context.
- `processor/internal/discordbot/autocreate_sync_test.go`, `autocreate_cache_test.go`, `autocreate_filter_test.go` — unit tests using a stub Discord session.
- `processor/internal/discordbot/threadkeepalive.go` — periodic + startup sweeper; queries `humans` rows of type `discord:thread` and unarchives via `s.ChannelEdit`.
- `processor/internal/discordbot/threadkeepalive_test.go` — sweep behaviour with a stub session.
- `processor/internal/api/autocreate.go` — `POST /api/autocreate/run` handler.

---

# PR 1 — Geofence properties

Adds `Properties map[string]any` to `geofence.Fence`. The GeoJSON parser captures every `properties.*` key not already mapped to a named field, making them available for filter/params expressions in later PRs. KOJI doesn't need changes — it downloads GeoJSON which goes through the same parser.

### Task 1.1: Add Properties field to Fence struct

**Files:**
- Modify: `processor/internal/geofence/geofence.go:13-24` (Fence struct)

- [ ] **Step 1: Add the field**

In `geofence.go`, modify the `Fence` struct:

```go
type Fence struct {
	Name             string         `json:"name"`
	NormalizedName   string         `json:"-"` // lowercased, underscores replaced with spaces (computed at load)
	ID               int            `json:"id"`
	Color            string         `json:"color"`
	Path             [][2]float64   `json:"path,omitempty"`
	Multipath        [][][2]float64 `json:"multipath,omitempty"`
	Group            string         `json:"group"`
	Description      string         `json:"description"`
	UserSelectable   bool           `json:"userSelectable"`
	DisplayInMatches bool           `json:"displayInMatches"`
	// Properties holds user-defined keys from the source's properties block
	// that aren't mapped to a named field above. Used by the bulk-autocreate
	// runner's filter/params Handlebars expressions. Populated by
	// parseGeoJSON; left nil for native-format files.
	Properties map[string]any `json:"-"`
}
```

- [ ] **Step 2: Verify the build still passes**

Run: `cd processor && go build ./...`
Expected: no output (success).

### Task 1.2: Failing test for property capture in parseGeoJSON

**Files:**
- Modify: `processor/internal/geofence/geofence_test.go`

- [ ] **Step 1: Find a sensible insertion point**

Run: `grep -n "^func Test" processor/internal/geofence/geofence_test.go`
Locate an existing test (any will do) — append the new test below the last function in the file.

- [ ] **Step 2: Add the failing test**

Append to `geofence_test.go`:

```go
func TestParseGeoJSONCapturesExtraProperties(t *testing.T) {
	geoJSON := `{
		"type":"FeatureCollection",
		"features":[
			{
				"type":"Feature",
				"properties":{
					"name":"Gent_centrum",
					"group":"Belgium",
					"server":"uk",
					"beserver":true,
					"priority":3
				},
				"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}
			}
		]
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "fences.json")
	if err := os.WriteFile(path, []byte(geoJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fences, err := LoadGeofenceFile(path, "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(fences) != 1 {
		t.Fatalf("expected 1 fence, got %d", len(fences))
	}
	f := fences[0]

	// Named fields stay populated.
	if f.Name != "Gent_centrum" || f.Group != "Belgium" {
		t.Errorf("named fields lost: name=%q group=%q", f.Name, f.Group)
	}

	// Properties should carry the extras.
	if got := f.Properties["server"]; got != "uk" {
		t.Errorf("Properties[server] = %v, want \"uk\"", got)
	}
	if got := f.Properties["beserver"]; got != true {
		t.Errorf("Properties[beserver] = %v, want true", got)
	}
	if got := f.Properties["priority"]; got != float64(3) {
		// JSON numbers decode as float64
		t.Errorf("Properties[priority] = %v (%T), want 3", got, got)
	}

	// Properties must NOT shadow named fields — name / group belong only to
	// the named slots.
	if _, present := f.Properties["name"]; present {
		t.Errorf("Properties should not contain 'name' (it's a named field)")
	}
	if _, present := f.Properties["group"]; present {
		t.Errorf("Properties should not contain 'group' (it's a named field)")
	}
}
```

Make sure these imports are present at the top of the file: `"os"`, `"path/filepath"`, `"testing"`.

- [ ] **Step 3: Run and confirm the test fails**

Run: `cd processor && go test -count=1 -run TestParseGeoJSONCapturesExtraProperties ./internal/geofence/`
Expected: FAIL with output like `Properties[server] = <nil>, want "uk"` (the test asserts properties that aren't being captured).

### Task 1.3: Implement property capture in parseGeoJSON

**Files:**
- Modify: `processor/internal/geofence/geofence.go` (geoJSONFeature, parseGeoJSON)

- [ ] **Step 1: Change geoJSONFeature to keep raw properties**

Replace the `geoJSONFeature` struct:

```go
type geoJSONFeature struct {
	Type       string          `json:"type"`
	Geometry   geoJSONGeometry `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
}
```

(`Properties` becomes a raw message so we can decode twice — once into the named struct, once into a generic map.)

The `geoJSONProperties` struct definition stays the same — we still use it for the named-field decode pass.

- [ ] **Step 2: Update parseGeoJSON to capture extras**

Replace the body of `parseGeoJSON` so it decodes the named subset first, then a flat map, then strips the named keys from the map:

```go
func parseGeoJSON(collection geoJSONCollection, defaultName string) []Fence {
	// Set of property keys promoted to named struct fields. Anything else
	// in the source's properties block ends up in Fence.Properties for use
	// by bulk-autocreate filter/params expressions.
	namedKeys := map[string]bool{
		"name":             true,
		"color":            true,
		"group":            true,
		"description":      true,
		"userSelectable":   true,
		"displayInMatches": true,
	}

	var fences []Fence
	for i, feature := range collection.Features {
		if feature.Type != "Feature" {
			continue
		}

		// Pass 1: decode the named-field subset.
		var props geoJSONProperties
		if len(feature.Properties) > 0 {
			if err := json.Unmarshal(feature.Properties, &props); err != nil {
				log.Warnf("geofence: parse named properties for feature %d: %v", i, err)
				continue
			}
		}

		// Pass 2: decode every property as a generic map, then strip the
		// named keys so they don't shadow the struct fields.
		var extras map[string]any
		if len(feature.Properties) > 0 {
			if err := json.Unmarshal(feature.Properties, &extras); err != nil {
				log.Warnf("geofence: parse extra properties for feature %d: %v", i, err)
				extras = nil
			}
			for k := range namedKeys {
				delete(extras, k)
			}
			if len(extras) == 0 {
				extras = nil
			}
		}

		name := props.Name
		if name == "" {
			prefix := defaultName
			if prefix == "" {
				prefix = "city"
			}
			name = prefix + strconv.Itoa(i+1)
		}
		userSel := true
		if props.UserSelectable != nil {
			userSel = *props.UserSelectable
		}
		dispMatch := true
		if props.DisplayInMatches != nil {
			dispMatch = *props.DisplayInMatches
		}

		fence := Fence{
			Name:             name,
			Color:            props.Color,
			Group:            props.Group,
			Description:      props.Description,
			UserSelectable:   userSel,
			DisplayInMatches: dispMatch,
			Properties:       extras,
		}
		applyGeoJSONGeometry(&fence, feature.Geometry)
		fences = append(fences, fence)
	}
	return fences
}
```

If your `parseGeoJSON` currently inlines the geometry handling (rather than calling `applyGeoJSONGeometry`), keep that inline block exactly as it was — only the per-feature decode preamble and the `Fence{}` literal change. Look at the existing function body to copy the geometry handling verbatim.

Add this import if missing: `log "github.com/sirupsen/logrus"`.

- [ ] **Step 3: Confirm logrus import is present**

Run: `grep -n "sirupsen/logrus" processor/internal/geofence/geofence.go`
Expected: at least one match. If none, add to the import block.

- [ ] **Step 4: Run test, confirm pass**

Run: `cd processor && go test -count=1 -run TestParseGeoJSONCapturesExtraProperties ./internal/geofence/`
Expected: PASS.

- [ ] **Step 5: Run the full geofence test suite to confirm no regressions**

Run: `cd processor && go test -count=1 ./internal/geofence/`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/geofence/geofence.go processor/internal/geofence/geofence_test.go
git commit -m "$(cat <<'EOF'
geofence: capture user-defined Properties from GeoJSON sources

The bulk-autocreate runner needs to filter and parameterise per-fence
on arbitrary user-defined properties (e.g. {{eq server "uk"}} on
KOJI fences that carry a "server" property). Add Properties
map[string]any to Fence; parseGeoJSON does a second decode pass into
a flat map, strips the keys promoted to named fields, and stashes
the rest. Native-format JSON files leave Properties nil — they don't
have a properties block.
EOF
)"
```

---

# PR 2 — Extract `applyAutocreate`

Refactor the per-fence body of `handleAutocreate` into a reusable function. PR 3 will call it from the bulk runner; the existing interactive command keeps its current behaviour (always reset on reuse).

### Task 2.1: Define the applyAutocreate signature and Options struct

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go` (top of file, near other types)

- [ ] **Step 1: Add the options struct and signature**

After the `threadEntry` struct (or wherever the type declarations end before `handleAutocreate`), add:

```go
// applyAutocreateOptions controls the per-fence application behaviour.
type applyAutocreateOptions struct {
	// ResetOnReuse, when true, wipes existing tracking on a reused
	// channel/thread and re-runs the template's commands. The interactive
	// !autocreate path sets this to true; the bulk sync path defaults to
	// false (preserve admin-tweaked tracking) and only sets true when the
	// `reset` keyword is on the trigger.
	ResetOnReuse bool

	// DryRun reports what would happen without touching Discord or the DB.
	DryRun bool
}

// applyAutocreateResult captures what one invocation did, for the caller's
// summary. Fields are populated regardless of DryRun.
type applyAutocreateResult struct {
	CategoryID    string                       // resolved or created category id (empty if template has no category)
	ChannelIDs    map[string]string            // channelName -> id
	ThreadIDs     map[string]map[string]string // channelName -> {label: thread_id}
	Reused        bool                         // true if the master channel already existed
	CommandsRan   int                          // total template commands actually executed
	Errors        []error
}

// reporter abstracts user feedback. The interactive path uses a Discord
// channel writer; the bulk runner uses a structured collector.
type reporter interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}
```

- [ ] **Step 2: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

### Task 2.2: Stub the function signature

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

- [ ] **Step 1: Add the function stub**

After the new types from Task 2.1, add the stub:

```go
// applyAutocreate runs one channelTemplate entry against the supplied args
// inside the given guild. Used by both the interactive !autocreate command
// and the bulk sync runner. The caller controls reset semantics via
// applyAutocreateOptions; see the comment on that struct.
//
// args is the lower-cased arg slice (matching the bot parser's Args);
// rawArgs preserves user-typed case (matching Parser.RawArgs). Both are
// indexed positionally into the template's {0}, {1}, ... placeholders.
func (b *Bot) applyAutocreate(
	s *discordgo.Session,
	tmpl *channelTemplate,
	args []string,
	rawArgs []string,
	guildID string,
	rep reporter,
	opts applyAutocreateOptions,
) applyAutocreateResult {
	// Stub — body filled in by Task 2.3.
	return applyAutocreateResult{}
}
```

- [ ] **Step 2: Verify build**

Run: `cd processor && go build ./...`
Expected: success. The stub is unused but compiles.

### Task 2.3: Move the per-fence body from handleAutocreate into applyAutocreate

This is the bulk of the refactor. The goal is mechanical move — same behaviour, just relocated. The interactive command sets `opts.ResetOnReuse = true` to preserve current behaviour.

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

- [ ] **Step 1: Read the current handleAutocreate body**

Open the file. The function spans roughly from `func (b *Bot) handleAutocreate` to the end of its closing brace. Identify:
- The arg parsing / template lookup at the top (stays in `handleAutocreate`).
- The `subArgsUnder` and `rawSubArgs` setup (stays).
- The category creation block.
- The channels-loop block (per-channel reuse, target registration, command execution, threadEntries call).
- The picker-post call at the end.

The stays-in-place parts read raw args from the user, look up `tmpl`, derive `subArgs/rawSubArgs/subArgsUnder`. After that, the rest of the function (category through end) becomes the body of `applyAutocreate`.

- [ ] **Step 2: Replace the function bodies**

Implementation strategy:

1. Keep `handleAutocreate`'s preamble (admin check, guild parse, template lookup, derive args). After deriving `subArgsUnder` and `rawSubArgs`, replace the rest with a single call:

```go
// Adapt the message-channel sender into a reporter for live feedback.
rep := newDiscordChannelReporter(s, m.ChannelID)

result := b.applyAutocreate(s, tmpl, args[1:], rawSubArgs, guildID, rep, applyAutocreateOptions{
	ResetOnReuse: true, // interactive: always wipe + re-apply on reuse
	DryRun:       false,
})
_ = result // interactive path doesn't need the structured result
```

(`args[1:]` here is the arg slice without the template name. If your existing code uses a slice variable already, reuse that.)

2. Move the deleted body verbatim into `applyAutocreate`. Replace any references inside it:
   - `m.ChannelID` for status messages → `rep.Info(...)`, `rep.Warn(...)`, `rep.Error(...)`.
   - `subArgs` → `args` (the function parameter).
   - `rawSubArgs` → `rawArgs`.
   - `subArgsUnder` → derive locally inside the function.

   Specifically, near the top of `applyAutocreate`, add:

```go
// Underscore-restored args used by the existing per-channel target/webhook
// naming code paths (channels with mid-name spaces would otherwise become
// targets with a literal space in their stored name).
subArgsUnder := make([]string, len(rawArgs))
for i, a := range rawArgs {
	subArgsUnder[i] = strings.ReplaceAll(a, " ", "_")
}
result := applyAutocreateResult{
	ChannelIDs: map[string]string{},
	ThreadIDs:  map[string]map[string]string{},
}
```

   At the various points inside the body where the existing code calls `s.ChannelMessageSend(m.ChannelID, ...)`, change to `rep.Info(...)` or `rep.Warn(...)` as appropriate (info for `>>` lines, warn for failures that don't abort).

   Replace the existing `if existingID := b.findChannelByName(...); existingID != ""` reuse path: still reuse the channel, but only call `b.resetChannelTracking(s, existingID)` when `opts.ResetOnReuse` is true. Otherwise skip the reset and leave existing tracking alone:

```go
if existingID := b.findChannelByName(s, guildID, categoryID, channelName); existingID != "" {
	if opts.ResetOnReuse {
		rep.Info(fmt.Sprintf(">> Reusing existing channel %s — resetting tracking", channelName))
		if !opts.DryRun {
			b.resetChannelTracking(s, existingID)
		}
	} else {
		rep.Info(fmt.Sprintf(">> Reusing existing channel %s — tracking left alone", channelName))
	}
	ch, err := s.Channel(existingID)
	if err != nil {
		rep.Warn(fmt.Sprintf("Failed to fetch existing channel %s: %v", channelName, err))
		continue
	}
	channel = ch
	result.Reused = true
} else {
	if opts.DryRun {
		rep.Info(fmt.Sprintf(">> [dry-run] Would create channel %s", channelName))
		continue
	}
	ch, err := s.GuildChannelCreateComplex(guildID, createData)
	if err != nil {
		rep.Warn(fmt.Sprintf("Failed to create channel %s: %v", channelName, err))
		continue
	}
	rep.Info(fmt.Sprintf(">> Creating %s", channelName))
	channel = ch
}
```

   Similarly for the per-thread reuse block in `createThreadsForChannel`: only call `db.DeleteHumanAndTracking(b.DB, threadID)` when `opts.ResetOnReuse` is true. The simplest plumbing is to add a parameter to `createThreadsForChannel`:

```go
func (b *Bot) createThreadsForChannel(
	s *discordgo.Session,
	rep reporter,
	guildID, masterChannelID string,
	chDef channelEntry,
	subArgs []string,
	opts applyAutocreateOptions,
) []threadCacheEntry {
```

   And inside, gate the existing `db.DeleteHumanAndTracking` reset call on `opts.ResetOnReuse`.

   Skip command execution at the end of channel/thread setup if not reusing-with-reset AND existed before — actually no: commands always run for newly-created channels/threads, and only run for reused ones when `opts.ResetOnReuse` is true. Wrap the existing `for _, cmdText := range chDef.Commands` loop:

```go
runCommands := !result.Reused || opts.ResetOnReuse
if runCommands && !opts.DryRun {
	quotedSubArgs := quoteForCommand(subArgsUnder)
	for _, cmdText := range chDef.Commands {
		// ... existing command-parsing-and-execution body unchanged ...
		result.CommandsRan++
	}
}
```

   (Same gating in the thread loop.)

   Add `result.CategoryID = categoryID` and `result.ChannelIDs[channelName] = channel.ID` at the right points so the caller has a structured summary.

- [ ] **Step 3: Add the discordChannelReporter type**

At the bottom of `autocreate.go`, after `quoteForCommand`:

```go
// discordChannelReporter implements reporter by sending each message to a
// Discord channel via the active session. Used by the interactive
// !autocreate path so the user sees live progress in the channel.
type discordChannelReporter struct {
	s         *discordgo.Session
	channelID string
}

func newDiscordChannelReporter(s *discordgo.Session, channelID string) reporter {
	return &discordChannelReporter{s: s, channelID: channelID}
}

func (r *discordChannelReporter) Info(msg string)  { r.s.ChannelMessageSend(r.channelID, msg) }
func (r *discordChannelReporter) Warn(msg string)  { r.s.ChannelMessageSend(r.channelID, msg) }
func (r *discordChannelReporter) Error(msg string) { r.s.ChannelMessageSend(r.channelID, msg) }
```

- [ ] **Step 4: Verify build**

Run: `cd processor && go build ./...`
Expected: success. If unused-variable errors crop up, those are leftovers from the refactor — read the error and remove.

### Task 2.4: Verify interactive path still works (smoke test)

**Files:**
- (No changes — this is a verification step.)

- [ ] **Step 1: Run the existing test suite**

Run: `cd processor && go test -count=1 ./internal/discordbot/...`
Expected: all tests pass.

- [ ] **Step 2: Run vet**

Run: `cd processor && go vet ./internal/discordbot/...`
Expected: no warnings.

### Task 2.5: Commit

- [ ] **Step 1: Commit**

```bash
git add processor/internal/discordbot/autocreate.go
git commit -m "$(cat <<'EOF'
discordbot: extract applyAutocreate from handleAutocreate

PR 3's bulk sync runner needs to invoke the per-channel autocreate
flow many times in a row against arbitrary args. Pull that body out
of handleAutocreate into a reusable applyAutocreate(s, tmpl, args,
rawArgs, guildID, reporter, options).

The interactive path sets ResetOnReuse=true to preserve today's
behaviour (reuse always wipes + re-applies template commands). The
bulk runner will default to false so scheduled syncs don't churn
admin-tweaked tracking. DryRun keyed off opts skips Discord and DB
writes.

Reporter interface lets the interactive path send live messages to
the user's Discord channel and the bulk runner collect structured
output for a summary at the end.
EOF
)"
```

---

# PR 3 — Bulk runner core (no triggers yet)

Adds `[autocreate]` config, the cache file, filter/params Handlebars rendering, and the per-rule sync runner. The runner is exercised by unit tests calling it directly — no bot subcommand or API endpoint yet.

### Task 3.1: Add config struct

**Files:**
- Modify: `processor/internal/config/config.go`
- Modify: `processor/internal/config/config_test.go` (if it exists; otherwise create)

- [ ] **Step 1: Add structs near the existing top-level Config**

Locate the existing top-level `Config` struct in `config.go`. Add a new field for autocreate, following the pattern of the existing sections:

```go
Autocreate AutocreateConfig `toml:"autocreate"`
```

Add the new types beside the other top-level type declarations:

```go
// AutocreateConfig holds the bulk-autocreate runner configuration.
type AutocreateConfig struct {
	// RemovalSafetyMaxPercent: abort the removal phase if removing more
	// than this percent of cached fences (cache must be ≥10 entries for
	// the check to engage). 0 disables the safety check.
	RemovalSafetyMaxPercent int `toml:"removal_safety_max_percent"`

	// Rules is the per-rule list. Each rule produces channels under one
	// guild from one channelTemplate.json entry.
	Rules []AutocreateRule `toml:"rules"`
}

// AutocreateRule is one [[autocreate.rules]] entry.
type AutocreateRule struct {
	Name          string   `toml:"name"`           // unique rule identifier
	Guild         string   `toml:"guild"`          // Discord guild ID
	Template      string   `toml:"template"`       // channelTemplate.json entry name
	Filter        string   `toml:"filter"`         // optional Handlebars expression; empty = match all
	Params        []string `toml:"params"`         // each element rendered per fence; positional template args
	RemoveMissing bool     `toml:"remove_missing"` // permits orphan removal when the trigger requests it
}
```

- [ ] **Step 2: Add a thread-keep-alive field to DiscordConfig**

Locate `DiscordConfig` in the same file. Add:

```go
// ThreadKeepAliveIntervalHours is the cadence (hours) at which the
// background sweeper unarchives Poracle-managed Discord threads.
// 0 disables the sweeper. Values >168 (7 days) are clamped to 168.
ThreadKeepAliveIntervalHours int `toml:"thread_keep_alive_interval_hours"`
```

- [ ] **Step 3: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

### Task 3.2: Defaults and validation

**Files:**
- Modify: `processor/internal/config/config.go` (in the `Load` function or wherever defaults are set)

- [ ] **Step 1: Locate the existing default-application code**

Run: `grep -n "func.*Load\b\|cfg\\.\\(General\\|Discord\\|Tuning\\)\\..*= " processor/internal/config/config.go | head -20`
Expected: look for a section near the end of `Load` (or in a `defaultsApplied`-style helper) that fills in zero-value defaults — pattern is `if cfg.X == 0 { cfg.X = N }`.

- [ ] **Step 2: Add defaults for the new fields**

In that same defaults section, add:

```go
// Autocreate defaults.
if cfg.Autocreate.RemovalSafetyMaxPercent < 0 {
	cfg.Autocreate.RemovalSafetyMaxPercent = 0
}
// 0 = disabled (no upper-bound check); otherwise leave whatever the
// user set, including the implicit default of 0.

// Thread keep-alive defaults: 24h if unspecified, clamp to 168h max.
if cfg.Discord.ThreadKeepAliveIntervalHours < 0 {
	cfg.Discord.ThreadKeepAliveIntervalHours = 0
}
if cfg.Discord.ThreadKeepAliveIntervalHours > 168 {
	cfg.Discord.ThreadKeepAliveIntervalHours = 168
}
// Note: 0 means "disabled". A user-omitted field stays 0; a user wanting
// the default 24h cadence will set 24 explicitly. (Better than implicit
// background side-effects on every install.)
```

For the validation that rejects malformed rules, add a helper called from `Load` after the TOML decode:

```go
// validateAutocreateRules surfaces obvious config mistakes early. Errors
// here abort startup; warnings are logged but allow startup to continue.
func validateAutocreateRules(cfg *Config) error {
	seen := map[string]bool{}
	for i, r := range cfg.Autocreate.Rules {
		if r.Name == "" {
			return fmt.Errorf("[[autocreate.rules]] entry %d: name is required", i)
		}
		if seen[r.Name] {
			return fmt.Errorf("[[autocreate.rules]] entry %d: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = true
		if r.Guild == "" {
			return fmt.Errorf("[[autocreate.rules]] %s: guild is required", r.Name)
		}
		if r.Template == "" {
			return fmt.Errorf("[[autocreate.rules]] %s: template is required", r.Name)
		}
		if len(r.Params) == 0 {
			return fmt.Errorf("[[autocreate.rules]] %s: params must contain at least one element", r.Name)
		}
	}
	return nil
}
```

Call it from `Load` right after the TOML decode (and any defaults-application step):

```go
if err := validateAutocreateRules(&cfg); err != nil {
	return nil, err
}
```

- [ ] **Step 3: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 4: Commit (config foundation only)**

```bash
git add processor/internal/config/config.go
git commit -m "$(cat <<'EOF'
config: add [autocreate] section + thread keep-alive cadence

Adds the AutocreateConfig + AutocreateRule structs that PR 3 onward
will consume, plus DiscordConfig.ThreadKeepAliveIntervalHours for
PR 7's sweeper. Includes defaults (0 = disabled), upper-bound clamp
on the keep-alive cadence (max 168h = Discord's archive ceiling),
and basic per-rule validation (unique name, required fields).

No runtime behaviour change — fields are unused until the runner
lands.
EOF
)"
```

### Task 3.3: Cache file load/save

**Files:**
- Create: `processor/internal/discordbot/autocreate_cache.go`
- Create: `processor/internal/discordbot/autocreate_cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `processor/internal/discordbot/autocreate_cache_test.go`:

```go
package discordbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutocreateCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autocreate.json")

	original := autocreateCache{
		"uk-areas": &autocreateRuleState{
			GuildID: "12345",
			Categories: []autocreateCategory{
				{Name: "Belgium", ID: "cat1"},
			},
			Fences: map[string]*autocreateFenceState{
				"Gent_centrum": {
					CategoryID: "cat1",
					ChannelID:  "ch1",
					ThreadIDs:  map[string]string{"Hundos": "th1"},
				},
			},
		},
	}

	if err := saveAutocreateCache(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadAutocreateCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded["uk-areas"].GuildID; got != "12345" {
		t.Errorf("GuildID = %q, want %q", got, "12345")
	}
	if got := loaded["uk-areas"].Fences["Gent_centrum"].ChannelID; got != "ch1" {
		t.Errorf("ChannelID = %q, want %q", got, "ch1")
	}
	if got := loaded["uk-areas"].Fences["Gent_centrum"].ThreadIDs["Hundos"]; got != "th1" {
		t.Errorf("ThreadIDs[Hundos] = %q, want %q", got, "th1")
	}
}

func TestAutocreateCacheLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	loaded, err := loadAutocreateCache(path)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if loaded == nil {
		t.Fatal("missing file should return empty map, not nil")
	}
	if len(loaded) != 0 {
		t.Errorf("missing file should return empty map, got %d entries", len(loaded))
	}
}

func TestAutocreateCacheSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "autocreate.json")

	if err := saveAutocreateCache(path, autocreateCache{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd processor && go test -count=1 -run TestAutocreateCache ./internal/discordbot/`
Expected: build error — types not defined.

- [ ] **Step 3: Implement the cache**

Create `processor/internal/discordbot/autocreate_cache.go`:

```go
package discordbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// autocreateCache is the on-disk state for bulk-autocreate runs, keyed by
// rule name. Loaded at startup, saved at the end of each sync.
type autocreateCache map[string]*autocreateRuleState

// autocreateRuleState records what one rule's last sync produced. The
// runner uses this for diff (which fences are already created), reconcile
// (which IDs are still valid Discord-side), and cleanup (which categories
// might be empty after orphan removal).
type autocreateRuleState struct {
	GuildID    string                            `json:"guild_id"`
	Categories []autocreateCategory              `json:"categories"`
	Fences     map[string]*autocreateFenceState  `json:"fences"`
	LastSync   time.Time                         `json:"last_sync,omitempty"`
}

// autocreateCategory tracks a category created (or reused) by this rule.
// Indexed by name so the sort and removal-when-empty steps can locate it.
type autocreateCategory struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// autocreateFenceState is one geofence's mapped Discord state. Keyed in
// the rule's Fences map by the original-case fence name (so the case-
// preserving RawArgs work end-to-end).
type autocreateFenceState struct {
	CategoryID string            `json:"category_id"`
	ChannelID  string            `json:"channel_id"`
	ThreadIDs  map[string]string `json:"thread_ids,omitempty"`
}

// loadAutocreateCache reads the JSON file at the given path. A missing
// file returns an empty cache rather than an error — the first sync run
// against a clean install populates the file.
func loadAutocreateCache(path string) (autocreateCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return autocreateCache{}, nil
		}
		return nil, fmt.Errorf("read autocreate cache %s: %w", path, err)
	}
	cache := autocreateCache{}
	if len(data) == 0 {
		return cache, nil
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse autocreate cache %s: %w", path, err)
	}
	return cache, nil
}

// saveAutocreateCache writes the cache atomically (temp file + rename)
// so a crash mid-write doesn't leave a truncated JSON file behind.
// Creates the parent directory if it doesn't exist.
func saveAutocreateCache(path string, cache autocreateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create autocreate cache dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal autocreate cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write autocreate cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename autocreate cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd processor && go test -count=1 -run TestAutocreateCache ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/autocreate_cache.go processor/internal/discordbot/autocreate_cache_test.go
git commit -m "$(cat <<'EOF'
discordbot: persistent cache for bulk-autocreate runs

Load/save of config/.cache/autocreate.json. Keyed by rule name so
two rules can't see each other's state. Atomic write via temp-and-
rename so a crash mid-save doesn't truncate. Missing file returns
an empty cache (first sync against a clean install just works).
EOF
)"
```

### Task 3.4: Filter and params rendering

**Files:**
- Create: `processor/internal/discordbot/autocreate_filter.go`
- Create: `processor/internal/discordbot/autocreate_filter_test.go`

- [ ] **Step 1: Write the failing test**

Create `processor/internal/discordbot/autocreate_filter_test.go`:

```go
package discordbot

import (
	"reflect"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func init() {
	// Ensure DTS helpers (eq, ne, and, or, not, ...) are registered before
	// the filter tests run. The renderer registers them on first use; we
	// force that here by touching its public API.
	dts.RegisterHelpers()
}

func TestRenderFilter_EmptyMatchesAll(t *testing.T) {
	f := geofence.Fence{Name: "Gent_centrum"}
	ok, err := renderFilter("", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("empty filter should match")
	}
}

func TestRenderFilter_TruthyProperty(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"beserver": true},
	}
	ok, err := renderFilter("{{beserver}}", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("expected truthy match for beserver=true")
	}

	f.Properties["beserver"] = false
	ok, err = renderFilter("{{beserver}}", f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error("beserver=false should not match")
	}
}

func TestRenderFilter_EqHelper(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"server": "uk"},
	}
	ok, err := renderFilter(`{{eq server "uk"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error(`expected {{eq server "uk"}} to match`)
	}

	ok, err = renderFilter(`{{eq server "ie"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error(`{{eq server "ie"}} should not match a uk fence`)
	}
}

func TestRenderFilter_NamedFieldsAvailable(t *testing.T) {
	f := geofence.Fence{Name: "Gent_centrum", Group: "Belgium"}
	ok, err := renderFilter(`{{eq group "Belgium"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error(`group should be available alongside Properties`)
	}
}

func TestRenderFilter_NamedFieldShadowsProperty(t *testing.T) {
	// Edge case: if Properties happened to contain "name", the named
	// field must still win.
	f := geofence.Fence{
		Name:       "Real_name",
		Properties: map[string]any{"name": "Decoy"},
	}
	ok, err := renderFilter(`{{eq name "Real_name"}}`, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("named Name field should win over Properties[\"name\"]")
	}
}

func TestRenderFilter_FalseStringNotTruthy(t *testing.T) {
	// Truthiness rule: rendered string is truthy unless trimmed value is
	// "", "false", or "0".
	f := geofence.Fence{Properties: map[string]any{"v": "false"}}
	ok, _ := renderFilter("{{v}}", f)
	if ok {
		t.Error(`literal "false" should not be truthy`)
	}
	f.Properties["v"] = "0"
	ok, _ = renderFilter("{{v}}", f)
	if ok {
		t.Error(`literal "0" should not be truthy`)
	}
	f.Properties["v"] = "  "
	ok, _ = renderFilter("{{v}}", f)
	if ok {
		t.Error("whitespace should not be truthy")
	}
}

func TestRenderParams_Positional(t *testing.T) {
	f := geofence.Fence{
		Name:  "Gent_centrum",
		Group: "Belgium",
	}
	out, err := renderParams([]string{"{{group}}", "{{name}}"}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"Belgium", "Gent_centrum"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestRenderParams_RendersAgainstProperties(t *testing.T) {
	f := geofence.Fence{
		Name:       "Gent_centrum",
		Properties: map[string]any{"region": "VL"},
	}
	out, err := renderParams([]string{"{{region}}-{{name}}"}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out[0] != "VL-Gent_centrum" {
		t.Errorf("got %q, want VL-Gent_centrum", out[0])
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd processor && go test -count=1 -run "TestRenderFilter|TestRenderParams" ./internal/discordbot/`
Expected: build error — `renderFilter` / `renderParams` not defined.

- [ ] **Step 3: Implement**

Create `processor/internal/discordbot/autocreate_filter.go`:

```go
package discordbot

import (
	"fmt"
	"strings"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// fenceContext builds the data context that filter and params expressions
// see during rendering. Named Fence fields are populated first, then
// properties are merged in — but only for keys that don't collide with a
// named field, so the struct fields always win.
func fenceContext(f geofence.Fence) map[string]any {
	ctx := map[string]any{
		"name":             f.Name,
		"group":            f.Group,
		"description":      f.Description,
		"color":            f.Color,
		"userSelectable":   f.UserSelectable,
		"displayInMatches": f.DisplayInMatches,
	}
	for k, v := range f.Properties {
		if _, exists := ctx[k]; exists {
			continue
		}
		ctx[k] = v
	}
	return ctx
}

// isTruthy applies the bulk-autocreate truthiness rule: trimmed string is
// truthy unless empty, "false", or "0".
func isTruthy(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || t == "false" || t == "0" {
		return false
	}
	return true
}

// renderFilter evaluates a Handlebars expression against a fence and
// reports whether the fence matches. An empty expression matches all.
func renderFilter(expr string, f geofence.Fence) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	tmpl, err := raymond.Parse(expr)
	if err != nil {
		return false, fmt.Errorf("parse filter %q: %w", expr, err)
	}
	out, err := tmpl.Exec(fenceContext(f))
	if err != nil {
		return false, fmt.Errorf("render filter %q: %w", expr, err)
	}
	return isTruthy(out), nil
}

// renderParams renders each element of params[] against the fence context
// and returns the resulting strings, one per input. Used by the bulk
// runner to derive the args passed to applyAutocreate.
func renderParams(params []string, f geofence.Fence) ([]string, error) {
	out := make([]string, len(params))
	ctx := fenceContext(f)
	for i, p := range params {
		tmpl, err := raymond.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("parse params[%d] %q: %w", i, p, err)
		}
		s, err := tmpl.Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("render params[%d] %q: %w", i, p, err)
		}
		out[i] = s
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd processor && go test -count=1 -run "TestRenderFilter|TestRenderParams" ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/autocreate_filter.go processor/internal/discordbot/autocreate_filter_test.go
git commit -m "$(cat <<'EOF'
discordbot: Handlebars filter + params rendering for bulk autocreate

renderFilter evaluates a per-rule expression against a fence and
returns truthy/falsy. renderParams produces positional arg strings
from the rule's params[] entries. Both share a fenceContext that
merges named Fence fields with the Properties map (named fields
take precedence on conflict).

Truthiness rule mirrors the spec: trimmed value !"" && !"false"
&& !"0". Helpers (eq, ne, and, or, not, ...) come from the DTS
helper registry — same engine, same dialect users learn for DTS
templates.
EOF
)"
```

### Task 3.5: Bulk runner core (create + reuse paths)

**Files:**
- Create: `processor/internal/discordbot/autocreate_sync.go`
- Create: `processor/internal/discordbot/autocreate_sync_test.go`

- [ ] **Step 1: Write the failing test (high-level shape)**

Create `processor/internal/discordbot/autocreate_sync_test.go`:

```go
package discordbot

import (
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

func TestSyncRule_FilterSelectsFences(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "Belgium", Properties: map[string]any{"server": "uk"}},
		{Name: "Bruges",       Group: "Belgium", Properties: map[string]any{"server": "ie"}},
		{Name: "Antwerp",      Group: "Belgium", Properties: map[string]any{"server": "uk"}},
	}
	rule := config.AutocreateRule{
		Name:     "uk-areas",
		Guild:    "g1",
		Template: "area",
		Filter:   `{{eq server "uk"}}`,
		Params:   []string{"{{group}}", "{{name}}"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 2 {
		t.Fatalf("expected 2 fences after filter, got %d", len(res.toCreate))
	}
	wantNames := map[string]bool{"Gent_centrum": true, "Antwerp": true}
	for _, c := range res.toCreate {
		if !wantNames[c.fence.Name] {
			t.Errorf("unexpected fence %q in create set", c.fence.Name)
		}
	}
}

func TestSyncRule_ClassifiesReusedAndOrphan(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum", Group: "Belgium"},
		{Name: "Antwerp",      Group: "Belgium"},
	}
	state := autocreateRuleState{
		Fences: map[string]*autocreateFenceState{
			"Gent_centrum": {ChannelID: "ch_gent"}, // present in both → reuse
			"Bruges":       {ChannelID: "ch_bru"},  // only in cache → orphan
		},
	}
	rule := config.AutocreateRule{
		Name:     "uk-areas",
		Guild:    "g1",
		Template: "area",
		Params:   []string{"{{group}}", "{{name}}"},
	}

	res := classifyFences(rule, fences, state)

	if len(res.toCreate) != 1 || res.toCreate[0].fence.Name != "Antwerp" {
		t.Errorf("toCreate = %+v, want only Antwerp", namesOf(res.toCreate))
	}
	if len(res.toReuse) != 1 || res.toReuse[0].fence.Name != "Gent_centrum" {
		t.Errorf("toReuse = %+v, want only Gent_centrum", namesOf(res.toReuse))
	}
	if len(res.orphans) != 1 || res.orphans[0] != "Bruges" {
		t.Errorf("orphans = %v, want [Bruges]", res.orphans)
	}
}

func TestSyncRule_FilterErrorSkipsFence(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum"},
	}
	rule := config.AutocreateRule{
		Name:   "x",
		Filter: `{{this is broken handlebars`,
		Params: []string{"{{name}}"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 0 || len(res.toReuse) != 0 {
		t.Errorf("broken filter should skip fence; got create=%d reuse=%d", len(res.toCreate), len(res.toReuse))
	}
	if len(res.skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(res.skipped))
	}
}

func TestSyncRule_ParamsErrorSkipsFence(t *testing.T) {
	fences := []geofence.Fence{
		{Name: "Gent_centrum"},
	}
	rule := config.AutocreateRule{
		Name:   "x",
		Params: []string{"{{this is also broken"},
	}

	res := classifyFences(rule, fences, autocreateRuleState{})

	if len(res.toCreate) != 0 {
		t.Errorf("expected 0 to-create when params render fails")
	}
	if len(res.skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(res.skipped))
	}
}

func namesOf(items []syncFenceCandidate) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.fence.Name
	}
	return out
}

func TestSyncCacheKey_PathFromBaseDir(t *testing.T) {
	got := syncCachePath("/etc/poracle")
	want := filepath.Join("/etc/poracle", "config", ".cache", "autocreate.json")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run, confirm failures**

Run: `cd processor && go test -count=1 -run "TestSyncRule|TestSyncCacheKey" ./internal/discordbot/`
Expected: build error (`classifyFences`, `syncCachePath`, etc. undefined).

- [ ] **Step 3: Implement classification + path helper**

Create `processor/internal/discordbot/autocreate_sync.go`:

```go
package discordbot

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// syncFenceCandidate pairs a fence with its rendered params for
// downstream consumption by applyAutocreate.
type syncFenceCandidate struct {
	fence  geofence.Fence
	params []string
}

// syncSkip records a fence that couldn't be processed (filter or params
// render error). Surfaced in the final summary.
type syncSkip struct {
	Fence  string
	Reason string
}

// syncClassifyResult is the output of classifyFences — the diff between
// the rule's filter, the live geofence list, and the cache state.
type syncClassifyResult struct {
	toCreate []syncFenceCandidate // fence matches filter, not in cache
	toReuse  []syncFenceCandidate // fence matches filter, present in cache
	orphans  []string             // fence in cache, no longer matches (or removed from geojson)
	skipped  []syncSkip           // filter or params render error
}

// classifyFences runs the rule's filter over fences, renders params for
// matches, and diffs against the cache state. Pure function — no Discord
// or DB calls — so it's easy to test directly.
func classifyFences(rule config.AutocreateRule, fences []geofence.Fence, state autocreateRuleState) syncClassifyResult {
	var res syncClassifyResult
	matched := map[string]bool{}

	for _, f := range fences {
		ok, err := renderFilter(rule.Filter, f)
		if err != nil {
			res.skipped = append(res.skipped, syncSkip{
				Fence:  f.Name,
				Reason: fmt.Sprintf("filter render error: %v", err),
			})
			continue
		}
		if !ok {
			continue
		}

		params, err := renderParams(rule.Params, f)
		if err != nil {
			res.skipped = append(res.skipped, syncSkip{
				Fence:  f.Name,
				Reason: fmt.Sprintf("params render error: %v", err),
			})
			continue
		}

		matched[f.Name] = true
		cand := syncFenceCandidate{fence: f, params: params}
		if state.Fences != nil && state.Fences[f.Name] != nil {
			res.toReuse = append(res.toReuse, cand)
		} else {
			res.toCreate = append(res.toCreate, cand)
		}
	}

	// Orphans: in cache but no longer matched.
	for name := range state.Fences {
		if !matched[name] {
			res.orphans = append(res.orphans, name)
		}
	}
	return res
}

// syncCachePath resolves the autocreate cache path against the project's
// base directory.
func syncCachePath(baseDir string) string {
	return filepath.Join(baseDir, "config", ".cache", "autocreate.json")
}

// autocreateSyncer holds the mutable state the bulk runner needs across
// invocations. Per-rule mutexes prevent overlapping syncs.
type autocreateSyncer struct {
	mu       sync.Mutex
	ruleLocks map[string]*sync.Mutex
}

func newAutocreateSyncer() *autocreateSyncer {
	return &autocreateSyncer{ruleLocks: map[string]*sync.Mutex{}}
}

// lockRule returns the rule's mutex (creating it on first use). Caller is
// expected to call .Lock() / .Unlock() and decide what to do if a sync is
// already in flight.
func (a *autocreateSyncer) lockRule(name string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	m, ok := a.ruleLocks[name]
	if !ok {
		m = &sync.Mutex{}
		a.ruleLocks[name] = m
	}
	return m
}

// SyncRuleOptions controls per-invocation flags for a sync run.
type SyncRuleOptions struct {
	DryRun   bool
	Reset    bool
	Removals bool
	Force    bool
}

// SyncRuleResult is the structured output of a single rule's sync run.
type SyncRuleResult struct {
	Rule     string         `json:"rule"`
	Matched  int            `json:"matched"`
	Created  []syncFenceLog `json:"created"`
	Reused   []syncFenceLog `json:"reused"`
	Removed  []syncFenceLog `json:"removed"`
	Skipped  []syncSkip     `json:"skipped"`
	Errors   []syncFenceLog `json:"errors"`
	Note     string         `json:"note,omitempty"` // e.g. "removal aborted: 78%>20%"
	DryRun   bool           `json:"dry_run"`
}

type syncFenceLog struct {
	Fence    string            `json:"fence"`
	Category string            `json:"category,omitempty"`
	Channel  string            `json:"channel,omitempty"`
	Threads  map[string]string `json:"threads,omitempty"`
	Error    string            `json:"error,omitempty"`
	Reason   string            `json:"reason,omitempty"`
}

// SyncOneRule runs the bulk syncer for a single rule. Returns a structured
// summary the trigger uses to render its reply / API response.
func (b *Bot) SyncOneRule(rule config.AutocreateRule, opts SyncRuleOptions) SyncRuleResult {
	mu := b.autocreateSync.lockRule(rule.Name)
	if !mu.TryLock() {
		return SyncRuleResult{
			Rule: rule.Name,
			Note: "already syncing",
		}
	}
	defer mu.Unlock()

	result := SyncRuleResult{
		Rule:   rule.Name,
		DryRun: opts.DryRun,
	}

	// 1. Locate the channel template.
	tmpl := b.findChannelTemplate(rule.Template)
	if tmpl == nil {
		result.Note = fmt.Sprintf("template %q not found", rule.Template)
		return result
	}

	// 2. Load cache.
	cachePath := syncCachePath(b.Cfg.BaseDir)
	cache, err := loadAutocreateCache(cachePath)
	if err != nil {
		result.Note = fmt.Sprintf("cache load failed: %v", err)
		return result
	}
	state := cache[rule.Name]
	if state == nil {
		state = &autocreateRuleState{
			GuildID: rule.Guild,
			Fences:  map[string]*autocreateFenceState{},
		}
	}

	// 3. Snapshot live geofences and classify.
	stateMgr := b.StateMgr.Get()
	if stateMgr == nil {
		result.Note = "state manager not ready"
		return result
	}
	fences := stateMgr.Geofence.Fences()
	classify := classifyFences(rule, fences, *state)
	result.Matched = len(classify.toCreate) + len(classify.toReuse)
	result.Skipped = classify.skipped

	// 4. Apply create/reuse. (Removals deferred to PR 6.)
	rep := newCollectingReporter()
	for _, c := range classify.toCreate {
		args, rawArgs := splitParamArgs(c.params)
		applyOpts := applyAutocreateOptions{ResetOnReuse: false, DryRun: opts.DryRun}
		ar := b.applyAutocreate(b.session, tmpl, args, rawArgs, rule.Guild, rep, applyOpts)
		if !opts.DryRun {
			updateStateFromResult(state, c.fence.Name, ar)
		}
		result.Created = append(result.Created, fenceLogFromResult(c.fence.Name, ar))
	}
	for _, c := range classify.toReuse {
		args, rawArgs := splitParamArgs(c.params)
		applyOpts := applyAutocreateOptions{ResetOnReuse: opts.Reset, DryRun: opts.DryRun}
		ar := b.applyAutocreate(b.session, tmpl, args, rawArgs, rule.Guild, rep, applyOpts)
		if !opts.DryRun {
			updateStateFromResult(state, c.fence.Name, ar)
		}
		result.Reused = append(result.Reused, fenceLogFromResult(c.fence.Name, ar))
	}

	// Stash orphans in result.Removed with a "would-remove" reason for now;
	// PR 6 changes this branch to actually remove when allowed.
	for _, name := range classify.orphans {
		result.Removed = append(result.Removed, syncFenceLog{
			Fence:  name,
			Reason: "orphan (removal not yet implemented)",
		})
	}

	// 5. Single debounced reload + save cache.
	if !opts.DryRun {
		state.LastSync = time.Now().UTC()
		state.GuildID = rule.Guild
		cache[rule.Name] = state
		if err := saveAutocreateCache(cachePath, cache); err != nil {
			log.Warnf("autocreate sync: save cache: %v", err)
		}
		// Caller (bot or API) is responsible for triggering reload.
	}

	return result
}

// findChannelTemplate looks up a template by name. Stub for PR 3 — wire
// this to wherever the templates are loaded once you locate it.
func (b *Bot) findChannelTemplate(name string) *channelTemplate {
	templates, err := b.loadChannelTemplates()
	if err != nil {
		log.Warnf("autocreate sync: load channel templates: %v", err)
		return nil
	}
	for i := range templates {
		if templates[i].Name == name {
			return &templates[i]
		}
	}
	return nil
}

// splitParamArgs takes the rendered params strings and produces both
// lower-cased Args (for matching paths) and case-preserving RawArgs (for
// naming) — same shape the bot parser produces. Whitespace inside a
// rendered element splits the element into multiple args; double-quoted
// segments are treated as one even when they contain spaces.
func splitParamArgs(params []string) (args, rawArgs []string) {
	for _, p := range params {
		toks := tokenizeParamString(p)
		for _, t := range toks {
			rawArgs = append(rawArgs, t)
			args = append(args, lowerASCII(t))
		}
	}
	return args, rawArgs
}

// updateStateFromResult mutates state so a subsequent run sees the new
// fence as "in cache".
func updateStateFromResult(state *autocreateRuleState, fenceName string, ar applyAutocreateResult) {
	if state.Fences == nil {
		state.Fences = map[string]*autocreateFenceState{}
	}
	fs, ok := state.Fences[fenceName]
	if !ok {
		fs = &autocreateFenceState{ThreadIDs: map[string]string{}}
		state.Fences[fenceName] = fs
	}
	if ar.CategoryID != "" {
		fs.CategoryID = ar.CategoryID
		// Ensure category present in state.Categories.
		seen := false
		for _, cat := range state.Categories {
			if cat.ID == ar.CategoryID {
				seen = true
				break
			}
		}
		if !seen {
			state.Categories = append(state.Categories, autocreateCategory{ID: ar.CategoryID})
		}
	}
	for _, chID := range ar.ChannelIDs {
		fs.ChannelID = chID
		break // expect one channel per fence in v1
	}
	for _, threads := range ar.ThreadIDs {
		for label, tid := range threads {
			if fs.ThreadIDs == nil {
				fs.ThreadIDs = map[string]string{}
			}
			fs.ThreadIDs[label] = tid
		}
	}
}

func fenceLogFromResult(name string, ar applyAutocreateResult) syncFenceLog {
	out := syncFenceLog{Fence: name, Threads: map[string]string{}}
	if ar.CategoryID != "" {
		out.Category = ar.CategoryID
	}
	for _, ch := range ar.ChannelIDs {
		out.Channel = ch
		break
	}
	for _, threads := range ar.ThreadIDs {
		for label, tid := range threads {
			out.Threads[label] = tid
		}
	}
	if len(ar.Errors) > 0 {
		out.Error = ar.Errors[0].Error()
	}
	return out
}
```

- [ ] **Step 4: Add the supporting helpers**

Create `processor/internal/discordbot/autocreate_helpers.go`:

```go
package discordbot

import (
	"strings"
	"unicode"
)

// tokenizeParamString splits a rendered params element on whitespace,
// preserving double-quoted segments as a single token. Mirrors the bot
// parser's tokenizer so bulk sync produces the same tokenisation a user
// would get from typing the equivalent command.
func tokenizeParamString(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// lowerASCII fast-paths the lower-case for ASCII strings (Discord channel
// names, fence names) without going through unicode normalisation.
func lowerASCII(s string) string {
	return strings.ToLower(s)
}

// collectingReporter accumulates messages instead of sending to Discord.
// Used by the bulk runner so summary output can be rendered after a full
// sync rather than streamed mid-run.
type collectingReporter struct {
	infos  []string
	warns  []string
	errors []string
}

func newCollectingReporter() *collectingReporter { return &collectingReporter{} }

func (r *collectingReporter) Info(msg string)  { r.infos = append(r.infos, msg) }
func (r *collectingReporter) Warn(msg string)  { r.warns = append(r.warns, msg) }
func (r *collectingReporter) Error(msg string) { r.errors = append(r.errors, msg) }
```

- [ ] **Step 5: Add the `loadChannelTemplates` helper if missing**

`handleAutocreate` already does this inline. Extract:

In `processor/internal/discordbot/autocreate.go`, find the existing block that does:

```go
templatePath := filepath.Join(b.Cfg.BaseDir, "config", "channelTemplate.json")
data, err := os.ReadFile(templatePath)
// ... json.Unmarshal into []channelTemplate ...
```

Pull it into a method:

```go
func (b *Bot) loadChannelTemplates() ([]channelTemplate, error) {
	path := filepath.Join(b.Cfg.BaseDir, "config", "channelTemplate.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read channel templates: %w", err)
	}
	var templates []channelTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("parse channel templates: %w", err)
	}
	return templates, nil
}
```

Replace the inline read in `handleAutocreate` with `templates, err := b.loadChannelTemplates()`.

- [ ] **Step 6: Hook the syncer into Bot**

In `processor/internal/discordbot/bot.go`, add a field to the `Bot` struct:

```go
autocreateSync *autocreateSyncer
```

Initialise it in `New(cfg Config)`:

```go
b.autocreateSync = newAutocreateSyncer()
```

(You may also need to expose `b.session` if it isn't already accessible — check the existing struct.)

- [ ] **Step 7: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 8: Run the classifyFences tests**

Run: `cd processor && go test -count=1 -run "TestSyncRule|TestSyncCacheKey" ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add processor/internal/discordbot/autocreate_sync.go processor/internal/discordbot/autocreate_sync_test.go processor/internal/discordbot/autocreate_helpers.go processor/internal/discordbot/autocreate.go processor/internal/discordbot/bot.go
git commit -m "$(cat <<'EOF'
discordbot: bulk autocreate sync runner (create/reuse only)

Per-rule sync runner: classifyFences diffs filter-matched geofences
against the rule's cache, producing toCreate/toReuse/orphans/skipped.
SyncOneRule wires the per-rule mutex, runs applyAutocreate for each
candidate (with ResetOnReuse=false by default — bulk should not churn
hand-tweaked tracking), updates state, and persists the cache.

Removals are stubbed (orphans surface in result.Removed with a
"not yet implemented" note); PR 6 wires the cascade. Reconcile pass
is PR 5. Triggers (bot subcommand, API) come in PR 4.

Helpers: tokenizeParamString respects "double-quoted" segments so
fence names with spaces stay as one arg; collectingReporter buffers
applyAutocreate output for the eventual summary instead of spamming
a Discord channel during a 200-fence run.
EOF
)"
```

---

# PR 4 — Triggers (bot subcommand + API)

Wires `!autocreate sync` and `POST /api/autocreate/run`. Both invoke `SyncOneRule` from PR 3. Adds the keyword i18n strings. Force is parsed but is a no-op until PR 6.

### Task 4.1: i18n keys for the new keywords

**Files:**
- Modify: `processor/internal/i18n/locale/en.json`
- Modify: `processor/internal/i18n/locale/de.json`
- Modify: all other locales (English placeholder)

- [ ] **Step 1: Find an existing arg.* group**

Run: `grep -n '"arg.list"\|"arg.add"' processor/internal/i18n/locale/en.json`
Expected: line numbers in the `arg.*` group near each other.

- [ ] **Step 2: Add the new keys to en.json**

Insert after `arg.overview`:

```json
  "arg.dryrun": "dryrun",
  "arg.reset": "reset",
  "arg.removals": "removals",
  "arg.force": "force",
```

- [ ] **Step 3: Add to de.json**

Same position; values:

```json
  "arg.dryrun": "trockenlauf",
  "arg.reset": "zurücksetzen",
  "arg.removals": "entfernungen",
  "arg.force": "erzwingen",
```

- [ ] **Step 4: Add English placeholder to other locales**

For each of `es, fr, it, ja, nb-no, pl, ru, sv, zh-cn`:

```bash
for f in processor/internal/i18n/locale/{es,fr,it,ja,nb-no,pl,ru,sv,zh-cn}.json; do
  python3 -c "
import sys, re
p = sys.argv[1]
with open(p) as f: text = f.read()
if 'arg.dryrun' in text:
    sys.exit(0)
new = re.sub(
    r'(\"arg\.overview\":\s*\"[^\"]*\",\n)',
    r'\1  \"arg.dryrun\": \"dryrun\",\n  \"arg.reset\": \"reset\",\n  \"arg.removals\": \"removals\",\n  \"arg.force\": \"force\",\n',
    text,
    count=1,
)
with open(p, 'w') as f: f.write(new)
" "$f"
done
```

Verify each locale parses:

```bash
for f in processor/internal/i18n/locale/*.json; do
  python3 -c "import json; json.load(open('$f'))" || echo "INVALID: $f"
done
```

Expected: no INVALID lines.

- [ ] **Step 5: Run i18n tests**

Run: `cd processor && go test -count=1 ./internal/i18n/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/i18n/locale/
git commit -m "i18n: add arg.dryrun / arg.reset / arg.removals / arg.force"
```

### Task 4.2: !autocreate sync subcommand

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

- [ ] **Step 1: Add a sync entrypoint near handleAutocreate**

At the top of `handleAutocreate`, after the admin check and before the existing arg parsing, add a fast-path that detects the `sync` subcommand:

```go
// Sync subcommand: bulk-run [[autocreate.rules]] entries.
if len(args) > 0 && (strings.EqualFold(args[0], "sync") ||
	strings.EqualFold(args[0], b.Translations.For(b.Cfg.General.Locale).T("arg.sync"))) {
	b.handleAutocreateSync(s, m, args[1:])
	return
}
```

(Add `arg.sync` translation if it doesn't exist — for now hard-code the English keyword and the locale's `arg.sync` if defined; if `arg.sync` isn't in the locale files, the second OR branch is a no-op string compare.)

- [ ] **Step 2: Add the subcommand handler**

Append to `autocreate.go`:

```go
// handleAutocreateSync runs bulk syncs over [[autocreate.rules]]. Admin-
// only. Arg shape:
//   <rule-name>?  <flag>* (where flag ∈ {dryrun, reset, removals, force},
//                          translatable, order-independent)
// Empty rule name = run every rule in turn.
func (b *Bot) handleAutocreateSync(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	tr := b.Translations.For(b.Cfg.General.Locale)

	keywords := map[string]bool{}
	for _, k := range []string{"arg.dryrun", "arg.reset", "arg.removals", "arg.force"} {
		keywords[strings.ToLower(tr.T(k))] = true
	}
	keywords["dryrun"] = true
	keywords["reset"] = true
	keywords["removals"] = true
	keywords["force"] = true

	var ruleName string
	opts := SyncRuleOptions{}
	for _, a := range args {
		al := strings.ToLower(a)
		switch {
		case al == "dryrun" || al == strings.ToLower(tr.T("arg.dryrun")):
			opts.DryRun = true
		case al == "reset" || al == strings.ToLower(tr.T("arg.reset")):
			opts.Reset = true
		case al == "removals" || al == strings.ToLower(tr.T("arg.removals")):
			opts.Removals = true
		case al == "force" || al == strings.ToLower(tr.T("arg.force")):
			opts.Force = true
		default:
			if ruleName != "" {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Unknown argument: %s", a))
				return
			}
			ruleName = a
		}
	}

	rules := b.Cfg.Autocreate.Rules
	if ruleName != "" {
		var match *config.AutocreateRule
		for i := range rules {
			if rules[i].Name == ruleName {
				match = &rules[i]
				break
			}
		}
		if match == nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No autocreate rule named %q", ruleName))
			return
		}
		rules = []config.AutocreateRule{*match}
	}

	if len(rules) == 0 {
		s.ChannelMessageSend(m.ChannelID, "No [[autocreate.rules]] configured")
		return
	}

	for _, r := range rules {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Sync %s (dry-run=%v reset=%v removals=%v force=%v)",
			r.Name, opts.DryRun, opts.Reset, opts.Removals, opts.Force))
		result := b.SyncOneRule(r, opts)
		s.ChannelMessageSend(m.ChannelID, formatSyncSummary(result))
	}

	// One debounced reload after all rules done.
	if b.ReloadFunc != nil && !opts.DryRun {
		b.ReloadFunc()
	}
}

// formatSyncSummary produces the per-rule summary the user sees in the
// channel after a sync run.
func formatSyncSummary(r SyncRuleResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Sync %s — %d fences matched\n", r.Rule, r.Matched)
	fmt.Fprintf(&b, "Created: %d\n", len(r.Created))
	fmt.Fprintf(&b, "Reused:  %d\n", len(r.Reused))
	fmt.Fprintf(&b, "Removed: %d\n", len(r.Removed))
	fmt.Fprintf(&b, "Skipped: %d\n", len(r.Skipped))
	fmt.Fprintf(&b, "Errors:  %d\n", len(r.Errors))
	if r.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", r.Note)
	}
	if r.DryRun {
		b.WriteString("(dry run — nothing was changed)\n")
	}
	return b.String()
}
```

Make sure the file's import block includes `"strings"`, `"github.com/pokemon/poracleng/processor/internal/config"`.

- [ ] **Step 3: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/discordbot/autocreate.go
git commit -m "$(cat <<'EOF'
discordbot: !autocreate sync subcommand

Routes the bulk runner from PR 3 from the existing handleAutocreate
entry point. Parses positional translatable keywords (dryrun, reset,
removals, force) order-independently. Admin-only — inherits the
guard from handleAutocreate's preamble. One ReloadFunc trigger after
all rules to coalesce per-fence inserts.
EOF
)"
```

### Task 4.3: API endpoint

**Files:**
- Create: `processor/internal/api/autocreate.go`
- Modify: `processor/cmd/processor/main.go` (route registration)

- [ ] **Step 1: Define the handler**

Create `processor/internal/api/autocreate.go`:

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// AutocreateDeps wires the dependencies the bulk autocreate API needs.
type AutocreateDeps struct {
	Cfg *config.Config
	Bot *discordbot.Bot
}

// AutocreateRunRequest is the POST /api/autocreate/run body.
type AutocreateRunRequest struct {
	Rule     string `json:"rule"`     // empty → all rules
	DryRun   bool   `json:"dry_run"`
	Reset    bool   `json:"reset"`
	Removals bool   `json:"removals"`
	Force    bool   `json:"force"`
}

// HandleAutocreateRun implements POST /api/autocreate/run. Authenticated
// via the same x-poracle-secret middleware as the other /api/* routes.
func HandleAutocreateRun(deps AutocreateDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req AutocreateRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		if deps.Bot == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "discord bot not running"})
			return
		}

		rules := deps.Cfg.Autocreate.Rules
		if req.Rule != "" {
			var matched []config.AutocreateRule
			for _, r := range rules {
				if r.Name == req.Rule {
					matched = append(matched, r)
					break
				}
			}
			if len(matched) == 0 {
				c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "rule not found"})
				return
			}
			rules = matched
		}
		if len(rules) == 0 {
			c.JSON(http.StatusOK, gin.H{"status": "ok", "rules": []discordbot.SyncRuleResult{}})
			return
		}

		opts := discordbot.SyncRuleOptions{
			DryRun:   req.DryRun,
			Reset:    req.Reset,
			Removals: req.Removals,
			Force:    req.Force,
		}

		results := make([]discordbot.SyncRuleResult, 0, len(rules))
		for _, r := range rules {
			results = append(results, deps.Bot.SyncOneRule(r, opts))
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "rules": results})
	}
}
```

- [ ] **Step 2: Register the route**

In `processor/cmd/processor/main.go`, find the existing API route group (search for `apiGroup.POST` to locate the pattern). Add:

```go
apiGroup.POST("/autocreate/run", api.HandleAutocreateRun(api.AutocreateDeps{
	Cfg: cfg,
	Bot: discordBot, // whatever the local variable for the running discord bot is
}))
```

(The exact variable name for the running bot will depend on existing main.go; locate by `grep -n "discordbot\\.New\\b" processor/cmd/processor/main.go` and use the assigned name.)

- [ ] **Step 3: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 4: Smoke test**

Run: `cd processor && go test -count=1 ./internal/api/ ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/autocreate.go processor/cmd/processor/main.go
git commit -m "$(cat <<'EOF'
api: POST /api/autocreate/run

Synchronous endpoint that calls discordbot.SyncOneRule for the
specified rule (or all rules when "rule" is empty). Body shape is
the AutocreateRunRequest struct: dry_run, reset, removals, force.
Auth via the existing x-poracle-secret middleware on /api/*.

Response body is { "status": "ok", "rules": [SyncRuleResult, ...] }
— same struct shape the bot subcommand prints, so external tooling
can consume one schema for both paths.
EOF
)"
```

### Task 4.4: Web editor schema

**Files:**
- Modify: `processor/internal/api/config_schema.go`

- [ ] **Step 1: Locate existing section definitions**

Run: `grep -n "Name: \"discord\"\|Name: \"general\"\|Name: \"tuning\"" processor/internal/api/config_schema.go | head`
Expected: top-level section entries; pick a sensible insertion point (near `discord` for the keep-alive field, after the existing top-level sections for `autocreate`).

- [ ] **Step 2: Add discord.thread_keep_alive_interval_hours**

Inside the `discord` section's `Fields:` slice, add:

```go
{Name: "thread_keep_alive_interval_hours", Type: "int", Default: 24,
 Description: "Hours between automatic unarchive sweeps for managed Discord threads (max 168 = 7 days; 0 = disabled)",
 HotReload: true},
```

- [ ] **Step 3: Add the autocreate top-level section**

After the existing top-level sections, add:

```go
{
	Name:    "autocreate",
	Display: "Autocreate (bulk channel sync)",
	Fields: []FieldDef{
		{Name: "removal_safety_max_percent", Type: "int", Default: 20,
		 Description: "Abort the removal phase when removing more than this % of cached fences (0 = disabled, only applies when cache has ≥10 entries)",
		 HotReload: true},
	},
	Tables: []TableDef{
		{
			Name:        "rules",
			Description: "Bulk-autocreate rules — apply a channel template across many geofences",
			Fields: []FieldDef{
				{Name: "name", Type: "string", Description: "Unique rule identifier"},
				{Name: "guild", Type: "string", Description: "Discord guild ID"},
				{Name: "template", Type: "string", Description: "channelTemplate.json entry name"},
				{Name: "filter", Type: "string", Description: "Optional Handlebars expression; empty = match all"},
				{Name: "params", Type: "string[]", Description: "Each element rendered per fence; positional template args"},
				{Name: "remove_missing", Type: "bool", Default: false,
				 Description: "Allow removal of orphans when the trigger requests it"},
			},
		},
	},
},
```

- [ ] **Step 4: Verify build + tests**

Run: `cd processor && go build ./... && go test -count=1 ./internal/api/`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api/config_schema.go
git commit -m "config_schema: register [autocreate] + thread_keep_alive_interval_hours"
```

---

# PR 5 — Reconcile pass

Adds the cache-pruning step that runs at the start of each sync. Detects out-of-band Discord deletions (channels, categories, threads) so cached IDs that no longer point anywhere don't make the diff loop think a fence still exists when it doesn't.

### Task 5.1: Failing test for reconcile

**Files:**
- Modify: `processor/internal/discordbot/autocreate_sync_test.go`

- [ ] **Step 1: Append the test**

Append to `autocreate_sync_test.go`:

```go
func TestReconcile_DropsMissingChannel(t *testing.T) {
	state := &autocreateRuleState{
		GuildID: "g1",
		Categories: []autocreateCategory{
			{Name: "Belgium", ID: "cat-alive"},
		},
		Fences: map[string]*autocreateFenceState{
			"Gent_centrum": {CategoryID: "cat-alive", ChannelID: "ch-deleted", ThreadIDs: map[string]string{"L": "th-alive"}},
			"Antwerp":      {CategoryID: "cat-alive", ChannelID: "ch-alive",   ThreadIDs: map[string]string{"L": "th-deleted"}},
		},
	}
	live := liveDiscordIDs{
		channels: map[string]bool{"cat-alive": true, "ch-alive": true},
		threads:  map[string]bool{"th-alive": true},
	}

	reconcileCacheAgainstLive(state, live)

	// Channel deleted → fence's channel_id wiped, threads dropped too.
	if state.Fences["Gent_centrum"].ChannelID != "" {
		t.Error("missing channel_id should be cleared")
	}
	if len(state.Fences["Gent_centrum"].ThreadIDs) != 0 {
		t.Error("threads under a deleted channel should be cleared")
	}

	// Channel alive but thread deleted → only the missing thread dropped.
	if state.Fences["Antwerp"].ChannelID != "ch-alive" {
		t.Error("alive channel should remain")
	}
	if _, present := state.Fences["Antwerp"].ThreadIDs["L"]; present {
		t.Error("deleted thread should be dropped")
	}
}

func TestReconcile_DropsMissingCategory(t *testing.T) {
	state := &autocreateRuleState{
		Categories: []autocreateCategory{
			{Name: "DeadCat", ID: "cat-deleted"},
		},
		Fences: map[string]*autocreateFenceState{
			"Foo": {CategoryID: "cat-deleted", ChannelID: "ch-alive"},
		},
	}
	live := liveDiscordIDs{
		channels: map[string]bool{"ch-alive": true},
	}

	reconcileCacheAgainstLive(state, live)

	if len(state.Categories) != 0 {
		t.Error("dead category should be removed from state.Categories")
	}
	// Fence's category_id is wiped (channel can stay if it still exists).
	if state.Fences["Foo"].CategoryID != "" {
		t.Error("fence category_id should be cleared when category is gone")
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd processor && go test -count=1 -run TestReconcile ./internal/discordbot/`
Expected: build error — `liveDiscordIDs`, `reconcileCacheAgainstLive` undefined.

- [ ] **Step 3: Implement**

Append to `autocreate_sync.go`:

```go
// liveDiscordIDs is the set of currently-existing channel/category/thread
// IDs in a guild — used by reconcile to prune dead entries from the
// rule's cache before the diff loop runs.
type liveDiscordIDs struct {
	channels map[string]bool // includes both regular channels and categories
	threads  map[string]bool // active threads only (archived are handled by the keep-alive sweeper)
}

// fetchLiveIDs queries Discord for all current channel/category/thread
// IDs in a guild. Returns empty sets on error so reconcile becomes a
// safe no-op instead of nuking the cache on a transient API hiccup.
func (b *Bot) fetchLiveIDs(guildID string) liveDiscordIDs {
	out := liveDiscordIDs{
		channels: map[string]bool{},
		threads:  map[string]bool{},
	}
	chans, err := b.session.GuildChannels(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildChannels(%s): %v", guildID, err)
		return out
	}
	for _, c := range chans {
		out.channels[c.ID] = true
	}
	threads, err := b.session.GuildThreadsActive(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildThreadsActive(%s): %v", guildID, err)
		return out
	}
	for _, t := range threads.Threads {
		out.threads[t.ID] = true
	}
	return out
}

// reconcileCacheAgainstLive drops cache entries pointing at IDs that no
// longer exist in Discord. Pure cache-pruning — never touches Discord.
func reconcileCacheAgainstLive(state *autocreateRuleState, live liveDiscordIDs) {
	if state == nil {
		return
	}

	// Drop dead categories.
	if len(state.Categories) > 0 {
		kept := state.Categories[:0]
		dead := map[string]bool{}
		for _, cat := range state.Categories {
			if live.channels[cat.ID] {
				kept = append(kept, cat)
			} else {
				dead[cat.ID] = true
			}
		}
		state.Categories = kept
		// Wipe fence.category_id refs to dead categories.
		for _, fs := range state.Fences {
			if dead[fs.CategoryID] {
				fs.CategoryID = ""
			}
		}
	}

	// Per-fence: drop missing channel + thread IDs.
	for _, fs := range state.Fences {
		if fs.ChannelID != "" && !live.channels[fs.ChannelID] {
			fs.ChannelID = ""
			fs.ThreadIDs = nil // a deleted parent channel deletes its threads
			continue
		}
		if len(fs.ThreadIDs) > 0 {
			for label, tid := range fs.ThreadIDs {
				if !live.threads[tid] {
					delete(fs.ThreadIDs, label)
				}
			}
			if len(fs.ThreadIDs) == 0 {
				fs.ThreadIDs = nil
			}
		}
	}
}
```

- [ ] **Step 4: Wire reconcile into SyncOneRule**

Find the comment `// 3. Snapshot live geofences and classify.` in `SyncOneRule`. Insert before it:

```go
// 3a. Reconcile cache against live Discord state — drop entries
// pointing at deleted channels/categories/threads so the diff loop
// treats them as needing creation.
reconcileCacheAgainstLive(state, b.fetchLiveIDs(rule.Guild))
```

(Renumber the original "3." comment to "3b." inline if you want, otherwise leave the surrounding comments alone.)

- [ ] **Step 5: Run tests, confirm pass**

Run: `cd processor && go test -count=1 -run "TestReconcile|TestSyncRule" ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/discordbot/autocreate_sync.go processor/internal/discordbot/autocreate_sync_test.go
git commit -m "$(cat <<'EOF'
discordbot: reconcile autocreate cache against live Discord state

Before each sync, list the guild's channels + active threads once
and prune cache entries whose IDs are gone. A category being deleted
in Discord clears state.Categories AND wipes fence.category_id refs;
a channel deletion clears its fence.channel_id and drops thread
state under it; a thread deletion drops just that label.

Pure pruning — never touches Discord. Subsequent diff loop sees
pruned entries as "missing in cache" and recreates fresh.
EOF
)"
```

---

# PR 6 — Removal cascade + safety + force keyword

Implements the actual delete path for orphan fences plus the safety threshold. Force keyword finally has an effect.

### Task 6.1: Implement the per-fence removal helper

**Files:**
- Modify: `processor/internal/discordbot/autocreate_sync.go`

- [ ] **Step 1: Add the helper near the bottom of the file**

```go
// removeOrphanFence walks down the fence's cached state, deleting threads
// (and their tracking), the bot-control human (if any) plus the channel,
// any Poracle webhooks on the channel (and their webhook humans), and the
// channel itself. Mirrors the reset logic in resetChannelTracking but
// also deletes the Discord entities, not just the DB rows.
func (b *Bot) removeOrphanFence(state *autocreateRuleState, fenceName string, opts SyncRuleOptions) syncFenceLog {
	out := syncFenceLog{Fence: fenceName, Threads: map[string]string{}}
	fs := state.Fences[fenceName]
	if fs == nil {
		out.Reason = "no cache entry"
		return out
	}

	// 1. Threads.
	for label, tid := range fs.ThreadIDs {
		out.Threads[label] = tid
		if opts.DryRun {
			continue
		}
		if err := db.DeleteHumanAndTracking(b.DB, tid); err != nil {
			log.Warnf("autocreate sync: DeleteHumanAndTracking(thread=%s): %v", tid, err)
		}
		if err := b.session.ChannelDelete(tid); err != nil {
			log.Warnf("autocreate sync: ChannelDelete(thread=%s): %v", tid, err)
		}
	}

	// 2. Webhooks named "Poracle" on the channel.
	if fs.ChannelID != "" {
		whs, err := b.session.ChannelWebhooks(fs.ChannelID)
		if err != nil {
			log.Warnf("autocreate sync: ChannelWebhooks(%s): %v", fs.ChannelID, err)
		} else {
			for _, wh := range whs {
				if wh.Name != "Poracle" {
					continue
				}
				url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
				if opts.DryRun {
					continue
				}
				if err := db.DeleteHumanAndTracking(b.DB, url); err != nil {
					log.Warnf("autocreate sync: DeleteHumanAndTracking(webhook=%s): %v", url, err)
				}
				if err := b.session.WebhookDelete(wh.ID); err != nil {
					log.Warnf("autocreate sync: WebhookDelete(%s): %v", wh.ID, err)
				}
			}
		}
	}

	// 3. Bot-control human (channel ID is the human ID).
	if fs.ChannelID != "" {
		out.Channel = fs.ChannelID
		if !opts.DryRun {
			if err := db.DeleteHumanAndTracking(b.DB, fs.ChannelID); err != nil {
				log.Warnf("autocreate sync: DeleteHumanAndTracking(channel=%s): %v", fs.ChannelID, err)
			}
			if err := b.session.ChannelDelete(fs.ChannelID); err != nil {
				log.Warnf("autocreate sync: ChannelDelete(channel=%s): %v", fs.ChannelID, err)
			}
		}
	}

	// 4. Drop the cache entry.
	if !opts.DryRun {
		delete(state.Fences, fenceName)
	}
	return out
}

// removeEmptyManagedCategories deletes category channels in Discord whose
// last child fence was just removed. Walks state.Categories, looks up
// each category's children in the live channel listing (cached for the
// run by the caller), and deletes those with zero children belonging to
// this rule.
func (b *Bot) removeEmptyManagedCategories(state *autocreateRuleState, opts SyncRuleOptions) []syncFenceLog {
	if state == nil || len(state.Categories) == 0 {
		return nil
	}
	stillUsed := map[string]bool{}
	for _, fs := range state.Fences {
		if fs.CategoryID != "" {
			stillUsed[fs.CategoryID] = true
		}
	}
	var removed []syncFenceLog
	kept := state.Categories[:0]
	for _, cat := range state.Categories {
		if stillUsed[cat.ID] {
			kept = append(kept, cat)
			continue
		}
		removed = append(removed, syncFenceLog{Fence: "<category " + cat.Name + ">", Channel: cat.ID})
		if !opts.DryRun {
			if err := b.session.ChannelDelete(cat.ID); err != nil {
				log.Warnf("autocreate sync: ChannelDelete(category=%s): %v", cat.ID, err)
			}
		}
	}
	state.Categories = kept
	return removed
}

// applyRemovalSafety reports whether the orphan removal phase should
// proceed. Returns (allowed, note). When disallowed, the caller adds the
// note to the result and skips removals (creates/reuses still run).
//
// Safety only engages when the cache has ≥10 entries (below that the
// percentage is too noisy). RemovalSafetyMaxPercent = 0 disables.
// `force` opt bypasses entirely. Dry-run never enforces (the whole point
// of dry-run is to preview).
func applyRemovalSafety(cacheCount, orphanCount, maxPct int, opts SyncRuleOptions) (bool, string) {
	if opts.DryRun || opts.Force {
		return true, ""
	}
	if maxPct == 0 || cacheCount < 10 {
		return true, ""
	}
	pct := orphanCount * 100 / cacheCount
	if pct <= maxPct {
		return true, ""
	}
	return false, fmt.Sprintf("removal aborted: %d of %d cached fences would be removed (%d%%, threshold %d%%) — re-run with `force` to override",
		orphanCount, cacheCount, pct, maxPct)
}
```

Add to the imports of `autocreate_sync.go`: `"github.com/pokemon/poracleng/processor/internal/db"`.

- [ ] **Step 2: Tests for safety threshold + cascade ordering**

Append to `autocreate_sync_test.go`:

```go
func TestRemovalSafety_BelowThresholdAllows(t *testing.T) {
	ok, note := applyRemovalSafety(20, 3, 20, SyncRuleOptions{Removals: true})
	if !ok || note != "" {
		t.Errorf("3/20 (15%%) should be allowed under 20%% threshold; got ok=%v note=%q", ok, note)
	}
}

func TestRemovalSafety_AboveThresholdAborts(t *testing.T) {
	ok, note := applyRemovalSafety(20, 10, 20, SyncRuleOptions{Removals: true})
	if ok {
		t.Errorf("10/20 (50%%) should abort under 20%% threshold")
	}
	if note == "" {
		t.Errorf("note should explain the abort")
	}
}

func TestRemovalSafety_BelowFloorBypasses(t *testing.T) {
	ok, _ := applyRemovalSafety(5, 5, 20, SyncRuleOptions{Removals: true})
	if !ok {
		t.Errorf("cache below 10 entries should bypass safety")
	}
}

func TestRemovalSafety_ZeroPercentDisabled(t *testing.T) {
	ok, _ := applyRemovalSafety(100, 100, 0, SyncRuleOptions{Removals: true})
	if !ok {
		t.Errorf("RemovalSafetyMaxPercent=0 should disable safety entirely")
	}
}

func TestRemovalSafety_ForceBypasses(t *testing.T) {
	ok, _ := applyRemovalSafety(20, 19, 20, SyncRuleOptions{Removals: true, Force: true})
	if !ok {
		t.Errorf("force should bypass even an over-threshold case")
	}
}

func TestRemovalSafety_DryRunNeverEnforces(t *testing.T) {
	ok, _ := applyRemovalSafety(20, 19, 20, SyncRuleOptions{Removals: true, DryRun: true})
	if !ok {
		t.Errorf("dry-run should never enforce the threshold")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd processor && go test -count=1 -run "TestRemovalSafety" ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 4: Wire removals into SyncOneRule**

Replace the orphan stub in `SyncOneRule`:

```go
// 4b. Orphan removals (gated on rule.RemoveMissing AND opts.Removals AND
// the safety threshold). Runs after creates/reuses so a sync that
// fails the safety check still adds new fences.
if rule.RemoveMissing && opts.Removals && len(classify.orphans) > 0 {
	allowed, note := applyRemovalSafety(len(state.Fences), len(classify.orphans),
		b.Cfg.Autocreate.RemovalSafetyMaxPercent, opts)
	if !allowed {
		result.Note = note
	} else {
		for _, name := range classify.orphans {
			result.Removed = append(result.Removed, b.removeOrphanFence(state, name, opts))
		}
		result.Removed = append(result.Removed, b.removeEmptyManagedCategories(state, opts)...)
	}
} else if len(classify.orphans) > 0 {
	for _, name := range classify.orphans {
		reason := "remove_missing=false"
		if !opts.Removals {
			reason = "trigger did not request removals"
		}
		result.Removed = append(result.Removed, syncFenceLog{Fence: name, Reason: reason})
	}
}
```

(Replace the previous "stash orphans in result.Removed with a 'would-remove' reason for now" block.)

- [ ] **Step 5: Run all sync tests**

Run: `cd processor && go test -count=1 -run "TestSync|TestReconcile|TestRemovalSafety" ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/discordbot/autocreate_sync.go processor/internal/discordbot/autocreate_sync_test.go
git commit -m "$(cat <<'EOF'
discordbot: orphan removal cascade + safety threshold

When remove_missing=true on the rule AND the trigger requests
removals: walk each orphaned fence's cached state and delete in
order — threads (humans + Discord), Poracle webhooks (humans +
webhooks), then the channel (human + Discord), then drop the cache
entry. After the loop, any cached category that no longer has a
child fence is also deleted.

applyRemovalSafety aborts the removal phase (NOT the whole sync —
creates/reuses still run) when the orphan count exceeds the
configured percent and the cache is large enough for the percentage
to be meaningful. Force opts past the check; dry-run never enforces.
EOF
)"
```

---

# PR 7 — Discord thread keep-alive sweeper

Periodic background goroutine that revives auto-archived private threads belonging to any `humans` row of type `discord:thread`. Independent of autocreate — also keeps `!channel add`'d threads alive.

### Task 7.1: Sweeper skeleton

**Files:**
- Create: `processor/internal/discordbot/threadkeepalive.go`
- Create: `processor/internal/discordbot/threadkeepalive_test.go`

- [ ] **Step 1: Failing test**

Create `processor/internal/discordbot/threadkeepalive_test.go`:

```go
package discordbot

import (
	"testing"
	"time"
)

func TestThreadKeepAliveDuration_RespectsConfig(t *testing.T) {
	if d := keepAliveTickerDuration(0); d != 0 {
		t.Errorf("0 → 0 (disabled), got %v", d)
	}
	if d := keepAliveTickerDuration(24); d != 24*time.Hour {
		t.Errorf("24 → 24h, got %v", d)
	}
	if d := keepAliveTickerDuration(200); d != 168*time.Hour {
		t.Errorf("200 → clamped to 168h, got %v", d)
	}
	if d := keepAliveTickerDuration(-5); d != 0 {
		t.Errorf("-5 → 0, got %v", d)
	}
}
```

Run: `cd processor && go test -count=1 -run TestThreadKeepAliveDuration ./internal/discordbot/`
Expected: build error.

- [ ] **Step 2: Skeleton implementation**

Create `processor/internal/discordbot/threadkeepalive.go`:

```go
package discordbot

import (
	"context"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// threadKeepAlive keeps Poracle-managed Discord threads alive by
// unarchiving them on a schedule. Runs one sweep at startup, then on a
// timer at the configured cadence. The sweeper is independent of
// autocreate — it scans humans rows of type discord:thread, so it
// covers manual !channel add, interactive !autocreate, and bulk sync
// equally.
type threadKeepAlive struct {
	bot      *Bot
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// keepAliveTickerDuration converts the [discord]
// thread_keep_alive_interval_hours config into a Duration, applying
// the upper-bound clamp (168h = 7d) and treating zero/negative as
// "disabled".
func keepAliveTickerDuration(hours int) time.Duration {
	if hours <= 0 {
		return 0
	}
	if hours > 168 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}

// startThreadKeepAlive spawns the background sweeper. Returns a stop
// function that cancels the goroutine and waits for it to exit. Returns
// a no-op stop fn when keep-alive is disabled.
func startThreadKeepAlive(b *Bot, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	k := &threadKeepAlive{bot: b, interval: interval, cancel: cancel}
	k.wg.Add(1)
	go k.run(ctx)
	return func() {
		cancel()
		k.wg.Wait()
	}
}

func (k *threadKeepAlive) run(ctx context.Context) {
	defer k.wg.Done()

	// Run once immediately at startup — a processor that was down for
	// >7 days needs everything revived on first run.
	k.sweep(ctx)

	t := time.NewTicker(k.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.sweep(ctx)
		}
	}
}

func (k *threadKeepAlive) sweep(ctx context.Context) {
	if k.bot == nil || k.bot.Humans == nil || k.bot.session == nil {
		return
	}
	threads, err := k.bot.Humans.ListByType(bot.TypeDiscordThread)
	if err != nil {
		log.Warnf("thread keep-alive: list discord:thread humans: %v", err)
		return
	}
	if len(threads) == 0 {
		return
	}

	// Resolve parent channel for each managed thread (cached for this run).
	parents := map[string]string{} // threadID → parentID
	parentsToWalk := map[string]bool{}
	for _, h := range threads {
		ch, err := k.bot.session.Channel(h.ID)
		if err != nil {
			log.Debugf("thread keep-alive: Channel(%s): %v (likely deleted, skipping)", h.ID, err)
			continue
		}
		parents[h.ID] = ch.ParentID
		parentsToWalk[ch.ParentID] = true
	}

	// For each unique parent, page through its archived private threads
	// and unarchive any whose ID is in our managed set.
	managed := map[string]bool{}
	for _, h := range threads {
		managed[h.ID] = true
	}
	for parentID := range parentsToWalk {
		if ctx.Err() != nil {
			return
		}
		k.revivePrivateArchived(ctx, parentID, managed)
	}
}

// revivePrivateArchived pages through ThreadsPrivateArchived for one
// parent channel, unarchiving any thread whose ID is in `managed`.
func (k *threadKeepAlive) revivePrivateArchived(ctx context.Context, parentID string, managed map[string]bool) {
	var before *time.Time
	const pageLimit = 100
	for {
		if ctx.Err() != nil {
			return
		}
		page, err := k.bot.session.ThreadsPrivateArchived(parentID, before, pageLimit)
		if err != nil {
			log.Warnf("thread keep-alive: ThreadsPrivateArchived(%s): %v", parentID, err)
			return
		}
		for _, thread := range page.Threads {
			if !managed[thread.ID] {
				continue
			}
			f := false
			if _, err := k.bot.session.ChannelEdit(thread.ID, &discordgo.ChannelEdit{Archived: &f}); err != nil {
				log.Warnf("thread keep-alive: unarchive %s: %v", thread.ID, err)
				continue
			}
			log.Infof("thread keep-alive: unarchived %s under %s", thread.ID, parentID)
		}
		if !page.HasMore || len(page.Threads) == 0 {
			return
		}
		// Page using the oldest thread's archive timestamp.
		oldest := page.Threads[len(page.Threads)-1]
		if oldest.ThreadMetadata == nil {
			return
		}
		t := oldest.ThreadMetadata.ArchiveTimestamp
		before = &t
	}
}
```

- [ ] **Step 3: Run test**

Run: `cd processor && go test -count=1 -run TestThreadKeepAliveDuration ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 4: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 5: Confirm bot.TypeDiscordThread exists**

Run: `grep -n "TypeDiscordThread" processor/internal/bot/command.go`
Expected: a constant definition. If not, this branch needs to be on `autocreate-threads` (which has the constant) — flag and ask the user.

### Task 7.2: Wire into bot startup

**Files:**
- Modify: `processor/internal/discordbot/bot.go`

- [ ] **Step 1: Start the sweeper from New**

In `New(cfg Config)`, after the bot is fully constructed and the session is open, add:

```go
b.stopKeepAlive = startThreadKeepAlive(b, keepAliveTickerDuration(cfg.Cfg.Discord.ThreadKeepAliveIntervalHours))
```

Add the field on the `Bot` struct:

```go
stopKeepAlive func()
```

- [ ] **Step 2: Stop on shutdown**

Find the bot's Close/Stop method (search `func (b *Bot) Close\|func (b *Bot) Stop`). Add:

```go
if b.stopKeepAlive != nil {
	b.stopKeepAlive()
}
```

(Before `session.Close()` so the goroutine isn't trying to use a torn-down session.)

- [ ] **Step 3: Verify build**

Run: `cd processor && go build ./...`
Expected: success.

- [ ] **Step 4: Run tests**

Run: `cd processor && go test -count=1 ./internal/discordbot/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/threadkeepalive.go processor/internal/discordbot/threadkeepalive_test.go processor/internal/discordbot/bot.go
git commit -m "$(cat <<'EOF'
discordbot: thread keep-alive sweeper

Periodic background goroutine that unarchives Poracle-managed Discord
threads. Source of truth: humans rows where type='discord:thread' —
covers manual !channel add, interactive !autocreate, and bulk sync
equally.

Runs one sweep immediately at startup (a processor offline for >7
days needs everything revived) plus on a time.Ticker at the
configured [discord] thread_keep_alive_interval_hours cadence.
Clamped to 168h (Discord's archive ceiling); 0 disables.

Per sweep: ListByType(discord:thread), resolve each thread's
parent channel, ThreadsPrivateArchived per parent (paginated), and
ChannelEdit Archived=false on each managed thread we find archived.
Errors logged + skipped, never fatal.

Cancelled cleanly on bot shutdown via context.
EOF
)"
```

---

## Final integration step

### Task F.1: End-to-end smoke test

**Files:**
- (none — verification step)

- [ ] **Step 1: Full build + test**

Run: `cd processor && go build ./... && go test -count=1 ./...`
Expected: all tests pass (modulo the known flaky `TestLiveUiconsIndex` which depends on a live external URL).

- [ ] **Step 2: Confirm no `go vet` warnings**

Run: `cd processor && go vet ./...`
Expected: silence.

- [ ] **Step 3: Confirm gofmt clean**

Run: `cd processor && gofmt -l .`
Expected: silence (no files need reformatting).

- [ ] **Step 4: If everything green, the feature is done**

Push branches as needed.
