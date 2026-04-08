package dts

import (
	"encoding/json"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// EmojiLookup resolves emoji strings by key and platform.
// Custom per-platform overrides (from emoji.json) take priority over defaults (from util.json).
type EmojiLookup struct {
	custom   map[string]map[string]string // platform → key → emoji string
	defaults map[string]string            // key → emoji string
}

// LoadEmoji creates an EmojiLookup by reading optional {configDir}/emoji.json
// and using utilEmojis as the default fallback layer.
// If emoji.json does not exist, only defaults are used.
// If utilEmojis is nil, an empty default map is used.
func LoadEmoji(configDir string, utilEmojis map[string]string) *EmojiLookup {
	e := &EmojiLookup{
		custom:   make(map[string]map[string]string),
		defaults: utilEmojis,
	}
	if e.defaults == nil {
		e.defaults = make(map[string]string)
	}

	path := filepath.Join(configDir, "emoji.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("dts: read emoji.json: %v", err)
		}
		return e
	}

	var custom map[string]map[string]string
	if err := json.Unmarshal(data, &custom); err != nil {
		log.Warnf("dts: parse emoji.json: %v", err)
		return e
	}
	e.custom = custom
	return e
}

// Defaults returns the default key→emoji map (from util.json).
func (e *EmojiLookup) Defaults() map[string]string {
	if e == nil {
		return nil
	}
	out := make(map[string]string, len(e.defaults))
	for k, v := range e.defaults {
		out[k] = v
	}
	return out
}

// PlatformOverrides returns the per-platform custom overrides (from emoji.json).
func (e *EmojiLookup) PlatformOverrides() map[string]map[string]string {
	if e == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(e.custom))
	for p, m := range e.custom {
		inner := make(map[string]string, len(m))
		for k, v := range m {
			inner[k] = v
		}
		out[p] = inner
	}
	return out
}

// MergedFor returns the effective key→emoji map for a given platform: defaults
// overlaid with the platform's overrides. Useful for clients that want a
// single flat map to resolve from.
func (e *EmojiLookup) MergedFor(platform string) map[string]string {
	if e == nil {
		return nil
	}
	out := make(map[string]string, len(e.defaults))
	for k, v := range e.defaults {
		out[k] = v
	}
	if platformMap, ok := e.custom[platform]; ok {
		for k, v := range platformMap {
			out[k] = v
		}
	}
	return out
}

// Lookup resolves an emoji string for the given key and platform.
// It checks custom[platform][key] first, then defaults[key], then returns "".
func (e *EmojiLookup) Lookup(key, platform string) string {
	if e == nil {
		return ""
	}
	if platformMap, ok := e.custom[platform]; ok {
		if emoji, ok := platformMap[key]; ok {
			return emoji
		}
	}
	if emoji, ok := e.defaults[key]; ok {
		return emoji
	}
	return ""
}
