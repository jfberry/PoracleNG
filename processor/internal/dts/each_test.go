package dts

import "testing"

func TestEachIsLast(t *testing.T) {
	ctx := map[string]any{
		"items": []string{"a", "b", "c"},
	}
	got := render(t, `{{#each items}}{{this}}{{#unless isLast}}, {{/unless}}{{/each}}`, ctx)
	expected := "a, b, c"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestEachIsFirst(t *testing.T) {
	ctx := map[string]any{
		"items": []string{"a", "b", "c"},
	}
	got := render(t, `{{#each items}}{{#unless isFirst}}, {{/unless}}{{this}}{{/each}}`, ctx)
	expected := "a, b, c"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestEachIsLastWithMaps(t *testing.T) {
	ctx := map[string]any{
		"pvp": []map[string]any{
			{"name": "Pikachu", "rank": 1},
			{"name": "Eevee", "rank": 5},
			{"name": "Bulbasaur", "rank": 10},
		},
	}
	got := render(t, `{{#each pvp}}{{name}}{{#unless isLast}}, {{/unless}}{{/each}}`, ctx)
	expected := "Pikachu, Eevee, Bulbasaur"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestForEachIsLast(t *testing.T) {
	ctx := map[string]any{
		"items": []map[string]any{
			{"name": "Dratini"},
			{"name": "Bagon"},
			{"name": "Deino"},
		},
	}
	got := render(t, `{{#forEach items}}{{name}}{{#unless isLast}}, {{/unless}}{{/forEach}}`, ctx)
	expected := "Dratini, Bagon, Deino"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestEachSingleElement(t *testing.T) {
	ctx := map[string]any{
		"items": []string{"only"},
	}
	got := render(t, `{{#each items}}{{this}}{{#unless isLast}}, {{/unless}}{{/each}}`, ctx)
	expected := "only"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

// Scalar items are wrapped to provide isFirst/isLast; helpers and the builtin
// if/unless must still receive the item itself.
func TestEachScalarItemsAsHelperArgs(t *testing.T) {
	ctx := map[string]any{
		"nums":  []int{0, 1, 2},
		"strs":  []string{"", "a"},
		"lists": []any{[]any{1, 2}, []any{3}},
	}
	tests := []struct{ tmpl, want string }{
		{`{{#each nums}}{{#if (gt this 1)}}[{{this}}]{{/if}}{{/each}}`, "[2]"},
		{`{{#each nums}}{{gt this 0}},{{/each}}`, "false,true,true,"},
		{`{{#each nums}}{{#eq this 1}}one{{/eq}}{{/each}}`, "one"},
		{`{{#each nums}}{{add this 10}},{{/each}}`, "10,11,12,"},
		{`{{#each nums}}{{#if this}}T{{else}}F{{/if}}{{/each}}`, "FTT"},
		{`{{#each strs}}{{#unless this}}empty{{/unless}}{{/each}}`, "empty"},
		{`{{#each lists}}{{length this}},{{/each}}`, "2,1,"},
		{`{{#each lists}}{{join this '-'}},{{/each}}`, "1-2,3,"},
		// the wrapper still provides isFirst/isLast and renders the item
		{`{{#each nums}}{{this}}{{#unless isLast}}/{{/unless}}{{/each}}`, "0/1/2"},
	}
	for _, tc := range tests {
		if got := render(t, tc.tmpl, ctx); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}
