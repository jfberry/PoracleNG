package dts

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
)

// tomlFile is the wire shape of a TOML DTS file. Mirrors the JSON shape
// (a flat list of entries) using TOML's array-of-tables syntax:
//
//	[[entry]]
//	id = "1"
//	type = "raid"
//	platform = "discord"
//	language = "en"
//	template = """..."""
//
//	[[entry.buttons]]
//	id = "mute_gym"
//	action = "mute"
//	scope = "gym"
//
// One TOML file may declare any number of [[entry]] blocks. The wrapper
// struct exists so BurntSushi/toml has a target type — it can't decode
// `[[entry]]` directly into a top-level []DTSEntry.
type tomlFile struct {
	Entries []DTSEntry `toml:"entry"`
}

// loadTOMLFile reads and parses a single TOML DTS file. Returns the
// entries it contained — possibly empty if the file is valid but
// declares no [[entry]] blocks.
//
// Loader policy mirrors the JSON path: file-level errors propagate to
// the caller (which logs + skips the whole file); per-entry validity
// is the caller's responsibility (validateEntryButtons drops broken
// buttons later, just like the JSON path).
func loadTOMLFile(path string) ([]DTSEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f tomlFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("toml: %w", err)
	}
	return f.Entries, nil
}

// warnDuplicateEntries scans the loaded entries for collisions on the
// (type, id, platform, language) key WITHIN the user-files tier
// (config/dts/* and config/dts.json). Each collision is logged at WARN
// with both file paths and a note about which entry wins (last-loaded).
//
// The intentional override hierarchy (config/dts/* > config/dts.json >
// fallbacks/dts.json) does NOT produce warnings — those collisions are
// by design. Within-tier collisions are an authoring bug that's
// otherwise invisible.
//
// Cheap: O(N) over all entries with a map for prior occurrences.
func warnDuplicateEntries(entries []DTSEntry) {
	type key struct{ typ, id, platform, lang string }
	seen := make(map[key]int) // → index into entries

	for i := range entries {
		// Skip fallback entries — those are the lowest tier of the
		// override hierarchy and intentionally lose to user files.
		if entries[i].Readonly {
			continue
		}
		k := key{
			typ:      entries[i].Type,
			id:       entries[i].ID.String(),
			platform: entries[i].Platform,
			lang:     entries[i].Language,
		}
		if prev, ok := seen[k]; ok {
			// Intentional override: dts/* file overrides config/dts.json.
			// Both are non-readonly so we can't distinguish them by the
			// Readonly flag — fall back to filename heuristic: the bare
			// "dts.json" is the legacy single-file entry point; anything
			// under config/dts/ is the per-file directory.
			prevIsLegacy := isLegacyConfigDTS(entries[prev].sourceFile)
			currIsLegacy := isLegacyConfigDTS(entries[i].sourceFile)
			if prevIsLegacy != currIsLegacy {
				// One is config/dts.json, the other is config/dts/*.toml
				// or *.json — that's the supported hierarchy. Update
				// `seen` to the override (the one that wins) but don't
				// warn.
				if currIsLegacy {
					// Newer entry is the legacy file: keep the dts/* override.
				} else {
					seen[k] = i
				}
				continue
			}
			log.Warnf("dts: duplicate entry key=(%s/%s/%s/%s) in %s and %s — last-loaded wins (%s); silence by giving one a distinct id",
				k.typ, k.id, k.platform, k.lang,
				entries[prev].sourceFile, entries[i].sourceFile,
				entries[i].sourceFile,
			)
			seen[k] = i
			continue
		}
		seen[k] = i
	}
}

// isLegacyConfigDTS reports whether path ends in the single-file
// config/dts.json entry point. Used by the override-vs-collision
// heuristic in warnDuplicateEntries: a collision between dts.json and
// a file under config/dts/ is the intentional override hierarchy, not
// an authoring bug.
func isLegacyConfigDTS(path string) bool {
	// Cheap suffix check. Operators rarely have multiple dts.json files
	// in their config tree; if they do, this heuristic might tag both
	// as "legacy" and miss a real conflict. That's an acceptable
	// false-negative for v1 — the operator's setup is non-standard
	// either way.
	return len(path) >= len("/dts.json") && path[len(path)-len("/dts.json"):] == "/dts.json"
}
