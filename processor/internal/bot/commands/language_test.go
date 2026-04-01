package commands

import (
	"testing"
)

func TestLanguageCommand_SetValid(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.Config.General.AvailableLanguages = []string{"en", "de", "fr"}

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, []string{"de"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetLanguage")

	h, _ := mock.Get("user1")
	if h.Language != "de" {
		t.Errorf("expected language de, got %s", h.Language)
	}
}

func TestLanguageCommand_Invalid(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.Config.General.AvailableLanguages = []string{"en", "de"}

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, []string{"xx"})

	assertReact(t, replies, "🙅")
	assertNoCall(t, mock, "SetLanguage")
}

func TestLanguageCommand_NoArgs(t *testing.T) {
	ctx, _ := testCtx(t)

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, nil)

	assertReact(t, replies, "🙅")
}

func TestLanguageCommand_CaseInsensitive(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.Config.General.AvailableLanguages = []string{"en", "DE"}

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, []string{"de"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetLanguage")
}
