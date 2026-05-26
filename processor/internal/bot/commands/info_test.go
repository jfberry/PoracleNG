package commands

import (
	"strings"
	"testing"
)

// TestInfo_Poracle_Redirects verifies that !info poracle returns the
// "moved to !poracle-admin status" redirect message for any user (no admin gate).
func TestInfo_Poracle_Redirects(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(adminLabel(admin), func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = admin

			cmd := &InfoCommand{}
			replies := cmd.Run(ctx, []string{"poracle"})

			if len(replies) == 0 {
				t.Fatal("expected at least one reply, got none")
			}
			text := replies[0].Text
			if !strings.Contains(text, "poracle-admin status") {
				t.Errorf("expected redirect to poracle-admin status, got: %q", text)
			}
		})
	}
}

// TestInfo_Config_Redirects verifies that !info config returns the
// "moved to !poracle-admin config" redirect message for any user (no admin gate).
func TestInfo_Config_Redirects(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(adminLabel(admin), func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = admin

			cmd := &InfoCommand{}
			replies := cmd.Run(ctx, []string{"config"})

			if len(replies) == 0 {
				t.Fatal("expected at least one reply, got none")
			}
			text := replies[0].Text
			if !strings.Contains(text, "poracle-admin config") {
				t.Errorf("expected redirect to poracle-admin config, got: %q", text)
			}
		})
	}
}

// TestInfo_Poracle_NoReact verifies that !info poracle does NOT return a
// 🙅 react for non-admins (the admin gate has been removed).
func TestInfo_Poracle_NoReact(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = false

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"poracle"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if replies[0].React == "🙅" {
		t.Error("non-admin should NOT receive 🙅 react for !info poracle — admin gate was removed")
	}
}

// TestInfo_Config_NoReact verifies that !info config does NOT return a
// 🙅 react for non-admins (the admin gate has been removed).
func TestInfo_Config_NoReact(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = false

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"config"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if replies[0].React == "🙅" {
		t.Error("non-admin should NOT receive 🙅 react for !info config — admin gate was removed")
	}
}

// TestInfo_HelpArgReturnsUsage verifies that "!info help" returns usage text
// rather than interpreting "help" as a pokemon name lookup.
func TestInfo_HelpArgReturnsUsage(t *testing.T) {
	ctx, _ := testCtx(t)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"help"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	// The usage message contains "info" as the command name and subcommand hints.
	if !strings.Contains(text, "info") {
		t.Errorf("expected usage text containing 'info', got: %q", text)
	}
	// Must NOT return the pokemon-not-found error for "help".
	if strings.Contains(text, "not found") || strings.Contains(text, "pokemon_not_found") {
		t.Errorf("!info help should return usage, not pokemon-not-found: %q", text)
	}
	// Must NOT have a block react
	if replies[0].React == "🙅" {
		t.Error("!info help should not return a 🙅 react")
	}
}

func adminLabel(admin bool) string {
	if admin {
		return "admin"
	}
	return "non-admin"
}
