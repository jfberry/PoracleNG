# Per-User Distance & Bearing for All Alert Types — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `{{distance}}`, `{{bearing}}` and `{{bearingEmoji}}` resolve to each user's own value in every alert type, not just pokemon.

**Architecture:** The matcher already computes `Distance`/`Bearing`/`CardinalDirection` on every `webhook.MatchedUser` (`matching/generic.go:120-135`); only the pokemon-only PVP enrichment path surfaces them as template fields, and `renderGrouped` is predicated on their absence. We add a cached template-source check (`UsesPerUserFields`, mirroring the existing `UsesTile`), extract the existing per-user render loop into a reusable `renderPerUser`, and in the non-pokemon path split users per `(template, platform, language)` group: groups whose template references the positional fields render per-user with a positional enrichment map; all others keep the group-render fast path unchanged.

**Tech Stack:** Go, `jfberry/raymond` Handlebars, standard `regexp`/`testing`.

## Global Constraints

- Pre-commit gate (run from `processor/` before every commit): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
- No new dependencies.
- The `dts` package must not import `enrichment` (would complete an `enrichment → dts` import cycle — see `internal/dts/quest_summary.go:136`). The positional map is therefore built inside `dts` directly from `webhook.MatchedUser`, which `dts` already imports.
- Per-user field key names must exactly mirror `enrichment.PokemonPerUser` (`internal/enrichment/peruser.go:58-64`): `distance`, `bearing`, `bearingEmojiKey`, `userDistanceTrack`, `userTrackDistance`. The LayeredView resolves `{{bearingEmoji}}` from `bearingEmojiKey`; a different key silently breaks emoji resolution.
- Zero behaviour change for templates that do not reference the positional fields — including every template that uses only `userDistanceTrack` / `userTrackDistance`. This is the explicit regression to guard against.

---

### Task 1: `UsesPerUserFields` template-source check

**Files:**
- Modify: `processor/internal/dts/templates.go` (add cache field, predicate, method; extend 5 reset sites)
- Test: `processor/internal/dts/templates_test.go`

**Interfaces:**
- Produces: `func sourceUsesPerUserFields(source string) bool` and `func (ts *TemplateStore) UsesPerUserFields(templateType, platform, templateID, language string) bool` — Task 3 consumes the method.

- [ ] **Step 1: Write the failing test**

Add to `processor/internal/dts/templates_test.go`:

```go
func TestSourceUsesPerUserFields(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"distance", `{"content":"{{distance}}m"}`, true},
		{"triple-stache bearing", `{"content":"{{{bearing}}}"}`, true},
		{"bearingEmoji", `{"content":"{{bearingEmoji}}"}`, true},
		{"distance subexpression", `{"content":"{{fmtDist (distance)}}"}`, true},
		{"userDistanceTrack flag only", `{"content":"{{#if userDistanceTrack}}near{{/if}}"}`, false},
		{"userTrackDistance only", `{"content":"{{userTrackDistance}}"}`, false},
		{"unrelated fields", `{"content":"{{name}} {{iv}}%"}`, false},
		{"prose distance not in braces", `{"content":"distance to travel"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sourceUsesPerUserFields(c.src); got != c.want {
				t.Errorf("sourceUsesPerUserFields(%q) = %v, want %v", c.src, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/dts/ -run TestSourceUsesPerUserFields -v`
Expected: FAIL — `undefined: sourceUsesPerUserFields`.

- [ ] **Step 3: Add the predicate**

At the top of `processor/internal/dts/templates.go`, add `"regexp"` to the import block, then add near `cacheKey` (around line 519):

```go
// perUserFieldRe matches a Handlebars expression that references a
// per-user positional field. It anchors at an opening stache (`{{` or
// `{{{`) and allows any helper name / arguments up to the next brace
// (`[^{}]*`), so both `{{distance}}` and `{{fmtDist (distance)}}` match.
// The `\b...\b` word boundaries keep `userDistanceTrack` and
// `userTrackDistance` — which are group-safe flags, not positional
// values — from matching: in `userdistancetrack` the 'distance' run is
// flanked by word characters, so no boundary exists before it.
var perUserFieldRe = regexp.MustCompile(`\{\{\{?[^{}]*\b(distance|bearing|bearingemoji)\b`)

// sourceUsesPerUserFields reports whether a rendered-template source
// references {{distance}}, {{bearing}} or {{bearingEmoji}} (in any stache
// or subexpression form). Applied to lowercased source so casing in the
// template doesn't matter.
func sourceUsesPerUserFields(source string) bool {
	return perUserFieldRe.MatchString(strings.ToLower(source))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/dts/ -run TestSourceUsesPerUserFields -v`
Expected: PASS (all 8 sub-tests).

- [ ] **Step 5: Add the cached method and cache field**

In `processor/internal/dts/templates.go`:

Add the field to the `TemplateStore` struct (after `tileUsage map[string]bool` at line 99):

```go
	perUserUsage map[string]bool
```

Initialise it in `LoadTemplates` (in the `&TemplateStore{...}` literal at line 114, alongside `tileUsage: make(map[string]bool),`):

```go
		perUserUsage: make(map[string]bool),
```

Add the same `ts.perUserUsage = make(map[string]bool)` line immediately after each existing `ts.tileUsage = make(map[string]bool)` reset, at all four sites: `Reload` (~line 348), `ClearCache` (~line 480), `SaveEntry` (~line 1201), and `DeleteEntry` (~line 1281).

Add the method next to `UsesTile` (after line 517):

```go
// UsesPerUserFields reports whether the template selected for these
// parameters references a per-user positional field ({{distance}},
// {{bearing}}, {{bearingEmoji}}). Result is cached per selection key,
// mirroring UsesTile. Unlike UsesTile, an unresolvable template returns
// false: falling through to the group-render fast path is the safe,
// zero-regression default when there is no source to inspect.
func (ts *TemplateStore) UsesPerUserFields(templateType, platform, templateID, language string) bool {
	key := cacheKey(templateType, platform, templateID, language)

	ts.mu.RLock()
	if result, ok := ts.perUserUsage[key]; ok {
		ts.mu.RUnlock()
		return result
	}
	ts.mu.RUnlock()

	// Trigger template resolution (which populates sourceCache).
	tmpl := ts.Get(templateType, platform, templateID, language)
	if tmpl == nil {
		return false
	}

	ts.mu.RLock()
	source, ok := ts.sourceCache[key]
	ts.mu.RUnlock()
	if !ok {
		return false
	}

	uses := sourceUsesPerUserFields(source)

	ts.mu.Lock()
	ts.perUserUsage[key] = uses
	ts.mu.Unlock()

	return uses
}
```

- [ ] **Step 6: Add a method-level test**

Add to `processor/internal/dts/templates_test.go`:

```go
func TestUsesPerUserFields(t *testing.T) {
	configDir := t.TempDir()
	fallbackDir := t.TempDir()
	writeTestDTS(t, configDir, []DTSEntry{
		{Type: "raid", ID: "dist", Platform: "discord", Template: map[string]any{"content": "{{gymName}} {{distance}}m"}},
		{Type: "raid", ID: "plain", Platform: "discord", Template: map[string]any{"content": "{{gymName}} raid"}},
	})
	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.UsesPerUserFields("raid", "discord", "dist", "en") {
		t.Error("expected UsesPerUserFields true for template with {{distance}}")
	}
	if ts.UsesPerUserFields("raid", "discord", "plain", "en") {
		t.Error("expected UsesPerUserFields false for template without positional fields")
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd processor && go test ./internal/dts/ -run 'TestSourceUsesPerUserFields|TestUsesPerUserFields' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/dts/
git add internal/dts/templates.go internal/dts/templates_test.go
git commit -m "feat(dts): add UsesPerUserFields template-source check"
```

---

### Task 2: Extract `renderPerUser` from `renderForUsers`

Pure refactor — no behaviour change. Moves the existing per-user render loop into its own method so Task 3 can call it for a subset of users.

**Files:**
- Modify: `processor/internal/dts/renderer.go` (`renderForUsers` body split into `renderForUsers` + new `renderPerUser`)

**Interfaces:**
- Produces: `func (r *Renderer) renderPerUser(templateType string, enrichment map[string]any, perLangEnrichment, perUserEnrichment map[string]map[string]any, webhookFields map[string]any, original map[string]any, users []webhook.MatchedUser, areas []webhook.MatchedArea, logReference, editKeyBase string, tthMap map[string]any, lat, lon string, shlinkCache map[string]string) []webhook.DeliveryJob` — Task 3 consumes it.

- [ ] **Step 1: Replace `renderForUsers` body below the setup lines**

In `processor/internal/dts/renderer.go`, `renderForUsers` currently computes `tthMap`, `lat`, `lon`, `shlinkCache`, then has `if perUserEnrichment == nil { return r.renderGrouped(...) }` followed by the inline `var jobs; for _, user := range users { ... }; return jobs`.

Replace everything from `if perUserEnrichment == nil {` to the closing `return jobs` / `}` of `renderForUsers` with:

```go
	if perUserEnrichment == nil {
		return r.renderGrouped(templateType, enrichment, perLangEnrichment, webhookFields, original, users, areas, logReference, tthMap, lat, lon, shlinkCache, editKeyBase)
	}

	return r.renderPerUser(templateType, enrichment, perLangEnrichment, perUserEnrichment, webhookFields, original, users, areas, logReference, editKeyBase, tthMap, lat, lon, shlinkCache)
}

// renderPerUser renders one DeliveryJob per user, building a fresh
// LayeredView per user so per-user enrichment (PVP display, distance,
// bearing) resolves to that user's own values. This is the non-grouped
// path: it is used for pokemon (which always has per-user PVP data) and,
// via Task 3, for any grouped type whose template references per-user
// positional fields.
func (r *Renderer) renderPerUser(
	templateType string,
	enrichment map[string]any,
	perLangEnrichment map[string]map[string]any,
	perUserEnrichment map[string]map[string]any,
	webhookFields map[string]any,
	original map[string]any,
	users []webhook.MatchedUser,
	areas []webhook.MatchedArea,
	logReference string,
	editKeyBase string,
	tthMap map[string]any,
	lat, lon string,
	shlinkCache map[string]string,
) []webhook.DeliveryJob {
	var jobs []webhook.DeliveryJob

	for _, user := range users {
```

Leave the entire existing loop body (from `// a. Determine platform` through the `jobs = append(...)` block and the closing `}` of the `for`) and the final `return jobs` exactly as they are — they now belong to `renderPerUser`. The net change is: the `if perUserEnrichment == nil` early-return now also has an explicit `renderPerUser` call for the non-nil case, and the loop is under a new function header.

- [ ] **Step 2: Verify it builds**

Run: `cd processor && go build ./internal/dts/`
Expected: builds clean, no unused-variable errors (`tthMap`/`lat`/`lon`/`shlinkCache` are now passed through).

- [ ] **Step 3: Run the existing renderer tests to prove no behaviour change**

Run: `cd processor && go test ./internal/dts/ -count=1`
Expected: PASS — the pokemon path (`RenderPokemon`, `RenderPokemonChanged`) exercises `renderPerUser`; grouped types exercise the unchanged `renderGrouped`.

- [ ] **Step 4: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/dts/
git add internal/dts/renderer.go
git commit -m "refactor(dts): extract renderPerUser from renderForUsers"
```

---

### Task 3: Split non-pokemon users by per-user field usage

**Files:**
- Modify: `processor/internal/dts/renderer.go` (add `positionalPerUser`; rewrite the `perUserEnrichment == nil` branch of `renderForUsers`)
- Test: `processor/internal/dts/renderer_test.go`

**Interfaces:**
- Consumes: `renderPerUser` (Task 2), `TemplateStore.UsesPerUserFields` (Task 1).
- Produces: `func positionalPerUser(users []webhook.MatchedUser) map[string]map[string]any`.

- [ ] **Step 1: Write the failing tests**

Add to `processor/internal/dts/renderer_test.go`:

```go
func TestRenderAlertPerUserDistance(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "1", Platform: "discord", Default: true,
			Template: map[string]any{"content": "{{gymName}} {{distance}}m"}},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"gymName":   "Gym A",
		"latitude":  1.0,
		"longitude": 2.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Language: "en", Distance: 500, Bearing: 90, CardinalDirection: "east", TrackDistance: 1000},
		{ID: "u2", Type: "discord:user", Template: "1", Language: "en", Distance: 1200, Bearing: 270, CardinalDirection: "west", TrackDistance: 2000},
	}

	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "ref", "")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	got := map[string]string{}
	for _, j := range jobs {
		msg := parseMessage(t, j.Message)
		got[j.Target], _ = msg["content"].(string)
	}
	if got["u1"] != "Gym A 500m" {
		t.Errorf("u1 content = %q, want %q", got["u1"], "Gym A 500m")
	}
	if got["u2"] != "Gym A 1200m" {
		t.Errorf("u2 content = %q, want %q", got["u2"], "Gym A 1200m")
	}
}

func TestRenderAlertGroupedWithoutDistance(t *testing.T) {
	entries := []DTSEntry{
		{Type: "raid", ID: "1", Platform: "discord", Default: true,
			Template: map[string]any{"content": "{{gymName}} raid"}},
	}
	r := newTestRenderer(t, entries)

	enrichment := map[string]any{
		"gymName":   "Gym A",
		"latitude":  1.0,
		"longitude": 2.0,
		"tth":       map[string]any{"totalSeconds": 600},
	}
	users := []webhook.MatchedUser{
		{ID: "u1", Type: "discord:user", Template: "1", Language: "en", Distance: 500},
		{ID: "u2", Type: "discord:user", Template: "1", Language: "en", Distance: 1200},
	}

	jobs := r.RenderAlert("raid", enrichment, nil, nil, users, nil, "ref", "")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	for _, j := range jobs {
		msg := parseMessage(t, j.Message)
		if c, _ := msg["content"].(string); c != "Gym A raid" {
			t.Errorf("target %s content = %q, want %q", j.Target, c, "Gym A raid")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify the distance test fails**

Run: `cd processor && go test ./internal/dts/ -run 'TestRenderAlertPerUserDistance|TestRenderAlertGroupedWithoutDistance' -v`
Expected: `TestRenderAlertGroupedWithoutDistance` PASSES already (grouped path unchanged); `TestRenderAlertPerUserDistance` FAILS — both users render `Gym A m` (empty `{{distance}}`) because the grouped path provides no per-user distance.

- [ ] **Step 3: Add `positionalPerUser` and rewrite the branch**

In `processor/internal/dts/renderer.go`, add above `renderForUsers`:

```go
// positionalPerUser builds a per-user enrichment map carrying only the
// location-relative fields the matcher computes for every alert type.
// Unlike enrichment.PokemonPerUser it has no PVP dependency, so it applies
// to raids, quests, invasions and every other grouped type. Key names
// mirror PokemonPerUser exactly (internal/enrichment/peruser.go:58-64) so
// the LayeredView resolves {{distance}}, {{bearing}} and {{bearingEmoji}}
// identically on both paths.
func positionalPerUser(users []webhook.MatchedUser) map[string]map[string]any {
	m := make(map[string]map[string]any, len(users))
	for _, u := range users {
		m[u.ID] = map[string]any{
			"distance":          u.Distance,
			"bearing":           u.Bearing,
			"bearingEmojiKey":   u.CardinalDirection,
			"userDistanceTrack": u.TrackDistance > 0,
			"userTrackDistance": u.TrackDistance,
		}
	}
	return m
}
```

Replace the `if perUserEnrichment == nil { return r.renderGrouped(...) }` block (from Task 2 Step 1) with:

```go
	if perUserEnrichment == nil {
		// Split users by whether their resolved (template, platform,
		// language) references per-user positional fields. Templates that
		// don't (the common case) keep the group-render fast path;
		// templates that do render per-user so {{distance}}/{{bearing}}
		// resolve to each user's own value.
		var groupedUsers, perUserUsers []webhook.MatchedUser
		for _, user := range users {
			platform := delivery.PlatformFromType(user.Type)
			language := user.Language
			if language == "" {
				language = r.locale
			}
			templateID := r.resolveTemplate(user.Template)
			if r.templates.UsesPerUserFields(templateType, platform, templateID, language) {
				perUserUsers = append(perUserUsers, user)
			} else {
				groupedUsers = append(groupedUsers, user)
			}
		}

		var jobs []webhook.DeliveryJob
		if len(groupedUsers) > 0 {
			jobs = append(jobs, r.renderGrouped(templateType, enrichment, perLangEnrichment, webhookFields, original, groupedUsers, areas, logReference, tthMap, lat, lon, shlinkCache, editKeyBase)...)
		}
		if len(perUserUsers) > 0 {
			jobs = append(jobs, r.renderPerUser(templateType, enrichment, perLangEnrichment, positionalPerUser(perUserUsers), webhookFields, original, perUserUsers, areas, logReference, editKeyBase, tthMap, lat, lon, shlinkCache)...)
		}
		return jobs
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd processor && go test ./internal/dts/ -run 'TestRenderAlertPerUserDistance|TestRenderAlertGroupedWithoutDistance' -v`
Expected: both PASS.

- [ ] **Step 5: Run the full dts suite for regressions**

Run: `cd processor && go test ./internal/dts/ -count=1`
Expected: PASS — grouped and pokemon paths unchanged.

- [ ] **Step 6: Commit**

```bash
cd processor && go build ./... && go vet ./... && golangci-lint run ./internal/dts/
git add internal/dts/renderer.go internal/dts/renderer_test.go
git commit -m "feat(dts): resolve per-user distance/bearing for all alert types"
```

---

### Task 4: Correct the group-render documentation

**Files:**
- Modify: `CLAUDE.md` (Template Rendering section)

- [ ] **Step 1: Fix the false claim**

In `CLAUDE.md`, find the Webhook-Flow rendering bullet:

```
4. **Group rendering optimization**: for non-pokemon types, users with the same (template, platform, language) share a single render — only per-user fields (distance, bearing) are patched afterward
```

Replace with:

```
4. **Group rendering optimization**: for non-pokemon types, users with the same (template, platform, language) share a single render. Templates that reference per-user positional fields ({{distance}}, {{bearing}}, {{bearingEmoji}}) opt that group out of sharing and render per-user instead — detected via `TemplateStore.UsesPerUserFields`, mirroring the `UsesTile` mechanism.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: correct group-render per-user field description"
```

---

## Full-suite verification

After Task 4, run the complete pre-commit gate from `processor/`:

```bash
cd processor && go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```

Expected: all pass. This proves no matcher, enrichment, or delivery test regressed.

## Self-Review notes

- **Spec coverage:** `UsesPerUserFields` (spec §1) → Task 1; positional per-user map (spec §2) → Task 3 `positionalPerUser`; group-only-when-safe (spec §3) → Task 3 split; CLAUDE.md correction (spec Follow-up) → Task 4. Testing bullets (token-form matching, per-user vs grouped, area-based `distance = 0`) → Task 1 Step 1 cases + Task 3 tests. `bearingEmojiKey` resolution is covered structurally by the exact-key-name constraint.
- **Out of scope (per spec):** questSummary distance (its template never references the fields, so it stays grouped automatically); matcher changes; anchor-resolution changes.
- **Area-based `distance = 0`:** guaranteed by `positionalPerUser` copying `u.Distance` verbatim — an area rule leaves it 0.
