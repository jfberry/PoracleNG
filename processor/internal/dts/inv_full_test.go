package dts

import (
	"encoding/json"
	"fmt"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
)

func TestInvasionFullTemplateDebug(t *testing.T) {
	RegisterHelpers()

	// The actual DTS template (simplified)
	tpl := `{"embed":{"title":"Team Rocket at {{pokestopName}}","description":"Type: {{gruntType}} {{gruntTypeEmoji}}\nGender: {{genderData.name}}{{genderData.emoji}}\nRewards: {{#compare gruntRewardsList.first.chance '==' 100}}{{#forEach gruntRewardsList.first.monsters}}{{this.name}}{{#unless isLast}}, {{/unless}}{{/forEach}}{{/compare}}"}}`

	compiled, err := raymond.Parse(tpl)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Simulate the view after BuildPokemonView merges base + perLang
	view := map[string]any{
		"pokestopName":   "The Bridge Arms",
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
					{"name": "Magnemite"},
					{"name": "Voltorb"},
					{"name": "Electrode"},
				},
			},
		},
	}

	result, err := compiled.Exec(view)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	fmt.Printf("Raw result:\n%s\n\n", result)

	var msg map[string]any
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		t.Fatalf("json parse: %v\nraw: %s", err, result)
	}
	fmt.Printf("Parsed message:\n%+v\n", msg)
}
