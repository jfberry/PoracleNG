package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// testCtx builds a minimal CommandContext for unit testing commands.
// It provides a MockHumanStore, translations, and config.
// The caller can override fields after creation.
func testCtx(t *testing.T) (*bot.CommandContext, *store.MockHumanStore) {
	t.Helper()

	humans := store.NewMockHumanStore()

	// Seed a default user
	humans.AddHuman(&store.Human{
		ID:               "user1",
		Type:             bot.TypeDiscordUser,
		Name:             "TestUser",
		Enabled:          true,
		CurrentProfileNo: 1,
		Language:         "en",
	})

	bundle := i18n.Load("")

	reloaded := false
	ctx := &bot.CommandContext{
		UserID:       "user1",
		UserName:     "TestUser",
		Platform:     "discord",
		ChannelID:    "ch1",
		IsDM:         true,
		Language:      "en",
		ProfileNo:    1,
		TargetID:     "user1",
		TargetName:   "TestUser",
		TargetType:   bot.TypeDiscordUser,
		Humans:       humans,
		Translations: bundle,
		Config:       &config.Config{},
		ReloadFunc:   func() { reloaded = true },
	}

	// Store reloaded flag for assertions
	t.Cleanup(func() {
		_ = reloaded // accessible via wasReloaded helper
	})

	return ctx, humans
}

// assertReact checks the first reply has the expected react emoji.
func assertReact(t *testing.T, replies []bot.Reply, expected string) {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if replies[0].React != expected {
		t.Errorf("expected react %q, got %q (text: %q)", expected, replies[0].React, replies[0].Text)
	}
}

// assertTextContains checks the first reply text contains the substring.
func assertTextContains(t *testing.T, replies []bot.Reply, substr string) {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if !containsStr(replies[0].Text, substr) {
		t.Errorf("expected reply text to contain %q, got %q", substr, replies[0].Text)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || findStr(s, substr))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// assertCall checks that a specific method was called on the mock store.
func assertCall(t *testing.T, mock *store.MockHumanStore, method string) {
	t.Helper()
	for _, c := range mock.Calls {
		if c == method {
			return
		}
	}
	t.Errorf("expected store method %q to be called, calls were: %v", method, mock.Calls)
}

// assertNoCall checks that a specific method was NOT called.
func assertNoCall(t *testing.T, mock *store.MockHumanStore, method string) {
	t.Helper()
	for _, c := range mock.Calls {
		if c == method {
			t.Errorf("expected store method %q NOT to be called, but it was", method)
			return
		}
	}
}
