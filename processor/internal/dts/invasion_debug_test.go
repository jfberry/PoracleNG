package dts

import (
	"encoding/json"
	"fmt"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
)

func TestInvasionTemplateDebug(t *testing.T) {
	// Simulate what the invasion DTS template accesses
	tpl := `Type: {{gruntType}} {{gruntTypeEmoji}}
Gender: {{genderData.name}}{{genderData.emoji}}
Rewards: {{#compare gruntRewardsList.first.chance '==' 100}}{{#forEach gruntRewardsList.first.monsters}}{{this.name}}{{#unless isLast}}, {{/unless}}{{/forEach}}{{/compare}}`

	RegisterHelpers()

	compiled, err := raymond.Parse(tpl)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	view := map[string]any{
		"gruntType":      "Electric",
		"gruntTypeEmoji": "⚡",
		"genderData": map[string]any{
			"name":  "female",
			"emoji": "♀",
		},
		"gruntRewardsList": map[string]any{
			"first": map[string]any{
				"chance": 100,
				"monsters": []map[string]any{
					{"name": "Magnemite", "fullName": "Magnemite"},
					{"name": "Voltorb", "fullName": "Voltorb"},
				},
			},
		},
	}

	result, err := compiled.Exec(view)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	fmt.Printf("Result:\n%s\n", result)

	// Also test with JSON round-trip (how view builder produces the data)
	jsonBytes, _ := json.Marshal(view)
	var roundTripped map[string]any
	json.Unmarshal(jsonBytes, &roundTripped)

	result2, err := compiled.Exec(roundTripped)
	if err != nil {
		t.Fatalf("exec round-tripped: %v", err)
	}
	fmt.Printf("\nRound-tripped result:\n%s\n", result2)
}
