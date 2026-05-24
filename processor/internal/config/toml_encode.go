package config

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// SectionOrder is the canonical display + on-disk order of the
// top-level config.toml sections. Operator-facing surfaces (both the
// !pa config viewer and the rewritten config.toml on disk) follow this
// order so the "important stuff" (processor / database / discord /
// telegram) appears near the top and operational sections later.
//
// Sections not listed here fall through to an alphabetical tail in
// the encoder, so adding a new section to the Config struct without
// touching this list is safe — it just lands in the tail until
// someone decides where it belongs.
var SectionOrder = []string{
	"processor",
	"database",
	"geofence",
	"discord",
	"telegram",
	"general",
	"locale",
	"logging",
	"webhookLogging",
}

// EncodeOrderedTOML emits rawMap as TOML with top-level sections in
// SectionOrder priority. Sections not in the priority list are emitted
// alphabetically after the ordered ones (so a new section landing in
// the struct without a corresponding SectionOrder entry still gets
// written, just in the tail).
//
// Keys WITHIN each section follow BurntSushi's default ordering
// (alphabetical) — operator value matters most at the section level.
//
// A blank line is inserted between sections so the rendered file
// doesn't read as a wall of [section]/key=value lines.
func EncodeOrderedTOML(rawMap map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	seen := make(map[string]bool, len(SectionOrder))

	// Ordered sections first.
	for _, name := range SectionOrder {
		val, ok := rawMap[name]
		if !ok {
			continue
		}
		seen[name] = true
		if err := writeSection(&buf, name, val); err != nil {
			return nil, fmt.Errorf("encode [%s]: %w", name, err)
		}
	}

	// Alphabetical tail for anything not in the priority list. Stable
	// output so future struct additions don't shuffle existing files
	// arbitrarily on save.
	var tail []string
	for k := range rawMap {
		if !seen[k] {
			tail = append(tail, k)
		}
	}
	sort.Strings(tail)
	for _, name := range tail {
		if err := writeSection(&buf, name, rawMap[name]); err != nil {
			return nil, fmt.Errorf("encode [%s]: %w", name, err)
		}
	}

	return buf.Bytes(), nil
}

// writeSection emits a single top-level [section] block. Uses the
// BurntSushi encoder on a one-key wrapper so nested sub-tables
// (reconciliation.discord, geofence.koji) get their proper
// [section.sub] headers without us having to walk the shape by hand.
//
// Adds a trailing blank line to space sections in the rendered file.
func writeSection(buf *bytes.Buffer, name string, value any) error {
	wrap := map[string]any{name: value}
	enc := toml.NewEncoder(buf)
	enc.Indent = "  "
	if err := enc.Encode(wrap); err != nil {
		return err
	}
	buf.WriteByte('\n')
	return nil
}
