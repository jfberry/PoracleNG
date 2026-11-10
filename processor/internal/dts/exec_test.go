package dts

import (
	"strings"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
)

// Help and greeting templates were executed with plain Exec: no data frame
// (so language-aware helpers always used English) and no panic recovery
// (so a bad template could crash the process) (#273).
func TestExecTemplateSetsLanguageAndPlatform(t *testing.T) {
	raymond.RegisterHelper("testFrame", func(options *raymond.Options) any {
		return getLang(options) + "/" + getPlatform(options)
	})
	defer raymond.RemoveHelper("testFrame")

	tmpl, err := raymond.Parse(`{{testFrame}}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExecTemplate(tmpl, map[string]any{}, "de", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if got != "de/telegram" {
		t.Errorf("got %q, want de/telegram", got)
	}
}

func TestExecTemplateRecoversPanics(t *testing.T) {
	raymond.RegisterHelper("testPanic", func(a, b any, options *raymond.Options) any { return nil })
	defer raymond.RemoveHelper("testPanic")

	tmpl, err := raymond.Parse(`{{testPanic 1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecTemplate(tmpl, map[string]any{}, "en", "discord"); err == nil || !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected a recovered panic error, got %v", err)
	}
}
