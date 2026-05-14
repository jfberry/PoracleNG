# !autocreate Threads — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `!autocreate` to create private threads under a master channel and post a button-driven picker so users can self-invite to the threads they want. Each thread is registered as its own `discord:thread` Poracle human and runs its own configured tracking commands at creation time.

**Architecture:** Threads and their picker post are described declaratively under each parent channel in `config/channelTemplate.json`. `!autocreate` creates the threads up front, registers each as a `discord:thread` human, runs its `commands` block via the existing parsed-execute loop, and posts (or edits) a picker embed in the master channel with one button per thread. Button `custom_id`s are stateless (`poracle:thread:<masterID>:<threadID>:join`) so the click handler can act after a bot restart without warm state. A small JSON cache file (`config/.cache/autocreate-threads.json`) records the master→threads mapping so re-runs of `!autocreate` and an opt-in role-loss reconciliation can find threads to update / kick from.

**Tech Stack:** Go 1.22, `bwmarrin/discordgo` v0.28 (gateway + REST), existing `bot.Parser` / `bot.Registry` command framework, existing reconciliation in `processor/internal/discordbot/reconciliation.go`, `store.HumanStore` for human creation. No new third-party dependencies.

---

## File structure

| File | Responsibility | New / modify |
|------|----------------|--------------|
| `processor/internal/discordbot/autocreate_threads.go` | ThreadCache type (load/save/find), custom-id codec, picker embed builder | **new** |
| `processor/internal/discordbot/autocreate_threads_test.go` | Unit tests for codec + cache I/O | **new** |
| `processor/internal/discordbot/autocreate.go` | Extend `channelEntry` with `Threads` and `ThreadPicker`; after channel creation, create threads and emit picker | modify |
| `processor/internal/discordbot/interaction.go` | `onInteractionCreate` handler: parse custom-id, verify access, `ThreadMemberAdd` | **new** |
| `processor/internal/discordbot/bot.go` | Register the interaction handler with discordgo | modify (~line 64) |
| `CLAUDE.md` | One-paragraph note describing the new picker flow under "Discord Reconciliation" | modify |

The custom-id codec, the cache file, and the picker embed all live together in `autocreate_threads.go` because they only make sense as a set: the cache stores what the picker renders, and the codec is the language the picker and the click handler share.

---

## Conventions used throughout this plan

- Imports in code blocks are abbreviated for readability — copy the existing imports plus any newly needed ones (logged in each task's "imports needed" line).
- Test commands assume CWD = `processor/`. Run `cd processor` once at the start.
- Every task ends with a commit step. The commit messages follow the existing repo style (subject in imperative, no body required for small commits).
- `MasterID` = the parent text channel's Discord ID. `ThreadID` = the private-thread Discord channel ID.

---

## Task 1: Custom-id codec

**Files:**
- Create: `processor/internal/discordbot/autocreate_threads.go`
- Test: `processor/internal/discordbot/autocreate_threads_test.go`

The button's `custom_id` is the only state that survives a bot restart, so encoding the `(masterID, threadID)` pair into it lets the click handler act with no in-memory lookup.

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/autocreate_threads_test.go
package discordbot

import "testing"

func TestEncodeThreadJoinID(t *testing.T) {
	got := encodeThreadJoinID("12345", "67890")
	want := "poracle:thread:12345:67890:join"
	if got != want {
		t.Errorf("encodeThreadJoinID = %q, want %q", got, want)
	}
}

func TestDecodeThreadJoinID(t *testing.T) {
	tests := []struct {
		in            string
		wantMaster    string
		wantThread    string
		wantOK        bool
	}{
		{"poracle:thread:111:222:join", "111", "222", true},
		{"poracle:thread:111:222", "", "", false},
		{"poracle:thread::222:join", "", "", false},
		{"random:button", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		master, thread, ok := decodeThreadJoinID(tc.in)
		if ok != tc.wantOK || master != tc.wantMaster || thread != tc.wantThread {
			t.Errorf("decodeThreadJoinID(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, master, thread, ok, tc.wantMaster, tc.wantThread, tc.wantOK)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd processor
go test ./internal/discordbot/ -run TestEncodeThreadJoinID -v
```
Expected: FAIL with "undefined: encodeThreadJoinID".

- [ ] **Step 3: Write the codec**

```go
// processor/internal/discordbot/autocreate_threads.go
package discordbot

import (
	"strings"
)

// Discord limits custom_id to 100 chars; this prefix scheme leaves
// ~80 chars of headroom which is more than enough for two snowflake IDs.
const threadJoinIDPrefix = "poracle:thread:"
const threadJoinIDSuffix = ":join"

// encodeThreadJoinID builds the button custom_id for a "join thread" button.
// The encoded form is stateless: the click handler can act on it directly
// after a bot restart with no warm state.
func encodeThreadJoinID(masterChannelID, threadID string) string {
	return threadJoinIDPrefix + masterChannelID + ":" + threadID + threadJoinIDSuffix
}

// decodeThreadJoinID reverses encodeThreadJoinID. Returns ok=false for any
// input that doesn't match the expected shape — callers must reject those
// rather than treating empty IDs as "all threads".
func decodeThreadJoinID(id string) (masterID, threadID string, ok bool) {
	if !strings.HasPrefix(id, threadJoinIDPrefix) || !strings.HasSuffix(id, threadJoinIDSuffix) {
		return "", "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(id, threadJoinIDPrefix), threadJoinIDSuffix)
	parts := strings.Split(body, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/discordbot/ -run TestEncodeThreadJoinID -v
go test ./internal/discordbot/ -run TestDecodeThreadJoinID -v
```
Expected: PASS for both.

- [ ] **Step 5: Commit**

```
git add processor/internal/discordbot/autocreate_threads.go processor/internal/discordbot/autocreate_threads_test.go
git commit -m "discordbot: add custom_id codec for thread-join buttons"
```

---

## Task 2: Thread cache file

**Files:**
- Modify: `processor/internal/discordbot/autocreate_threads.go`
- Modify: `processor/internal/discordbot/autocreate_threads_test.go`

The cache records, per master channel, the list of (threadID, label) entries plus the picker message ID. It's used by:
- `!autocreate` re-runs (skip thread creation if already cached, edit existing picker post)
- The reconciliation hook (find threads to kick from)

- [ ] **Step 1: Write the failing tests**

```go
// Append to processor/internal/discordbot/autocreate_threads_test.go
import (
	"path/filepath"
)

func TestThreadCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autocreate-threads.json")

	c := &threadCache{path: path}
	if err := c.load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}

	c.upsertMaster("guild1", "master1", "999")
	c.upsertThread("master1", threadCacheEntry{ThreadID: "t1", Label: "Hundo"})
	c.upsertThread("master1", threadCacheEntry{ThreadID: "t2", Label: "Nundo"})

	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2 := &threadCache{path: path}
	if err := c2.load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	m, ok := c2.master("master1")
	if !ok {
		t.Fatal("master not found after reload")
	}
	if m.GuildID != "guild1" || m.PickerMessageID != "999" {
		t.Errorf("master fields = %+v", m)
	}
	if len(m.Threads) != 2 || m.Threads[0].ThreadID != "t1" || m.Threads[1].Label != "Nundo" {
		t.Errorf("threads = %+v", m.Threads)
	}
}

func TestThreadCacheMastersForUser(t *testing.T) {
	c := &threadCache{}
	c.upsertMaster("g1", "m1", "")
	c.upsertMaster("g1", "m2", "")
	c.upsertThread("m1", threadCacheEntry{ThreadID: "t1"})
	c.upsertThread("m2", threadCacheEntry{ThreadID: "t2"})

	all := c.allMasters()
	if len(all) != 2 {
		t.Errorf("allMasters len = %d, want 2", len(all))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/discordbot/ -run TestThreadCache -v
```
Expected: FAIL with "undefined: threadCache".

- [ ] **Step 3: Implement the cache**

Append to `processor/internal/discordbot/autocreate_threads.go`:

```go
import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// threadCacheEntry is one button on one master channel's picker.
type threadCacheEntry struct {
	ThreadID string `json:"threadId"`
	Label    string `json:"label"`
}

// threadCacheMaster is the per-master section of the on-disk cache.
type threadCacheMaster struct {
	GuildID         string             `json:"guildId"`
	PickerMessageID string             `json:"pickerMessageId,omitempty"`
	Threads         []threadCacheEntry `json:"threads"`
}

// threadCache is a JSON-backed map of master channel ID -> threadCacheMaster.
// Concurrent access is guarded by mu; callers should call load() once at
// startup and save() after each mutation.
type threadCache struct {
	mu      sync.Mutex
	path    string
	masters map[string]*threadCacheMaster
}

func newThreadCache(path string) *threadCache {
	return &threadCache{path: path, masters: map[string]*threadCacheMaster{}}
}

func (c *threadCache) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.masters == nil {
		c.masters = map[string]*threadCacheMaster{}
	}
	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read thread cache %s: %w", c.path, err)
	}
	if err := json.Unmarshal(data, &c.masters); err != nil {
		return fmt.Errorf("parse thread cache %s: %w", c.path, err)
	}
	return nil
}

func (c *threadCache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.MarshalIndent(c.masters, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread cache: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		return fmt.Errorf("write thread cache %s: %w", c.path, err)
	}
	return nil
}

func (c *threadCache) upsertMaster(guildID, masterID, pickerMessageID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.masters == nil {
		c.masters = map[string]*threadCacheMaster{}
	}
	m := c.masters[masterID]
	if m == nil {
		m = &threadCacheMaster{}
		c.masters[masterID] = m
	}
	m.GuildID = guildID
	if pickerMessageID != "" {
		m.PickerMessageID = pickerMessageID
	}
}

// upsertThread adds the entry if its ThreadID is not yet known, otherwise
// updates the label in place. Order is preserved (append-only).
func (c *threadCache) upsertThread(masterID string, e threadCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.masters[masterID]
	if m == nil {
		return
	}
	for i := range m.Threads {
		if m.Threads[i].ThreadID == e.ThreadID {
			m.Threads[i].Label = e.Label
			return
		}
	}
	m.Threads = append(m.Threads, e)
}

func (c *threadCache) master(masterID string) (*threadCacheMaster, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.masters[masterID]
	if !ok {
		return nil, false
	}
	// Return a copy so callers can't mutate the cache without going through
	// the upsert methods.
	out := *m
	out.Threads = append([]threadCacheEntry(nil), m.Threads...)
	return &out, true
}

// allMasters returns a sorted snapshot of every master channel currently
// in the cache. The slice is detached from the cache; mutating it does not
// affect cache state.
func (c *threadCache) allMasters() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.masters))
	for id := range c.masters {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/discordbot/ -run TestThreadCache -v
```
Expected: PASS for both `TestThreadCacheRoundTrip` and `TestThreadCacheMastersForUser`.

- [ ] **Step 5: Commit**

```
git add processor/internal/discordbot/autocreate_threads.go processor/internal/discordbot/autocreate_threads_test.go
git commit -m "discordbot: add JSON-backed thread cache for autocreate"
```

---

## Task 3: Schema additions to channelTemplate

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

Extend the existing `channelEntry` struct with optional `Threads` and `ThreadPicker` fields. The existing JSON shape stays valid (both fields are optional and parse as zero values when absent).

- [ ] **Step 1: Add the struct fields**

In `processor/internal/discordbot/autocreate.go`, add to the `channelEntry` struct (currently around line 36):

```go
type channelEntry struct {
	ChannelName  string             `json:"channelName"`
	ChannelType  string             `json:"channelType"` // "text" or "voice"
	Topic        string             `json:"topic"`
	ControlType  string             `json:"controlType"` // "bot" or "webhook"
	WebhookName  string             `json:"webhookName"`
	Roles        []roleEntry        `json:"roles"`
	Commands     []string           `json:"commands"`

	// New thread-picker fields. Both are optional; an entry with no
	// Threads block behaves exactly as before.
	ThreadPicker *threadPickerDef   `json:"threadPicker,omitempty"`
	Threads      []threadEntry      `json:"threads,omitempty"`
}

// threadPickerDef configures the per-master "click to join" embed.
// Strings support {0}-style placeholder expansion against the same
// template args as the rest of channelTemplate.
type threadPickerDef struct {
	EmbedTitle       string `json:"embedTitle"`
	EmbedDescription string `json:"embedDescription"`
	Pinned           bool   `json:"pinned"`
}

// threadEntry is one private thread under a parent text channel.
type threadEntry struct {
	Name        string   `json:"name"`        // thread name (also default button label)
	ButtonLabel string   `json:"buttonLabel"` // optional override
	ButtonStyle string   `json:"buttonStyle"` // "primary" / "secondary" / "success" / "danger" — secondary if blank
	Commands    []string `json:"commands"`    // run as the thread's human at creation
}
```

- [ ] **Step 2: Build to confirm the struct compiles**

```
go build ./internal/discordbot/...
```
Expected: succeeds with no output.

- [ ] **Step 3: Commit**

```
git add processor/internal/discordbot/autocreate.go
git commit -m "discordbot: add Threads and ThreadPicker to channel template schema"
```

---

## Task 4: Picker embed builder

**Files:**
- Modify: `processor/internal/discordbot/autocreate_threads.go`
- Modify: `processor/internal/discordbot/autocreate_threads_test.go`

Pure function that turns the cached entries plus the configured picker into the discordgo embed + components payload. Tests verify button count, custom-id format, and that text expansion against template args works.

- [ ] **Step 1: Write the failing test**

```go
// Append to autocreate_threads_test.go
import "github.com/bwmarrin/discordgo"

func TestBuildPickerPayload(t *testing.T) {
	picker := &threadPickerDef{
		EmbedTitle:       "Area alerts for {0}",
		EmbedDescription: "Click to join.",
		Pinned:           true,
	}
	threads := []threadCacheEntry{
		{ThreadID: "t1", Label: "Hundo"},
		{ThreadID: "t2", Label: "Nundo"},
	}

	embeds, components := buildPickerPayload("master1", picker, threads, []string{"amsterdam_apollo"})

	if len(embeds) != 1 {
		t.Fatalf("embeds len = %d, want 1", len(embeds))
	}
	if embeds[0].Title != "Area alerts for amsterdam_apollo" {
		t.Errorf("title = %q, want template-expanded", embeds[0].Title)
	}
	if len(components) != 1 {
		t.Fatalf("components len = %d, want one ActionsRow", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("first component not ActionsRow: %T", components[0])
	}
	if len(row.Components) != 2 {
		t.Errorf("buttons = %d, want 2", len(row.Components))
	}
	btn0 := row.Components[0].(discordgo.Button)
	if btn0.Label != "Hundo" {
		t.Errorf("button label = %q, want Hundo", btn0.Label)
	}
	wantID := "poracle:thread:master1:t1:join"
	if btn0.CustomID != wantID {
		t.Errorf("custom_id = %q, want %q", btn0.CustomID, wantID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/discordbot/ -run TestBuildPickerPayload -v
```
Expected: FAIL with "undefined: buildPickerPayload".

- [ ] **Step 3: Implement the builder**

Append to `processor/internal/discordbot/autocreate_threads.go`:

```go
import "github.com/bwmarrin/discordgo"

// buildPickerPayload returns the embed and components for the master
// channel's picker post. Discord caps action rows at 5 buttons, so
// callers should keep configured threads ≤ 5 per master in v1
// (validation in autocreate enforces this).
func buildPickerPayload(masterID string, picker *threadPickerDef, threads []threadCacheEntry, args []string) ([]*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	title := formatTemplate(picker.EmbedTitle, args)
	desc := formatTemplate(picker.EmbedDescription, args)

	embeds := []*discordgo.MessageEmbed{{
		Title:       title,
		Description: desc,
	}}

	buttons := make([]discordgo.MessageComponent, 0, len(threads))
	for _, th := range threads {
		buttons = append(buttons, discordgo.Button{
			Label:    th.Label,
			Style:    discordgo.SecondaryButton,
			CustomID: encodeThreadJoinID(masterID, th.ThreadID),
		})
	}
	if len(buttons) == 0 {
		return embeds, nil
	}
	return embeds, []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}
```

`formatTemplate` already exists in `autocreate.go` — it's the same `{0}` / `{1}` substitution used for channel commands.

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/discordbot/ -run TestBuildPickerPayload -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add processor/internal/discordbot/autocreate_threads.go processor/internal/discordbot/autocreate_threads_test.go
git commit -m "discordbot: add picker embed + button builder"
```

---

## Task 5: Cache wired onto the bot, loaded at startup

**Files:**
- Modify: `processor/internal/discordbot/bot.go`
- Modify: `processor/internal/discordbot/autocreate_threads.go`

The cache instance lives on the `Bot` struct so the autocreate handler, the interaction handler, and the reconciler all share the same view. Path is `<configDir>/.cache/autocreate-threads.json`.

- [ ] **Step 1: Add a constructor for the configured path**

Append to `autocreate_threads.go`:

```go
import "path/filepath"

// threadCachePath returns the on-disk location for the cache file.
// configDir is the value getConfigDir() resolves for the running process.
func threadCachePath(configDir string) string {
	return filepath.Join(configDir, ".cache", "autocreate-threads.json")
}
```

- [ ] **Step 2: Add the field to Bot and load at startup**

Open `processor/internal/discordbot/bot.go`. Find the `Bot` struct (search for `type Bot struct`) and add:

```go
	threadCache *threadCache
```

Then in the constructor / `Start` path (right after the bot is wired but before `session.Open()` — typically next to where `b.Humans = ...` is set), add:

```go
	b.threadCache = newThreadCache(threadCachePath(getConfigDir(b.Cfg)))
	if err := b.threadCache.load(); err != nil {
		log.Warnf("discord bot: load thread cache: %v", err)
	}
```

If you can't immediately see `getConfigDir`, search for an existing call (`grep -rn getConfigDir processor/internal/discordbot/`) — it lives in `cmd/processor/main.go` and is exposed via `b.Cfg`. Match the pattern other discordbot files use; if there isn't one, accept the configDir as a constructor argument and pass `getConfigDir(cfg)` from `cmd/processor/main.go`.

- [ ] **Step 3: Build to confirm**

```
go build ./...
```
Expected: succeeds.

- [ ] **Step 4: Commit**

```
git add processor/internal/discordbot/bot.go processor/internal/discordbot/autocreate_threads.go
git commit -m "discordbot: load thread cache on startup"
```

---

## Task 6: Thread creation in !autocreate

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

After the parent channel is created and its commands have run, walk `chDef.Threads`. For each: create the private thread (or skip if cached), register a `discord:thread` human, run the thread's commands. Persist to cache after each successful creation so a partial run survives a crash.

- [ ] **Step 1: Add a helper to autocreate the threads block**

Add this helper near the end of `autocreate.go` (above the existing `formatTemplate` if you want it to read top-down):

```go
// createThreadsForChannel iterates chDef.Threads, creates each private
// thread under masterChannelID (or reuses cached ID), registers the
// thread as a discord:thread Poracle human, and runs its commands list
// against the existing parsed-execute flow. Returns the cache entries
// for the threads it owns so the caller can persist them and emit the
// picker.
func (b *Bot) createThreadsForChannel(s *discordgo.Session, m *discordgo.MessageCreate, guildID, masterChannelID string, chDef channelEntry, subArgs []string) []threadCacheEntry {
	if len(chDef.Threads) == 0 {
		return nil
	}
	if len(chDef.Threads) > 5 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("⚠️ %s: only first 5 threads will get buttons (Discord ActionsRow limit)", chDef.ChannelName))
	}

	cached, _ := b.threadCache.master(masterChannelID)
	cachedByName := map[string]threadCacheEntry{}
	if cached != nil {
		for _, e := range cached.Threads {
			cachedByName[e.Label] = e
		}
	}

	var entries []threadCacheEntry
	for _, th := range chDef.Threads {
		threadName := formatTemplate(th.Name, subArgs)
		label := th.ButtonLabel
		if label == "" {
			label = threadName
		} else {
			label = formatTemplate(label, subArgs)
		}

		var threadID string
		if existing, ok := cachedByName[label]; ok {
			threadID = existing.ThreadID
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Thread %s already cached (%s); skipping create", threadName, threadID))
		} else {
			created, err := s.ThreadStartComplex(masterChannelID, &discordgo.ThreadStart{
				Name:                threadName,
				Type:                discordgo.ChannelTypeGuildPrivateThread,
				AutoArchiveDuration: 10080,
				Invitable:           false,
			})
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("❌ Failed to create thread %s: %v", threadName, err))
				continue
			}
			threadID = created.ID
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("✅ Created private thread %s (%s)", threadName, threadID))
		}

		// Register a discord:thread human row for this thread.
		h := &store.Human{
			ID:      threadID,
			Type:    bot.TypeDiscordThread,
			Name:    threadName,
			Enabled: true,
		}
		if err := b.Humans.Create(h); err != nil {
			// Already-registered is benign on re-runs; log and continue.
			log.Warnf("discord bot: autocreate register thread %s: %v", threadName, err)
		} else {
			_ = b.Humans.CreateDefaultProfile(threadID, threadName, nil, 0, 0)
		}

		// Run the thread's commands against the thread's human.
		threadArgs := append(append([]string{}, subArgs...), threadName)
		for _, cmdText := range th.Commands {
			expanded := formatTemplate(cmdText, threadArgs)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">>> [%s] %s", threadName, expanded))
			b.runOneAutocreateCommand(s, m, guildID, threadID, threadName, expanded, bot.TypeDiscordThread)
		}

		entry := threadCacheEntry{ThreadID: threadID, Label: label}
		entries = append(entries, entry)
		b.threadCache.upsertMaster(guildID, masterChannelID, "")
		b.threadCache.upsertThread(masterChannelID, entry)
		if err := b.threadCache.save(); err != nil {
			log.Warnf("discord bot: persist thread cache: %v", err)
		}
	}
	return entries
}
```

- [ ] **Step 2: Extract the existing command-runner into runOneAutocreateCommand**

Inside `autocreate.go`, the existing block at ~line 296 already parses and runs commands. Pull lines `296`–`~340` into a new method on `*Bot` so both the channel and the thread paths can share it:

```go
// runOneAutocreateCommand parses one !-prefixed command string and
// executes it through the shared registry as the named target.
// Mirrors the inline block previously in handleAutocreate.
func (b *Bot) runOneAutocreateCommand(s *discordgo.Session, m *discordgo.MessageCreate, guildID, targetID, targetName, expanded, targetType string) {
	parsed := b.Parser.Parse(b.Cfg.Discord.Prefix + expanded)
	for _, pc := range parsed {
		handler := b.Registry.Lookup(pc.CommandKey)
		if handler == nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Unknown command: %s", pc.CommandKey))
			continue
		}
		ctx := &bot.CommandContext{
			UserID:     m.Author.ID,
			UserName:   m.Author.Username,
			Platform:   "discord",
			ChannelID:  m.ChannelID,
			GuildID:    guildID,
			IsDM:       false,
			IsAdmin:    true,
			Language:   b.Cfg.General.Locale,
			ProfileNo:  1,
			TargetID:   targetID,
			TargetName: targetName,
			TargetType: targetType,
			// (copy any other fields the existing block sets — match exactly)
		}
		_ = handler.Run(ctx, pc.Args)
	}
}
```

Then replace the inline block at the original site with `b.runOneAutocreateCommand(s, m, guildID, targetID, targetName, expanded, targetType)`.

- [ ] **Step 3: Call the new helper after the existing channel commands run**

In `handleAutocreate`, after the existing per-channel command loop (~line 320 once you've extracted), add:

```go
threadEntries := b.createThreadsForChannel(s, m, guildID, channelID, chDef, subArgsUnder)
_ = threadEntries // used in Task 7 (picker post)
```

(`channelID` is whatever local variable already holds the master channel's Discord ID — confirm by reading the surrounding code; the existing channel-creation block stores it.)

- [ ] **Step 4: Build**

```
go build ./...
```
Expected: succeeds. Likely additions to imports: nothing new — `discordgo` and `store` are already imported.

- [ ] **Step 5: Commit**

```
git add processor/internal/discordbot/autocreate.go
git commit -m "discordbot: !autocreate creates private threads + registers humans"
```

---

## Task 7: Picker post (idempotent)

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go`

After threads exist, post the picker in the master channel — or edit it if the cache already records a `PickerMessageID`. Pin if `picker.Pinned`.

- [ ] **Step 1: Add a helper that posts or edits the picker**

Append to `autocreate.go`:

```go
// emitPickerPost creates or edits the picker message for masterChannelID.
// Idempotent: if the cache holds a PickerMessageID and the message still
// exists, it's edited in place; otherwise a fresh message is posted and
// its ID is written back to the cache.
func (b *Bot) emitPickerPost(s *discordgo.Session, masterChannelID string, picker *threadPickerDef, entries []threadCacheEntry, args []string) {
	if picker == nil || len(entries) == 0 {
		return
	}
	embeds, components := buildPickerPayload(masterChannelID, picker, entries, args)

	cached, _ := b.threadCache.master(masterChannelID)
	if cached != nil && cached.PickerMessageID != "" {
		_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    masterChannelID,
			ID:         cached.PickerMessageID,
			Embeds:     &embeds,
			Components: &components,
		})
		if err == nil {
			return
		}
		log.Warnf("discord bot: edit picker %s/%s failed (%v) — posting fresh", masterChannelID, cached.PickerMessageID, err)
	}

	msg, err := s.ChannelMessageSendComplex(masterChannelID, &discordgo.MessageSend{
		Embeds:     embeds,
		Components: components,
	})
	if err != nil {
		log.Warnf("discord bot: post picker in %s: %v", masterChannelID, err)
		return
	}
	b.threadCache.upsertMaster(cached.GuildID, masterChannelID, msg.ID)
	if err := b.threadCache.save(); err != nil {
		log.Warnf("discord bot: persist picker message id: %v", err)
	}

	if picker.Pinned {
		if err := s.ChannelMessagePin(masterChannelID, msg.ID); err != nil {
			log.Warnf("discord bot: pin picker %s/%s: %v", masterChannelID, msg.ID, err)
		}
	}
}
```

- [ ] **Step 2: Call it from handleAutocreate**

Right after the `threadEntries := …` line you added in Task 6 step 3, replace the `_ = threadEntries` line with:

```go
if chDef.ThreadPicker != nil {
	b.emitPickerPost(s, channelID, chDef.ThreadPicker, threadEntries, subArgsUnder)
}
```

- [ ] **Step 3: Build**

```
go build ./...
```
Expected: succeeds.

- [ ] **Step 4: Commit**

```
git add processor/internal/discordbot/autocreate.go
git commit -m "discordbot: emit (or edit) picker post after thread creation"
```

---

## Task 8: Interaction handler

**Files:**
- Create: `processor/internal/discordbot/interaction.go`
- Modify: `processor/internal/discordbot/bot.go`

Hook discordgo's `InteractionCreate` event. Decode `custom_id`, sanity-check master access via the interaction's permission bits, and call `ThreadMemberAdd`. Reply ephemerally so other thread members don't see the interaction.

- [ ] **Step 1: Write the handler**

```go
// processor/internal/discordbot/interaction.go
package discordbot

import (
	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
)

// onInteractionCreate routes message-component (button) interactions
// whose custom_id matches the thread-join prefix. Other interaction
// types are ignored — Poracle doesn't currently use slash commands or
// modals through this handler.
func (b *Bot) onInteractionCreate(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if ic.Type != discordgo.InteractionMessageComponent {
		return
	}
	data := ic.MessageComponentData()
	masterID, threadID, ok := decodeThreadJoinID(data.CustomID)
	if !ok {
		return
	}

	userID := ""
	if ic.Member != nil && ic.Member.User != nil {
		userID = ic.Member.User.ID
	} else if ic.User != nil {
		userID = ic.User.ID
	}
	if userID == "" {
		return
	}

	// Authorisation: the user must currently see the master channel.
	// Discord populates ic.Member.Permissions with the effective bits
	// for the channel the interaction was raised from, which is the
	// master channel (the picker lives there).
	if ic.Member == nil || ic.Member.Permissions&discordgo.PermissionViewChannel == 0 {
		respondEphemeral(s, ic, "🙅 You don't have access to this channel.")
		return
	}

	// Already a thread member? No-op per design (B answer).
	tm, err := s.ThreadMember(threadID, userID)
	if err == nil && tm != nil {
		respondEphemeral(s, ic, "👌 You're already in this thread.")
		return
	}

	if err := s.ThreadMemberAdd(threadID, userID); err != nil {
		log.Warnf("discord bot: ThreadMemberAdd master=%s thread=%s user=%s: %v", masterID, threadID, userID, err)
		respondEphemeral(s, ic, "❌ Couldn't add you to the thread — please try again later.")
		return
	}
	respondEphemeral(s, ic, "✅ Joined.")
}

func respondEphemeral(s *discordgo.Session, ic *discordgo.InteractionCreate, msg string) {
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Warnf("discord bot: InteractionRespond: %v", err)
	}
}
```

- [ ] **Step 2: Register the handler**

In `processor/internal/discordbot/bot.go`, find the `session.AddHandler(...)` block (lines 59–64 today) and add:

```go
session.AddHandler(b.onInteractionCreate)
```

next to the existing handlers.

- [ ] **Step 3: Build**

```
go build ./...
```
Expected: succeeds.

- [ ] **Step 4: Commit**

```
git add processor/internal/discordbot/interaction.go processor/internal/discordbot/bot.go
git commit -m "discordbot: handle thread-join button clicks"
```

---

## Task 9: Documentation

**Files:**
- Modify: `CLAUDE.md`

Brief paragraph under `## Discord Reconciliation` so future maintainers (and Claude) know the picker flow and the opt-in flag exist.

- [ ] **Step 1: Add the paragraph**

In `CLAUDE.md`, find `## Discord Reconciliation` and add this subsection at its end:

```markdown
### Picker-managed threads

`!autocreate` can create private threads under a master text channel and post a button-driven picker. Each thread is registered as its own `discord:thread` Poracle human and runs its configured `commands` block at creation time, so it has tracking rules from the moment it exists. Button `custom_id`s are stateless (`poracle:thread:<masterID>:<threadID>:join`), so click handling survives bot restarts. The master→threads map is cached at `config/.cache/autocreate-threads.json`.

Role-loss handling is left to Discord: private-thread visibility inherits View Channel permission on the parent text channel, so a user who loses the master-channel role automatically loses access to the threads under it without any reconciler involvement.
```

- [ ] **Step 2: Commit**

```
git add CLAUDE.md
git commit -m "docs: document !autocreate threads + role_sync_threads"
```

---

## Task 10: Final integration check

- [ ] **Step 1: Full test suite**

```
cd processor
go test ./...
```
Expected: every package PASS.

- [ ] **Step 2: Build the binary**

```
go build ./cmd/processor
```
Expected: produces `processor/processor` (or whatever the local builds to) with no errors.

- [ ] **Step 3: Push the branch**

```
git push -u origin autocreate-threads
```

---

## Out of scope for phase 1

Documented here so reviewers know what to expect (and what not to expect):

- **Auto-archive duration as a config option.** Currently hard-coded to 10080 (7 days) in Task 6. Promotion to a per-thread / per-master config field is deferred — flagged in the user spec as "later this could be an option".
- **Thread deletion on config removal.** When the JSON drops a thread, `!autocreate` warns ("thread X exists in cache but is no longer in your config") but does not delete the Discord thread or the human row. Manual cleanup for v1.
- **Per-thread role gating.** All buttons are equally available to anyone who can see the master channel. If the future needs "this thread only for role X", the codec gains a third encoded field.
- **Interactive editor for the threads JSON.** The user mentioned this for later.
- **Composite `discord:thread` IDs encoding the master channel.** Today the human row keys on `threadID` alone — fine for delivery, fine for reconciliation since the cache resolves master from thread. Reconsidered if/when threads gain cross-master sharing.
- **Active `ThreadMemberRemove` on role loss.** Discord enforces parent-channel View permission for private threads, so a user who loses the master-channel role loses thread visibility automatically — they remain a thread member object on Discord's side but can no longer see the thread or its messages. Adding an explicit reconciler call would only matter if we wanted the thread-member list to *visibly* shrink in real time, which isn't a phase-1 requirement. If that becomes useful, the wiring is: a `RoleSyncThreads` flag on `[discord]`, a helper on `Reconciliation` that walks the thread cache and calls `s.ThreadMemberRemove`, and a hook in both `reconcileNonAreaSecurity` and `reconcileAreaSecurity` at the `before && !after` transition.
