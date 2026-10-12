package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func TestTrackPersonalKeywords(t *testing.T) {
	ctx := trackCtx(t)
	replies := runTrack(t, ctx, "25 personalshiny personalbackground")
	if len(replies) == 0 || replies[0].React == "🙅" {
		t.Fatalf("replies = %+v", replies)
	}
	rows, _ := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if len(rows) != 1 || rows[0].Personal != db.PersonalShiny|db.PersonalBackground {
		t.Fatalf("rows = %+v", rows)
	}
	// No linked accounts: created, with a warning.
	var all strings.Builder
	for _, r := range replies {
		all.WriteString(r.Text)
	}
	if !strings.Contains(all.String(), "account add") {
		t.Fatalf("missing no-accounts warning: %q", all.String())
	}
}

func TestTrackPersonalWithAccountsNoWarning(t *testing.T) {
	ctx := trackCtx(t)
	mgr := state.NewManager()
	mgr.Set(&state.State{Humans: map[string]*db.Human{
		ctx.TargetID: {ID: ctx.TargetID, Accounts: []db.PlayerAccount{{}}},
	}})
	ctx.StateMgr = mgr
	replies := runTrack(t, ctx, "25 personalshiny")
	if len(replies) == 0 || replies[0].React == "🙅" {
		t.Fatalf("replies = %+v", replies)
	}
	if strings.Contains(replies[0].Text, "account add") {
		t.Fatalf("unexpected warning: %q", replies[0].Text)
	}
}

func TestTrackEverythingPersonalShinyIsNarrowing(t *testing.T) {
	ctx := trackCtx(t)
	ctx.IsAdmin = false
	ctx.Config.Tracking.EverythingFlagPermissions = "allow-any"
	replies := runTrack(t, ctx, "everything personalshiny")
	if len(replies) > 0 && replies[0].React == "🙅" {
		t.Fatalf("everything personalshiny rejected: %+v", replies)
	}
}
