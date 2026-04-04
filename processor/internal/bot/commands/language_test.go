package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func TestLanguageCommand_SetValid(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.Config.General.AvailableLanguages = map[string]config.LanguageEntry{
		"en": {Poracle: "poracle"},
		"de": {Poracle: "dasporacle"},
		"fr": {Poracle: "leporacle"},
	}

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
	ctx.Config.General.AvailableLanguages = map[string]config.LanguageEntry{
		"en": {Poracle: "poracle"},
		"de": {Poracle: "dasporacle"},
	}

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
	ctx.Config.General.AvailableLanguages = map[string]config.LanguageEntry{
		"en": {},
		"DE": {},
	}

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, []string{"de"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetLanguage")
}

func TestLanguageCommand_NoAvailable(t *testing.T) {
	ctx, mock := testCtx(t)
	// No available_languages configured — accept any language code

	cmd := &LanguageCommand{}
	replies := cmd.Run(ctx, []string{"ja"})

	assertReact(t, replies, "✅")
	assertCall(t, mock, "SetLanguage")
	h, _ := mock.Get("user1")
	if h.Language != "ja" {
		t.Errorf("expected language ja, got %s", h.Language)
	}
}
