package discordbot

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
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
