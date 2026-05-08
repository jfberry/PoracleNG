package discordbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
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

// threadCacheEntry is one button on one master channel's picker.
// Style is the configured button style ("primary"/"secondary"/"success"/
// "danger"). Empty defaults to "secondary" at render time.
type threadCacheEntry struct {
	ThreadID string `json:"threadId"`
	Label    string `json:"label"`
	Style    string `json:"style,omitempty"`
}

// Discord component limits — buttons spread across ActionsRows, then
// across messages when a master has more threads than fit in one.
const (
	pickerButtonsPerRow     = 5
	pickerRowsPerMessage    = 5
	pickerButtonsPerMessage = pickerButtonsPerRow * pickerRowsPerMessage // 25
)

// buttonStyleFor maps the configured string style to its discordgo
// constant. Unknown / empty values fall back to SecondaryButton.
func buttonStyleFor(s string) discordgo.ButtonStyle {
	switch strings.ToLower(s) {
	case "primary":
		return discordgo.PrimaryButton
	case "success":
		return discordgo.SuccessButton
	case "danger":
		return discordgo.DangerButton
	case "secondary", "":
		return discordgo.SecondaryButton
	default:
		return discordgo.SecondaryButton
	}
}

// threadCacheMaster is the per-master section of the on-disk cache.
// PickerMessageIDs is the ordered list of message IDs that make up the
// picker — one per chunk of up to pickerButtonsPerMessage buttons.
type threadCacheMaster struct {
	GuildID          string             `json:"guildId"`
	PickerMessageIDs []string           `json:"pickerMessageIds,omitempty"`
	Threads          []threadCacheEntry `json:"threads"`
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
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("create thread cache dir %s: %w", filepath.Dir(c.path), err)
	}
	if err := os.WriteFile(c.path, data, 0o644); err != nil {
		return fmt.Errorf("write thread cache %s: %w", c.path, err)
	}
	return nil
}

// ensureMaster creates the master entry if absent and (optionally) records
// the guild ID. It never touches PickerMessageIDs — use setPickerMessageIDs
// for that, after the picker post(s) have been emitted.
func (c *threadCache) ensureMaster(guildID, masterID string) {
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
	if guildID != "" {
		m.GuildID = guildID
	}
}

// setPickerMessageIDs replaces the master's picker-message ID list.
// Pass an empty slice to record "no picker currently posted".
func (c *threadCache) setPickerMessageIDs(masterID string, ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.masters[masterID]
	if m == nil {
		return
	}
	m.PickerMessageIDs = append([]string(nil), ids...)
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

// removeThreadByLabel drops the cached thread entry under masterID whose
// label matches. No-op when the master or label isn't found. Returns
// the thread ID that was removed (empty if nothing was removed) so the
// caller can also do Discord/DB cleanup keyed off it.
func (c *threadCache) removeThreadByLabel(masterID, label string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.masters[masterID]
	if m == nil {
		return ""
	}
	for i, e := range m.Threads {
		if e.Label != label {
			continue
		}
		removed := e.ThreadID
		m.Threads = append(m.Threads[:i], m.Threads[i+1:]...)
		return removed
	}
	return ""
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

// pickerMessage is one Discord message in the picker. Most masters fit
// in one message (up to pickerButtonsPerMessage buttons across rows);
// pickers with more threads spill into additional messages with no
// embed (the embed lives only on the first message).
type pickerMessage struct {
	Embeds     []*discordgo.MessageEmbed
	Components []discordgo.MessageComponent
}

// buildPickerMessages chunks threads into one or more Discord messages.
// Each message holds up to pickerButtonsPerMessage buttons, spread across
// ActionsRows of pickerButtonsPerRow buttons each. Only the first message
// carries the configured embed; subsequent messages are button-only so
// the title doesn't repeat.
func buildPickerMessages(masterID string, picker *threadPickerDef, threads []threadCacheEntry, args []string) []pickerMessage {
	if len(threads) == 0 {
		return nil
	}

	var messages []pickerMessage
	for i := 0; i < len(threads); i += pickerButtonsPerMessage {
		end := min(i+pickerButtonsPerMessage, len(threads))
		chunk := threads[i:end]

		msg := pickerMessage{
			Components: chunkButtonsIntoRows(masterID, chunk),
		}
		if i == 0 {
			msg.Embeds = []*discordgo.MessageEmbed{{
				Title:       formatTemplate(picker.EmbedTitle, args),
				Description: formatTemplate(picker.EmbedDescription, args),
			}}
		}
		messages = append(messages, msg)
	}
	return messages
}

// chunkButtonsIntoRows splits one message's worth of threads into
// ActionsRows of up to pickerButtonsPerRow buttons each.
func chunkButtonsIntoRows(masterID string, threads []threadCacheEntry) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	for i := 0; i < len(threads); i += pickerButtonsPerRow {
		end := min(i+pickerButtonsPerRow, len(threads))
		rowChunk := threads[i:end]
		buttons := make([]discordgo.MessageComponent, 0, len(rowChunk))
		for _, th := range rowChunk {
			buttons = append(buttons, discordgo.Button{
				Label:    th.Label,
				Style:    buttonStyleFor(th.Style),
				CustomID: encodeThreadJoinID(masterID, th.ThreadID),
			})
		}
		rows = append(rows, discordgo.ActionsRow{Components: buttons})
	}
	return rows
}

// parentByThread returns a snapshot map of threadID → masterChannelID for
// every thread currently tracked in the cache. The returned map is a fresh
// copy; callers may use it without holding any lock.
func (c *threadCache) parentByThread() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string)
	for masterID, m := range c.masters {
		if m == nil {
			continue
		}
		for _, e := range m.Threads {
			if e.ThreadID != "" {
				out[e.ThreadID] = masterID
			}
		}
	}
	return out
}

// pruneMissingForGuild drops thread entries whose ThreadID is not in
// liveThreadIDs from every master that belongs to guildID. Saves the
// cache if anything changed and returns the count removed. Called by
// the bulk runner after the autocreate-cache reconcile so both caches
// stay in sync — without this, dry-run reads stale threadCache entries
// and reports "Reusing thread" for threads that no longer exist in
// Discord.
func (c *threadCache) pruneMissingForGuild(guildID string, liveThreadIDs map[string]bool) int {
	c.mu.Lock()
	changed := false
	removed := 0
	for _, m := range c.masters {
		if m == nil || m.GuildID != guildID {
			continue
		}
		kept := m.Threads[:0]
		for _, e := range m.Threads {
			if liveThreadIDs[e.ThreadID] {
				kept = append(kept, e)
			} else {
				removed++
				changed = true
			}
		}
		m.Threads = kept
	}
	c.mu.Unlock()
	if changed {
		if err := c.save(); err != nil {
			log.Warnf("discord bot: persist threadCache after prune: %v", err)
		}
	}
	return removed
}

// threadCachePath returns the on-disk location for the cache file,
// rooted at the project's config directory. Mirrors the convention used
// by other config/.cache files (e.g. gym-state.json).
func threadCachePath(baseDir string) string {
	return filepath.Join(baseDir, "config", ".cache", "autocreate-threads.json")
}
