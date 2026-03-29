package dts

import (
	"fmt"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
)

func TestCompareIntDebug(t *testing.T) {
	RegisterHelpers()

	tpl := `{{#compare chance '==' 100}}YES{{else}}NO{{/compare}}`
	compiled, _ := raymond.Parse(tpl)

	// Test with Go int (what enrichment produces)
	result, _ := compiled.Exec(map[string]any{"chance": 100})
	fmt.Printf("int 100 == 100: %s\n", result)

	// Test with float64 (what JSON unmarshal produces)
	result2, _ := compiled.Exec(map[string]any{"chance": float64(100)})
	fmt.Printf("float64 100 == 100: %s\n", result2)

	// Test nested access
	tpl2 := `{{#compare data.first.chance '==' 100}}YES{{else}}NO{{/compare}}`
	compiled2, _ := raymond.Parse(tpl2)
	result3, _ := compiled2.Exec(map[string]any{
		"data": map[string]any{
			"first": map[string]any{
				"chance": 100,
			},
		},
	})
	fmt.Printf("nested int 100 == 100: %s\n", result3)
}
