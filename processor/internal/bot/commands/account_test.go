package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func accountCtx(t *testing.T, selfService, admin bool) (*bot.CommandContext, *store.MockPlayerAccountStore) {
	t.Helper()
	ctx, _ := testCtx(t)
	accts := store.NewMockPlayerAccountStore()
	ctx.PlayerAccounts = accts
	ctx.Config.PlayerAccounts.SelfService = selfService
	ctx.IsAdmin = admin
	ctx.UserID, ctx.TargetID, ctx.TargetType = "u1", "u1", "discord:user"
	ctx.ReloadFunc = func() {}
	return ctx, accts
}

func runAccount(ctx *bot.CommandContext, verbatim ...string) string {
	ctx.VerbatimArgs = verbatim
	lower := make([]string, len(verbatim))
	for i, v := range verbatim {
		lower[i] = strings.ReplaceAll(strings.ToLower(v), "_", " ")
	}
	var sb strings.Builder
	for _, r := range (&AccountCommand{}).Run(ctx, lower) {
		sb.WriteString(r.React + r.Text + "\n")
	}
	return sb.String()
}

func TestAccountSelfServiceAddKeepsIDVerbatim(t *testing.T) {
	ctx, accts := accountCtx(t, true, false)
	out := runAccount(ctx, "add", "User_ABC", "Main")
	if !strings.Contains(out, "✅") {
		t.Fatalf("add: %s", out)
	}
	list, _ := accts.List("u1")
	if len(list) != 1 || list[0].PlayerID != "User_ABC" || list[0].Label != "Main" {
		t.Fatalf("stored = %+v", list)
	}
	runAccount(ctx, "add", "username_9")
	list, _ = accts.List("u1")
	if len(list) != 2 || list[1].PlayerID != "username_9" {
		t.Fatalf("user-prefixed id = %+v", list)
	}
}

func TestAccountWritesNeedAdminWithoutSelfService(t *testing.T) {
	ctx, accts := accountCtx(t, false, false)
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("non-admin add allowed: %s", out)
	}
	if list, _ := accts.List("u1"); len(list) != 0 {
		t.Fatal("stored despite refusal")
	}
	if out := runAccount(ctx); strings.Contains(out, "🙅") {
		t.Fatalf("list refused: %s", out)
	}
	ctx.IsAdmin = true
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "✅") {
		t.Fatalf("admin add refused: %s", out)
	}
}

func TestAccountChannelTargetsAdminOnly(t *testing.T) {
	ctx, _ := accountCtx(t, true, false)
	ctx.TargetID, ctx.TargetType = "chan1", "discord:channel"
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("self-service user linked a channel account: %s", out)
	}
}

func TestAccountRemoveAndLabel(t *testing.T) {
	ctx, accts := accountCtx(t, true, false)
	runAccount(ctx, "add", "Abc_1", "main")
	runAccount(ctx, "add", "def")
	if out := runAccount(ctx, "label", "def", "alt"); !strings.Contains(out, "✅") {
		t.Fatalf("label: %s", out)
	}
	if out := runAccount(ctx, "label", "def", "MAIN"); !strings.Contains(out, "🙅") {
		t.Fatalf("duplicate label accepted: %s", out)
	}
	if out := runAccount(ctx, "remove", "Main"); !strings.Contains(out, "✅") {
		t.Fatalf("remove by label: %s", out)
	}
	if out := runAccount(ctx, "remove", "nope"); !strings.Contains(out, "🙅") {
		t.Fatalf("remove missing: %s", out)
	}
	list, _ := accts.List("u1")
	if len(list) != 1 || list[0].PlayerID != "def" || list[0].Label != "alt" {
		t.Fatalf("final = %+v", list)
	}
	out := runAccount(ctx, "list")
	if !strings.Contains(out, "alt") {
		t.Fatalf("list output: %s", out)
	}
}

// A delegated channel admin may manage the channel they are in, but not
// another channel or a webhook reached by a target override.
func TestAccountDelegatedChannelAdminOnlyCurrentChannel(t *testing.T) {
	ctx, accts := accountCtx(t, false, false)
	ctx.Permissions.ChannelTracking = true
	ctx.ChannelID = "chan1"

	ctx.TargetID, ctx.TargetType = "chan1", "discord:channel"
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "✅") {
		t.Fatalf("delegated admin refused on own channel: %s", out)
	}
	ctx.TargetID, ctx.TargetType = "chan2", "discord:channel"
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("delegated admin linked another channel: %s", out)
	}
	ctx.TargetID, ctx.TargetType = "https://discord.com/api/webhooks/1/x", "webhook"
	if out := runAccount(ctx, "add", "abc"); !strings.Contains(out, "🙅") {
		t.Fatalf("delegated channel admin linked a webhook: %s", out)
	}
	if list, _ := accts.List("chan2"); len(list) != 0 {
		t.Fatalf("stored on chan2: %+v", list)
	}
}

// !apply gives each sub-command its own verbatim args, so a player ID
// linked through apply keeps its case and underscores.
func TestApplyAccountKeepsVerbatimArgs(t *testing.T) {
	ctx, accts := accountCtx(t, false, true)
	humans := ctx.Humans.(*store.MockHumanStore)
	humans.AddHuman(&store.Human{ID: "u2", Type: bot.TypeDiscordUser, Name: "U2", Enabled: true, CurrentProfileNo: 1, Language: "en"})
	reg := bot.NewRegistry()
	reg.Register(&AccountCommand{})
	ctx.Registry = reg

	verbatim := []string{"u2", "|", "account", "add", "User_ABC", "Main"}
	ctx.VerbatimArgs = verbatim
	lower := []string{"u2", "|", "account", "add", "user abc", "main"}
	out := (&ApplyCommand{}).Run(ctx, lower)
	list, _ := accts.List("u2")
	if len(list) != 1 || list[0].PlayerID != "User_ABC" || list[0].Label != "Main" {
		t.Fatalf("stored = %+v; replies = %+v", list, out)
	}

	// Without usable verbatim args the sub-command still gets its own
	// (normalised) args, not apply's.
	ctx.VerbatimArgs = nil
	(&ApplyCommand{}).Run(ctx, []string{"u2", "|", "account", "add", "other"})
	if list, _ = accts.List("u2"); len(list) != 2 || list[1].PlayerID != "other" {
		t.Fatalf("fallback stored = %+v", list)
	}
}
