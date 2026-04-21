package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// helpTestDTS loads the real shipped fallbacks so help tests render the
// actual files we ship, not a toy template.
func helpTestDTS(t *testing.T) *dts.TemplateStore {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// processor/internal/bot/commands/help_test.go → repo root is 4 up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	ts, err := dts.LoadTemplates(t.TempDir(), filepath.Join(repoRoot, "fallbacks"))
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return ts
}

// TestHelpAdminTopicsGated — a non-admin asking for an admin-only help
// topic gets the "unknown topic" reply, not the admin command syntax.
// Admin users see the real help.
func TestHelpAdminTopicsGated(t *testing.T) {
	ts := helpTestDTS(t)

	cases := []string{"enable", "disable", "broadcast", "userlist", "community"}

	for _, topic := range cases {
		t.Run(topic+"/non-admin", func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = false
			ctx.DTS = ts
			cmd := &HelpCommand{}
			replies := cmd.Run(ctx, []string{topic})
			if len(replies) == 0 {
				t.Fatal("expected at least one reply")
			}
			if replies[0].React != "🙅" {
				t.Errorf("non-admin asking for !help %s should get the 🙅 unknown-topic reply, got react=%q text=%q", topic, replies[0].React, replies[0].Text)
			}
			if replies[0].Embed != nil {
				t.Error("non-admin should never see the admin help embed")
			}
		})

		t.Run(topic+"/admin", func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = true
			ctx.DTS = ts
			cmd := &HelpCommand{}
			replies := cmd.Run(ctx, []string{topic})
			if len(replies) == 0 {
				t.Fatalf("admin asking for !help %s should get the admin help, got no reply", topic)
			}
			// Discord path returns Embed; Telegram would return Text. Either
			// way the non-empty payload proves the lookup went through.
			if replies[0].Embed == nil && replies[0].Text == "" {
				t.Errorf("admin asking for !help %s got an empty reply", topic)
			}
		})
	}
}

// TestHelpIndexHidesAdminSectionForNonAdmins — the user-facing index
// shouldn't leak the admin-commands field to non-admin viewers. We
// render the index for both isAdmin=true and isAdmin=false and assert
// the admin field is present only in the former.
func TestHelpIndexHidesAdminSectionForNonAdmins(t *testing.T) {
	ts := helpTestDTS(t)

	render := func(isAdmin bool) string {
		ctx, _ := testCtx(t)
		ctx.IsAdmin = isAdmin
		ctx.DTS = ts
		cmd := &HelpCommand{}
		replies := cmd.Run(ctx, nil) // no args → index
		if len(replies) == 0 {
			t.Fatalf("expected a reply (isAdmin=%v)", isAdmin)
		}
		// Discord returns Embed JSON; flatten to string for substring search.
		if replies[0].Embed != nil {
			return string(replies[0].Embed)
		}
		var sb strings.Builder
		for _, r := range replies {
			sb.WriteString(r.Text)
		}
		return sb.String()
	}

	adminView := render(true)
	userView := render(false)

	if !strings.Contains(adminView, "Admin commands") {
		t.Errorf("admin view should contain \"Admin commands\" section, got:\n%s", adminView)
	}
	if strings.Contains(userView, "Admin commands") {
		t.Errorf("non-admin view should NOT contain \"Admin commands\" section, got:\n%s", userView)
	}
	// Basic sanity — both views should carry the tracking commands section.
	for _, v := range []string{adminView, userView} {
		if !strings.Contains(v, "Tracking commands") {
			t.Errorf("index view missing tracking commands section:\n%s", v)
		}
	}
}

// TestHelpPicksUserCustomWhenCallerHasEmptyLanguage simulates the
// reported end-user scenario: operator has a custom help entry shaped
// {"type":"help","id":"1","default":true,"language":"en","platform":"discord"},
// and a caller whose ctx.Language is "" (unregistered user, or user
// who never ran !language). Before the language fallback, the lookup
// passed language="" which fails to match entry-language="en" at
// priority levels 3 and 4, so pass-1 (user) returned nil and pass-2
// (readonly) picked the shipped help/index. After the fix the effective
// language resolves to the server default locale, so the user's entry
// matches at level 3 via the default flag.
func TestHelpPicksUserCustomWhenCallerHasEmptyLanguage(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	fallbackDir := filepath.Join(repoRoot, "fallbacks")

	configDir := t.TempDir()
	userHelp := `[{
		"type": "help",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {
			"embed": {
				"title": "Custom Operator Help",
				"fields": [{"name": "Area tracking", "value": "area list"}]
			}
		}
	}]`
	if err := os.WriteFile(filepath.Join(configDir, "dts.json"), []byte(userHelp), 0644); err != nil {
		t.Fatal(err)
	}

	ts, err := dts.LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	ctx, _ := testCtx(t)
	ctx.DTS = ts
	ctx.Language = "" // unregistered / never-ran-!language user
	ctx.Config = &config.Config{}
	ctx.Config.General.Locale = "en"

	cmd := &HelpCommand{}
	replies := cmd.Run(ctx, nil)
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}

	// Flatten the reply to a string to search for the distinctive title.
	var out strings.Builder
	if replies[0].Embed != nil {
		out.Write(replies[0].Embed)
	}
	for _, r := range replies {
		out.WriteString(r.Text)
	}

	// The help renderer blanks title/description intentionally, so we
	// search for the custom FIELD name — unique enough to distinguish
	// from the shipped help/index.
	if !strings.Contains(out.String(), "Area tracking") {
		t.Errorf("expected user's custom help fields to be rendered, got:\n%s", out.String())
	}
	// Sanity: make sure we didn't accidentally render the shipped index,
	// which has distinctive field name "Tracking commands".
	if strings.Contains(out.String(), "Tracking commands") {
		t.Errorf("user's custom help was shadowed by shipped help/index:\n%s", out.String())
	}
}
