package api

import "testing"

func hasFieldDef(fields []FieldDef, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// TestFieldsByType_ShowcaseVsIncident locks the editor field-surface split: the
// showcase type lists the showcase fields but NOT the incident-only aliases
// (incidentTypeName / incidentEmoji / color), which have no showcase alias and
// would render empty — the showcase template hardcodes its title/emoji/colour.
func TestFieldsByType_ShowcaseVsIncident(t *testing.T) {
	incident, ok := fieldsByType["incident"]
	if !ok {
		t.Fatal("incident type missing from fieldsByType")
	}
	showcase, ok := fieldsByType["showcase"]
	if !ok {
		t.Fatal("showcase type missing from fieldsByType")
	}

	for _, f := range []string{"incidentTypeName", "incidentEmoji", "color"} {
		if !hasFieldDef(incident.Fields, f) {
			t.Errorf("incident type should list %q", f)
		}
		if hasFieldDef(showcase.Fields, f) {
			t.Errorf("showcase type must NOT list %q — it does not resolve", f)
		}
	}

	for _, f := range []string{"pokestopName", "displayType", "showcasePresent", "showcaseFocusName", "showcaseFocusCategory"} {
		if !hasFieldDef(showcase.Fields, f) {
			t.Errorf("showcase type should list %q", f)
		}
	}

	foundFocus := false
	for _, s := range showcase.Snippets {
		if s.Label == "Showcase focus" {
			foundFocus = true
		}
	}
	if !foundFocus {
		t.Error("showcase type should include the 'Showcase focus' snippet")
	}
}
