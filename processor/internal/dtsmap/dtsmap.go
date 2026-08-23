// Package dtsmap holds the single canonical table mapping every DTS
// template-type name (and every raw webhook-type spelling) to the webhook
// source it is enriched from. It is shared between the processor's
// enrichment dispatch (cmd/processor) and the API's testdata endpoints
// (internal/api) — cmd/processor can import internal packages, but
// internal/api cannot import package main, so the table lives here as the
// single source of truth both sides use.
package dtsmap

import (
	"maps"
	"strings"
)

// Source describes where a DTS template-type name comes from: which raw
// webhook type it is enriched from, the canonical DTS template type name
// used for template selection, and whether the source data is a "derived"
// event — one that doesn't arrive directly on the webhook receiver but is
// synthesized from processor-internal state (e.g. an encounter change, an
// RSVP update, a summary digest).
type Source struct {
	WebhookType  string
	TemplateType string
	Derived      bool
}

// types is the single canonical table mapping every name the editor,
// !poracle-test, and the DTS template-selection system may use for an alert
// type to its underlying webhook source and DTS template type. It covers
// two kinds of entries:
//
//   - DTS template-type names (the keys in internal/api/dts_fields.go's
//     fieldsByType, e.g. "monster", "egg", "monsterChanged") — these are
//     the names the editor and !poracle-test address types by.
//   - Identity entries for raw webhook-type spellings (e.g. "pokemon",
//     "max_battle", "fort_update") that aren't already a DTS name above, so
//     callers can pass either spelling interchangeably.
//
// "pokestop" is intentionally NOT an identity entry: it's the shared Golbat
// wire category for both invasion and lure, disambiguated by payload shape
// (see resolveDTSTypeFromRaw in cmd/processor/test.go), not by name — it
// can't resolve to a single TemplateType on its own. Callers that walk this
// table (e.g. the testdata endpoint) must keep that guard: don't match
// testdata entries against a resolved WebhookType of "pokestop" the way
// every other entry is matched — split by payload shape instead.
var types = map[string]Source{
	// DTS template-type names.
	//
	// The four derived types' WebhookType values are the literal
	// testdata.json "type" field / live-dispatch wire spelling established
	// by the tasks that actually implemented them (monster_changed,
	// rsvp_changes, quest_summary use underscores; weatherchange has no
	// separator at all) — NOT the hyphenated CLI-display spelling
	// (monster-changed, rsvp-changes, quest-summary, weather-change) also
	// registered below as identity aliases. Walkers that match testdata
	// entries by `entry.Type == src.WebhookType` (see internal/api's
	// testdata endpoint) depend on this being the true wire string.
	"monster":        {WebhookType: "pokemon", TemplateType: "monster"},
	"monsterNoIv":    {WebhookType: "pokemon", TemplateType: "monsterNoIv"},
	"monsterChanged": {WebhookType: "monster_changed", TemplateType: "monsterChanged", Derived: true},
	"raid":           {WebhookType: "raid", TemplateType: "raid"},
	"egg":            {WebhookType: "raid", TemplateType: "egg"},
	"rsvpChanges":    {WebhookType: "rsvp_changes", TemplateType: "rsvpChanges", Derived: true},
	"quest":          {WebhookType: "quest", TemplateType: "quest"},
	"questSummary":   {WebhookType: "quest_summary", TemplateType: "questSummary", Derived: true},
	"invasion":       {WebhookType: "pokestop", TemplateType: "invasion"},
	"incident":       {WebhookType: "incident", TemplateType: "incident", Derived: true},
	"showcase":       {WebhookType: "showcase", TemplateType: "showcase"},
	"lure":           {WebhookType: "pokestop", TemplateType: "lure"},
	"weatherchange":  {WebhookType: "weatherchange", TemplateType: "weatherchange", Derived: true},
	"gym":            {WebhookType: "gym", TemplateType: "gym"},
	"nest":           {WebhookType: "nest", TemplateType: "nest"},
	"maxbattle":      {WebhookType: "max_battle", TemplateType: "maxbattle"},

	// Identity entries for raw webhook-type spellings not already covered
	// above.
	//
	// "fort-update" is the DTS TemplateType name for fort updates (see
	// internal/api/dts_fields.go's fieldsByType key "fort-update"), but the
	// wire/testdata.json spelling is the underscored "fort_update" — same
	// mismatch class as the four derived types above. Both spellings must
	// resolve to WebhookType "fort_update" so dtsmap.Alias("fort-update")
	// matches testdata entries and ?dtsType=fort-update isn't silently
	// empty.
	//
	// "max-battle" is the CLI-display / API hyphenated spelling for
	// "maxbattle" (see !poracle-test's validHooks and POST /api/test's
	// resolveTestWireType, internal/api/test_type.go) — same identity-entry
	// treatment as "fort-update" above, so both hyphenated forms resolve
	// consistently instead of only one of the two.
	"pokemon":     {WebhookType: "pokemon", TemplateType: "monster"},
	"max_battle":  {WebhookType: "max_battle", TemplateType: "maxbattle"},
	"max-battle":  {WebhookType: "max_battle", TemplateType: "maxbattle"},
	"fort_update": {WebhookType: "fort_update", TemplateType: "fort-update"},
	"fort-update": {WebhookType: "fort_update", TemplateType: "fort-update"},

	// Identity entries for the derived event's CLI-display (hyphenated)
	// spelling (see !poracle-test's validHooks in
	// internal/bot/commands/poracletest.go, which converts hyphens to
	// underscores before dispatch/testdata lookup) and its underlying
	// wire/testdata.json spelling, so a derived name resolves the same way
	// no matter which of the three forms (DTS template-type name, CLI
	// hyphenated display name, or wire underscore name) a caller uses.
	"monster-changed": {WebhookType: "monster_changed", TemplateType: "monsterChanged", Derived: true},
	"rsvp-changes":    {WebhookType: "rsvp_changes", TemplateType: "rsvpChanges", Derived: true},
	"quest-summary":   {WebhookType: "quest_summary", TemplateType: "questSummary", Derived: true},
	"weather-change":  {WebhookType: "weatherchange", TemplateType: "weatherchange", Derived: true},
	"monster_changed": {WebhookType: "monster_changed", TemplateType: "monsterChanged", Derived: true},
	"rsvp_changes":    {WebhookType: "rsvp_changes", TemplateType: "rsvpChanges", Derived: true},
	"quest_summary":   {WebhookType: "quest_summary", TemplateType: "questSummary", Derived: true},
}

// Alias resolves a DTS template-type name (e.g. "monster", "egg",
// "monsterChanged") OR a raw webhook type (e.g. "pokemon", "max_battle") to
// its canonical Source. The second return value is false when name is not
// recognized.
func Alias(name string) (Source, bool) {
	src, ok := types[name]
	return src, ok
}

// typesFold is a lowercase-keyed mirror of types, built once at package
// init (a plain package-level var initializer — Go runs these
// single-threaded before main/any goroutines, so no locking is needed) for
// AliasFold. None of the canonical keys collide once lowercased: the
// camelCase, hyphenated, and underscored spellings of the same derived type
// ("monsterChanged" / "monster-changed" / "monster_changed") differ by
// separator, not just case, so folding case never merges two distinct
// entries.
var typesFold = buildTypesFold()

func buildTypesFold() map[string]Source {
	out := make(map[string]Source, len(types))
	for k, v := range types {
		out[strings.ToLower(k)] = v
	}
	return out
}

// AliasFold is like Alias but matches name case-insensitively. It exists for
// callers whose input has already been lowercased upstream and so can't be
// matched against camelCase keys like "monsterChanged" or "monsterNoIv" by
// Alias alone — e.g. !poracle-test's leading type token, which the bot
// command parser lowercases before the command ever sees it (see
// internal/bot/parser.go's tokenize and internal/bot/commands/poracletest.go's
// resolveHookType).
func AliasFold(name string) (Source, bool) {
	src, ok := typesFold[strings.ToLower(name)]
	return src, ok
}

// TypeMap returns a defensive copy of the full canonical table, for callers
// (e.g. the API) to expose to clients.
func TypeMap() map[string]Source {
	out := make(map[string]Source, len(types))
	maps.Copy(out, types)
	return out
}
