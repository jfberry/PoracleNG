package dts

import "testing"

// Reconciliation greetings used to look the template up with language ""
// even though they render in [general] locale, so a locale-specific default
// greeting was only reached by the last-resort step. Greeting must select the
// entry for the requested language (#315).
func TestGreeting_SelectsRequestedLanguage(t *testing.T) {
	ts, _ := newTestStore(t, []DTSEntry{
		{Type: "greeting", ID: "1", Platform: "discord", Language: "de", Default: true, Template: "hallo"},
		{Type: "greeting", ID: "1", Platform: "discord", Language: "fr", Default: true, Template: "bonjour"},
	})
	tmpl := ts.Greeting("discord", "de")
	if tmpl == nil {
		t.Fatal("no greeting template found")
	}
	out, err := ExecTemplate(tmpl, map[string]any{}, "de", "discord")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hallo" {
		t.Errorf("greeting = %q, want the de entry", out)
	}
	if ts.Greeting("telegram", "de") != nil {
		t.Error("telegram has no greeting; want nil")
	}
}
