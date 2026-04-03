package dts

import "testing"

func TestEachIsLast(t *testing.T) {
	ctx := map[string]interface{}{
		"items": []string{"a", "b", "c"},
	}
	got := render(t, `{{#each items}}{{this}}{{#unless isLast}}, {{/unless}}{{/each}}`, ctx)
	expected := "a, b, c"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestEachIsFirst(t *testing.T) {
	ctx := map[string]interface{}{
		"items": []string{"a", "b", "c"},
	}
	got := render(t, `{{#each items}}{{#unless isFirst}}, {{/unless}}{{this}}{{/each}}`, ctx)
	expected := "a, b, c"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestEachIsLastWithMaps(t *testing.T) {
	ctx := map[string]interface{}{
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
	ctx := map[string]interface{}{
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
	ctx := map[string]interface{}{
		"items": []string{"only"},
	}
	got := render(t, `{{#each items}}{{this}}{{#unless isLast}}, {{/unless}}{{/each}}`, ctx)
	expected := "only"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}
