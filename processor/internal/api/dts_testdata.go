package api

import (
	"encoding/json"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/dtsmap"
)

// TestDataEntry represents a single test scenario from testdata.json.
type TestDataEntry struct {
	Type     string          `json:"type"`
	Test     string          `json:"test"`
	Location string          `json:"location"`
	Webhook  json.RawMessage `json:"webhook"`
	// DtsType is set only when the entry was returned by a ?dtsType= query
	// (RegisterDTSTestdata) — the DTS template type it was resolved to
	// preview under. Empty (and omitted) for entries returned by the legacy
	// ?type=<webhookType> filter or an unfiltered request, preserving the
	// pre-existing wire shape for those callers.
	DtsType string `json:"dtsType,omitempty"`
}

// filterByDTSType resolves dtsType via the shared dtsmap alias table and
// returns the subset of entries that preview it, each tagged with dtsType.
// Returns nil (no matches) for an unrecognized dtsType.
//
// Two source webhook types need a payload-shape split beyond a plain
// e.Type == src.WebhookType match, mirroring resolveDTSTypeFromRaw in
// cmd/processor/test.go:
//   - "pokestop" is the shared wire category for both "invasion" and "lure"
//     (dtsmap.Alias("pokestop") deliberately has no identity entry — it
//     can't resolve to a single TemplateType on its own, see dtsmap's doc
//     comment). Entries are classified by which fields their webhook
//     payload carries.
//   - "raid" is the shared wire category for both "raid" (boss) and "egg"
//     entries, split by whether the payload's pokemon_id is set.
func filterByDTSType(entries []TestDataEntry, dtsType string) []TestDataEntry {
	src, ok := dtsmap.Alias(dtsType)
	if !ok {
		return nil
	}

	var filtered []TestDataEntry
	for _, e := range entries {
		switch src.WebhookType {
		case "pokestop":
			if e.Type != "pokestop" || classifyPokestopEntry(e.Webhook) != dtsType {
				continue
			}
		case "raid":
			if e.Type != "raid" || classifyRaidEntry(e.Webhook) != dtsType {
				continue
			}
		default:
			if e.Type != src.WebhookType {
				continue
			}
		}
		tagged := e
		tagged.DtsType = dtsType
		filtered = append(filtered, tagged)
	}
	return filtered
}

// classifyPokestopEntry splits a "pokestop"-typed testdata payload into
// "invasion" or "lure" by which fields it carries — the same payload-shape
// split the editor's capture-test-data.mjs performs client-side today, and
// the resolveDTSTypeFromRaw pokestop branch performs for live previews.
// Lure fields are checked first since they're the more specific signal;
// grunt/incident payloads (including the content-less showcase envelope,
// which carries grunt_type/character/display_type but no lure fields) fall
// through to "invasion".
func classifyPokestopEntry(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	hasAny := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := fields[k]; ok {
				return true
			}
		}
		return false
	}
	if hasAny("lure_id", "lure_expiration", "lure_type") {
		return "lure"
	}
	if hasAny("grunt_type", "character", "display_type", "incident_grunt_type") {
		return "invasion"
	}
	return ""
}

// classifyRaidEntry splits a "raid"-typed testdata payload into "raid"
// (boss) or "egg" by pokemon_id, mirroring resolveDTSTypeFromRaw's raid
// branch in cmd/processor/test.go.
func classifyRaidEntry(raw json.RawMessage) string {
	var peek struct {
		PokemonID int `json:"pokemon_id"`
	}
	if json.Unmarshal(raw, &peek) == nil && peek.PokemonID > 0 {
		return "raid"
	}
	return "egg"
}

// dtsTypeInfo is the wire shape of a single entry in the discoverable
// DTS-type -> source map (dtsmap.Source, JSON-cased for the API).
type dtsTypeInfo struct {
	WebhookType  string `json:"webhookType"`
	TemplateType string `json:"templateType"`
	Derived      bool   `json:"derived"`
}

// dtsTypeInfoMap converts the shared dtsmap table to its API wire shape, so
// clients (the DTS editor) can discover every DTS type name, its source
// webhook type, and whether it's derived instead of hardcoding a copy.
func dtsTypeInfoMap() map[string]dtsTypeInfo {
	src := dtsmap.TypeMap()
	out := make(map[string]dtsTypeInfo, len(src))
	for name, s := range src {
		out[name] = dtsTypeInfo{WebhookType: s.WebhookType, TemplateType: s.TemplateType, Derived: s.Derived}
	}
	return out
}

// loadTestdata reads testdata.json, merging config (overrides) with fallback (defaults).
func loadTestdata(configDir, fallbackDir string) []TestDataEntry {
	// Load fallback first
	fallbackEntries := readTestdataFile(filepath.Join(fallbackDir, "testdata.json"))

	// Load config override
	configEntries := readTestdataFile(filepath.Join(configDir, "testdata.json"))

	if fallbackEntries == nil && configEntries == nil {
		return nil
	}

	if configEntries == nil {
		return fallbackEntries
	}
	if fallbackEntries == nil {
		return configEntries
	}

	// Merge: config entries override fallback entries by type+test key
	configKeys := make(map[string]TestDataEntry, len(configEntries))
	for _, e := range configEntries {
		configKeys[e.Type+"/"+e.Test] = e
	}

	merged := make([]TestDataEntry, 0, len(fallbackEntries)+len(configEntries))
	for _, e := range fallbackEntries {
		key := e.Type + "/" + e.Test
		if override, ok := configKeys[key]; ok {
			merged = append(merged, override)
			delete(configKeys, key)
		} else {
			merged = append(merged, e)
		}
	}
	// Append config-only entries not present in fallback
	for _, e := range configKeys {
		merged = append(merged, e)
	}

	return merged
}

func readTestdataFile(path string) []TestDataEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []TestDataEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Warnf("dts testdata: failed to parse %s: %v", path, err)
		return nil
	}
	return entries
}
