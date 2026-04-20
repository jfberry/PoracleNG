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
func loadTestdata(baseDir string) ([]testdataEntry, error) {
	bundledPath := filepath.Join(baseDir, "fallbacks", "testdata.json")
	userPath := filepath.Join(baseDir, "config", "testdata.json")

	var result []testdataEntry

	if data, err := os.ReadFile(bundledPath); err == nil {
		var entries []testdataEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("parse %s: %w", bundledPath, err)
		}
		result = append(result, entries...)
	}

	if data, err := os.ReadFile(userPath); err == nil {
		var entries []testdataEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			log.Warnf("poracle-test: failed to parse %s: %v", userPath, err)
		} else {
			result = append(result, entries...)
		}
	}

	return result, nil
}

func (c *PoracleTestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	tr := ctx.Tr()
	validHooks := []string{"pokemon", "raid", "pokestop", "gym", "nest", "quest", "fort-update", "max-battle"}

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.Tf("msg.poracle_test.usage", strings.Join(validHooks, ", "))}}
	}

	hookTypeDisplay := args[0]
	valid := slices.Contains(validHooks, hookTypeDisplay)
	if !valid {
		return []bot.Reply{{Text: tr.Tf("msg.poracle_test.usage", strings.Join(validHooks, ", "))}}
	}

	hookType := strings.ReplaceAll(hookTypeDisplay, "-", "_")

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
	case "pokestop":
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
	case "quest", "gym", "nest":
		// No timestamp freshening needed
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
	default:
		return hookType // quest, gym, nest, egg, invasion, lure — match 1:1
	}
}
