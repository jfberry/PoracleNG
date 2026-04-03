package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// PoracleTestCommand implements !poracle-test — send a test webhook.
type PoracleTestCommand struct{}

func (c *PoracleTestCommand) Name() string     { return "cmd.poracle_test" }
func (c *PoracleTestCommand) Aliases() []string { return nil }

var poracleTestParams = []bot.ParamDef{
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.language"},
}

// testdataEntry represents one item from testdata.json.
type testdataEntry struct {
	Type     string                 `json:"type"`
	Test     string                 `json:"test"`
	Location string                 `json:"location,omitempty"`
	Webhook  map[string]interface{} `json:"webhook"`
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
	valid := false
	for _, v := range validHooks {
		if v == hookTypeDisplay {
			valid = true
			break
		}
	}
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
	if t, ok := parsed.Strings["template"]; ok {
		template = t
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
		msg := tr.Tf("msg.poracle_test.tests_found", hookType) + "\n\n"
		for _, entry := range testdata {
			if entry.Type == hookType {
				msg += "  " + entry.Test + "\n"
			}
		}
		return []bot.Reply{{Text: msg}}
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
	hook := make(map[string]interface{})
	for k, v := range dataItem.Webhook {
		hook[k] = v
	}

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
		if oldObj, ok := hook["old"].(map[string]interface{}); ok {
			newOld := make(map[string]interface{})
			for k, v := range oldObj {
				newOld[k] = v
			}
			if loc, ok := newOld["location"].(map[string]interface{}); ok {
				newLoc := make(map[string]interface{})
				for k, v := range loc {
					newLoc[k] = v
				}
				newLoc["lat"] = humanLat
				newLoc["lon"] = humanLon
				newOld["location"] = newLoc
			}
			hook["old"] = newOld
		}
		if newObj, ok := hook["new"].(map[string]interface{}); ok {
			newNew := make(map[string]interface{})
			for k, v := range newObj {
				newNew[k] = v
			}
			if loc, ok := newNew["location"].(map[string]interface{}); ok {
				newLoc := make(map[string]interface{})
				for k, v := range loc {
					newLoc[k] = v
				}
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

	// Marshal webhook for the API request
	webhookJSON, err := json.Marshal(hook)
	if err != nil {
		log.Errorf("poracle-test: marshal webhook: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Build the API request
	testReq := map[string]interface{}{
		"type":    dataItem.Type,
		"webhook": json.RawMessage(webhookJSON),
		"target": map[string]interface{}{
			"id":        ctx.TargetID,
			"name":      ctx.TargetName,
			"type":      ctx.TargetType,
			"language":  language,
			"template":  template,
			"latitude":  humanLat,
			"longitude": humanLon,
		},
	}

	reqBody, err := json.Marshal(testReq)
	if err != nil {
		log.Errorf("poracle-test: marshal request: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Reply first with queued message
	displayID := testID
	reply := bot.Reply{
		React: "✅",
		Text:  tr.Tf("msg.poracle_test.queued", hookType, displayID, template),
	}

	// POST to the processor's own /api/test endpoint
	processorURL := fmt.Sprintf("http://localhost:%d", ctx.Config.Processor.Port)
	url := processorURL + "/api/test"

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if ctx.Config.Alerter.APISecret != "" {
		req.Header.Set("X-Poracle-Secret", ctx.Config.Alerter.APISecret)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("poracle-test: request: %v", err)
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.poracle_test.failed", err.Error())}}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Errorf("poracle-test: status %d: %s", resp.StatusCode, string(body))
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.poracle_test.failed", fmt.Sprintf("HTTP %d", resp.StatusCode))}}
	}

	return []bot.Reply{reply}
}
