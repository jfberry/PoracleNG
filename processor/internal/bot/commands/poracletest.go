package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

func (c *PoracleTestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	validHooks := []string{"pokemon", "raid", "pokestop", "gym", "nest", "quest", "fort-update", "max-battle"}

	if len(args) == 0 {
		return []bot.Reply{{Text: "Hooks supported are: " + strings.Join(validHooks, ", ")}}
	}

	hookType := args[0]
	valid := false
	for _, v := range validHooks {
		if v == hookType {
			valid = true
			break
		}
	}
	if !valid {
		return []bot.Reply{{Text: "Hooks supported are: " + strings.Join(validHooks, ", ")}}
	}

	// Parse remaining args
	remaining := args[1:]
	parsed := ctx.ArgMatcher.Match(remaining, poracleTestParams, ctx.Language)

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}

	// Test ID is the first unrecognized arg
	testID := ""
	if len(parsed.Unrecognized) > 0 {
		testID = parsed.Unrecognized[0]
	}

	// Look up user location for the test
	var human struct {
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Language  *string `db:"language"`
	}
	ctx.DB.Get(&human, "SELECT latitude, longitude, language FROM humans WHERE id = ? LIMIT 1", ctx.TargetID)

	language := ctx.Language
	if human.Language != nil && *human.Language != "" {
		language = *human.Language
	}

	// POST to the processor's own /api/test endpoint
	testReq := map[string]any{
		"type":   strings.ReplaceAll(hookType, "-", "_"),
		"testId": testID,
		"target": map[string]any{
			"id":        ctx.TargetID,
			"name":      ctx.TargetName,
			"type":      ctx.TargetType,
			"language":  language,
			"template":  template,
			"latitude":  human.Latitude,
			"longitude": human.Longitude,
		},
	}

	reqBody, err := json.Marshal(testReq)
	if err != nil {
		log.Errorf("poracle-test: marshal: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Call the test endpoint on ourselves
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
		return []bot.Reply{{React: "🙅", Text: "Test failed: " + err.Error()}}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Errorf("poracle-test: status %d: %s", resp.StatusCode, string(body))
		return []bot.Reply{{React: "🙅", Text: fmt.Sprintf("Test failed: %d", resp.StatusCode)}}
	}

	displayID := testID
	if displayID == "" {
		displayID = "default"
	}
	return []bot.Reply{{React: "✅", Text: fmt.Sprintf("Queueing %s test hook [%s] template [%s]", hookType, displayID, template)}}
}
