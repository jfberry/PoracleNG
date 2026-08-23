package commands

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/dtsmap"
)

// PoracleTestCommand implements !poracle-test — send a test webhook.
type PoracleTestCommand struct{}

func (c *PoracleTestCommand) Name() string      { return "cmd.poracle_test" }
func (c *PoracleTestCommand) Aliases() []string { return nil }

var poracleTestParams = []bot.ParamDef{
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.language"},
}

// testdataEntry represents one item from testdata.json.
type testdataEntry struct {
	Type     string         `json:"type"`
	Test     string         `json:"test"`
	Location string         `json:"location,omitempty"`
	Webhook  map[string]any `json:"webhook"`
}

// loadTestdata loads and merges bundled + user testdata.json files.
// User entries (config/testdata.json) override bundled entries (fallbacks/testdata.json)
// with the same (type, test) key.
func loadTestdata(baseDir string) ([]testdataEntry, error) {
	bundledPath := filepath.Join(baseDir, "fallbacks", "testdata.json")
	userPath := filepath.Join(baseDir, "config", "testdata.json")

	var bundled, user []testdataEntry

	if data, err := os.ReadFile(bundledPath); err == nil {
		if err := json.Unmarshal(data, &bundled); err != nil {
			return nil, fmt.Errorf("parse %s: %w", bundledPath, err)
		}
	}

	if data, err := os.ReadFile(userPath); err == nil {
		if err := json.Unmarshal(data, &user); err != nil {
			log.Warnf("poracle-test: failed to parse %s: %v", userPath, err)
			user = nil
		}
	}

	userByKey := make(map[string]testdataEntry, len(user))
	for _, e := range user {
		userByKey[e.Type+"/"+e.Test] = e
	}

	result := make([]testdataEntry, 0, len(bundled)+len(user))
	for _, e := range bundled {
		key := e.Type + "/" + e.Test
		if override, ok := userByKey[key]; ok {
			result = append(result, override)
			delete(userByKey, key)
		} else {
			result = append(result, e)
		}
	}
	for _, e := range user {
		if _, ok := userByKey[e.Type+"/"+e.Test]; ok {
			result = append(result, e)
		}
	}

	return result, nil
}

// validHooks lists the leading !poracle-test type tokens accepted before
// falling back to dtsmap resolution in resolveHookType: raw webhook-type
// spellings (pokemon, raid, pokestop, ...) plus the CLI-display hyphenated
// spelling for each derived type (monster-changed, max-battle, ...). Also
// echoed verbatim in the usage/unknown-type reply text.
var validHooks = []string{"pokemon", "raid", "pokestop", "incident", "gym", "nest", "quest", "quest-summary", "monster-changed", "rsvp-changes", "fort-update", "max-battle", "showcase", "weatherchange"}

func (c *PoracleTestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	tr := ctx.Tr()

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.Tf("msg.poracle_test.usage", strings.Join(validHooks, ", "))}}
	}

	hookTypeDisplay := args[0]
	hookType, valid := resolveHookType(hookTypeDisplay)
	if !valid {
		return []bot.Reply{{Text: tr.Tf("msg.poracle_test.usage", strings.Join(validHooks, ", "))}}
	}

	// Load testdata
	testdata, err := loadTestdata(ctx.Config.BaseDir)
	if err != nil {
		log.Errorf("poracle-test: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Parse remaining args
	remaining := args[1:]
	parsed := ctx.ArgMatcher.Match(remaining, poracleTestParams, ctx.Language)

	template := ctx.DefaultTemplate()
	explicitTemplate := false
	if t, ok := parsed.Strings["template"]; ok {
		template = t
		explicitTemplate = true
	}

	language := ctx.Language
	if l, ok := parsed.Strings["language"]; ok {
		language = l
	}

	// Test ID is the first unrecognized arg (if any)
	testID := ""
	if len(parsed.Unrecognized) > 0 {
		testID = parsed.Unrecognized[0]
	}

	// If no test ID, list available tests for this hook type
	if testID == "" {
		var msg strings.Builder
		msg.WriteString(tr.Tf("msg.poracle_test.tests_found", hookType) + "\n\n")
		for _, entry := range testdata {
			if entry.Type == hookType {
				msg.WriteString("  " + entry.Test + "\n")
			}
		}
		return []bot.Reply{{Text: msg.String()}}
	}

	// Find the test data item
	var dataItem *testdataEntry
	for i := range testdata {
		if testdata[i].Type == hookType && testdata[i].Test == testID {
			dataItem = &testdata[i]
			break
		}
	}
	if dataItem == nil {
		return []bot.Reply{{Text: tr.Tf("msg.poracle_test.not_found", hookType, testID)}}
	}

	// Validate explicit template exists (after finding test data so we can
	// resolve the actual DTS type: pokestop→lure/invasion, raid→egg/raid).
	// Unlike tracking commands where admins get a warning, test commands
	// always block on missing templates — no point sending a test that can't render.
	if explicitTemplate && template != "" && ctx.DTS != nil {
		dtsType := resolveDTSType(hookType, dataItem.Webhook)
		platform := targetDTSPlatform(ctx)
		if !ctx.DTS.Exists(dtsType, platform, template, ctx.Language) {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("tracking.template_not_found", template)}}
		}
	}

	// Look up user location
	human, _ := ctx.Humans.Get(ctx.TargetID)
	var humanLat, humanLon float64
	if human != nil {
		humanLat = human.Latitude
		humanLon = human.Longitude
		if human.Language != "" && language == ctx.Language {
			language = human.Language
		}
	}

	// Deep copy the webhook so we don't mutate the loaded testdata
	hook := make(map[string]any)
	maps.Copy(hook, dataItem.Webhook)

	// Move location to user's location (unless location: "keep")
	if dataItem.Location != "keep" {
		if _, ok := hook["latitude"]; ok {
			hook["latitude"] = humanLat
		}
		if _, ok := hook["longitude"]; ok {
			hook["longitude"] = humanLon
		}
	}

	// Freshen timestamps
	nowSecs := time.Now().Unix()
	switch hookType {
	case "pokemon":
		hook["disappear_time"] = nowSecs + 10*60
	case "raid":
		start := nowSecs + 10*60
		hook["start"] = start
		hook["end"] = start + 30*60
	case "rsvp_changes":
		start := nowSecs + 10*60
		hook["start"] = start
		hook["end"] = start + 30*60
		// Deep-copy the rsvps array before mutating — hook is only a
		// shallow copy of dataItem.Webhook (maps.Copy doesn't recurse), so
		// hook["rsvps"] is still the SAME nested slice/maps as the loaded
		// testdata entry; mutating in place would corrupt the shared
		// bundled/user sample for subsequent invocations (same rationale as
		// the fort_update/monster_changed cases below). Each timeslot is
		// bumped to a distinct near-future point (+5min, +15min, ...) so the
		// rendered rsvpChanges preview shows a live-looking RSVP window
		// instead of the canned sample's fixed far-future timestamp.
		if rsvps, ok := hook["rsvps"].([]any); ok {
			newRsvps := make([]any, len(rsvps))
			for i, r := range rsvps {
				rm, ok := r.(map[string]any)
				if !ok {
					newRsvps[i] = r
					continue
				}
				newR := make(map[string]any, len(rm))
				maps.Copy(newR, rm)
				newR["timeslot"] = (nowSecs + int64(5+i*10)*60) * 1000
				newRsvps[i] = newR
			}
			hook["rsvps"] = newRsvps
		}
	case "pokestop", "incident":
		if _, ok := hook["incident_expiration"]; ok {
			hook["incident_expiration"] = nowSecs + 10*60
		}
		if _, ok := hook["incident_expire_timestamp"]; ok {
			hook["incident_expire_timestamp"] = nowSecs + 10*60
		}
		if _, ok := hook["lure_expiration"]; ok {
			hook["lure_expiration"] = nowSecs + 5*60
		}
	case "fort_update":
		// Deep copy old/new location objects
		if oldObj, ok := hook["old"].(map[string]any); ok {
			newOld := make(map[string]any)
			maps.Copy(newOld, oldObj)
			if loc, ok := newOld["location"].(map[string]any); ok {
				newLoc := make(map[string]any)
				maps.Copy(newLoc, loc)
				newLoc["lat"] = humanLat
				newLoc["lon"] = humanLon
				newOld["location"] = newLoc
			}
			hook["old"] = newOld
		}
		if newObj, ok := hook["new"].(map[string]any); ok {
			newNew := make(map[string]any)
			maps.Copy(newNew, newObj)
			if loc, ok := newNew["location"].(map[string]any); ok {
				newLoc := make(map[string]any)
				maps.Copy(newLoc, loc)
				newLoc["lat"] = humanLat + 0.001
				newLoc["lon"] = humanLon + 0.001
				newNew["location"] = newLoc
			}
			hook["new"] = newNew
		}
	case "max_battle":
		battleStart := nowSecs - 1*60
		hook["battle_start"] = battleStart
		hook["start_time"] = battleStart
		battleEnd := nowSecs + 120*60
		hook["battle_end"] = battleEnd
		hook["end_time"] = battleEnd
	case "quest", "gym", "nest", "quest_summary":
		// No timestamp freshening needed
	case "monster_changed":
		// Only the `new` sighting's disappear_time needs freshening so the
		// preview doesn't render as already-expired; `old` is a fixed prior
		// point in time with no timestamp templates read (BuildOriginalView
		// never surfaces DisappearTime). Deep-copy `new` before mutating —
		// hook is only a shallow copy of dataItem.Webhook (maps.Copy doesn't
		// recurse), so hook["new"] is still the SAME nested map as the
		// loaded testdata entry; mutating it in place would corrupt the
		// shared bundled/user sample for subsequent invocations (same
		// rationale as the fort_update case above).
		if newObj, ok := hook["new"].(map[string]any); ok {
			newNew := make(map[string]any, len(newObj))
			maps.Copy(newNew, newObj)
			if _, ok := newNew["disappear_time"]; ok {
				newNew["disappear_time"] = nowSecs + 10*60
			}
			hook["new"] = newNew
		}
	case "weatherchange":
		// The cell's own gameplay_condition/old_gameplay_condition carry no
		// timestamp, but each affected-pokemon entry's disappear_time is a
		// canned sample value — freshen it the same way "pokemon" freshens
		// disappear_time, so clean-alert TTH computation doesn't see an
		// already-past despawn.
		if affected, ok := hook["affected"].([]any); ok {
			for i, a := range affected {
				am, ok := a.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := am["disappear_time"]; ok {
					am["disappear_time"] = nowSecs + 10*60 + int64(i)*60
				}
			}
		}
	}

	// Marshal webhook for the ProcessTest call
	webhookJSON, err := json.Marshal(hook)
	if err != nil {
		log.Errorf("poracle-test: marshal webhook: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if ctx.TestProcessor == nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.poracle_test.failed", "test processor not available")}}
	}

	target := bot.TestTarget{
		ID:        ctx.TargetID,
		Name:      ctx.TargetName,
		Type:      ctx.TargetType,
		Language:  language,
		Template:  template,
		Latitude:  humanLat,
		Longitude: humanLon,
	}

	if err := ctx.TestProcessor.ProcessTest(dataItem.Type, json.RawMessage(webhookJSON), target); err != nil {
		log.Errorf("poracle-test: %v", err)
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.poracle_test.failed", err.Error())}}
	}

	displayID := testID
	return []bot.Reply{
		{React: "✅", Text: tr.Tf("msg.poracle_test.queued", hookType, displayID, template)},
	}
}

// resolveHookType maps a !poracle-test leading type token to the wire
// dispatch type used for testdata lookup (loadTestdata's entries are keyed
// by wire type — "pokemon", "raid", "max_battle", ...), the timestamp
// freshening switch in Run, and resolveDTSType.
//
// The token has already been lowercased (and, for unquoted tokens,
// underscore-normalized) by the parser's tokenize step before Run ever sees
// it (see internal/bot/parser.go) — a real "monsterChanged" arrives here as
// "monsterchanged", never in its original camelCase.
//
// Resolution order:
//  1. validHooks — the existing raw webhook-type spellings (pokemon, raid,
//     pokestop, ...) and CLI-display hyphenated derived-type names
//     (monster-changed, max-battle, ...), matched as-is (case already
//     lowered by the parser, so this is an exact-string check).
//  2. The shared dtsmap alias table, via AliasFold since the token's case
//     information is already gone — this is what lets a DTS template-type
//     name (monster, maxbattle, egg, monsterchanged, ...) resolve too.
//
// dtsmap deliberately has no "pokestop" entry (see dtsmap's doc comment), so
// a bare "pokestop" token is only ever matched by validHooks, never by the
// fallback. "invasion" and "lure" DO have their own dtsmap entries whose
// WebhookType is "pokestop" — unlike enrichForType's identically-motivated
// "pokestop" rewrite guard (cmd/processor/enrich.go), which keeps them
// dispatching under their own name because ITS switch has direct
// "invasion"/"lure" cases, here the resolved WebhookType IS what testdata
// lookup needs: fallbacks/testdata.json has no "invasion"- or "lure"-typed
// entries, only "pokestop"-typed ones disambiguated by payload shape
// (resolveDTSType's "pokestop" case) — exactly like a literal "pokestop"
// token. Resolving straight to "pokestop" is therefore correct here, not a
// bug: it lets "!poracle-test invasion ..."/"!poracle-test lure ..." find
// the same testdata bucket a literal "!poracle-test pokestop ..." would.
func resolveHookType(token string) (string, bool) {
	if slices.Contains(validHooks, token) {
		return strings.ReplaceAll(token, "-", "_"), true
	}
	if src, ok := dtsmap.AliasFold(token); ok {
		return src.WebhookType, true
	}
	return "", false
}

// resolveDTSType determines the DTS template type from the webhook type and data.
// Some types branch based on the webhook content (pokestop→lure/invasion, raid→egg/raid).
func resolveDTSType(hookType string, webhook map[string]any) string {
	switch hookType {
	case "pokemon":
		return "monster"
	case "raid":
		// If pokemon_id is present and > 0, it's a raid boss; otherwise it's an egg
		if pid, ok := webhook["pokemon_id"]; ok {
			if id, _ := pid.(float64); id > 0 {
				return "raid"
			}
		}
		return "egg"
	case "pokestop":
		// If lure_expiration is present and > 0, it's a lure; otherwise invasion
		if lure, ok := webhook["lure_expiration"]; ok {
			if exp, _ := lure.(float64); exp > 0 {
				return "lure"
			}
		}
		return "invasion"
	case "fort_update":
		return "fort-update"
	case "max_battle":
		return "maxbattle"
	case "showcase":
		return "showcase"
	case "quest_summary":
		return "questSummary"
	case "monster_changed":
		return "monsterChanged"
	case "rsvp_changes":
		return "rsvpChanges"
	default:
		return hookType // quest, gym, nest, egg, invasion, lure — match 1:1
	}
}
