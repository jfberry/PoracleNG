package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/mute"
)

func muteTestCtx(t *testing.T) (*bot.CommandContext, *mute.Store) {
	t.Helper()
	ctx, _ := testCtx(t)
	store := mute.NewStore()
	ctx.MuteStore = store
	reg := bot.NewRegistry()
	reg.Register(&MuteCommand{})
	reg.Register(&UnmuteCommand{})
	ctx.Registry = reg
	return ctx, store
}

func onlyMute(t *testing.T, store *mute.Store) mute.Entry {
	t.Helper()
	list := store.List("user1")
	if len(list) != 1 {
		t.Fatalf("mutes = %+v, want exactly one", list)
	}
	return list[0]
}

// Per-type aliases prepend the type name (`!invasion mute pokestop X` runs
// `!mute invasion pokestop X`). Only `<type> id:N` used to be understood;
// any other scope after the type was read as a pokemon name (#280).
func TestMuteTypeAliasWithScope(t *testing.T) {
	cases := []struct {
		typ       string
		args      []string
		wantScope string
		wantValue string
	}{
		{"invasion", []string{"mute", "pokestop", "stop1"}, mute.ScopePokestop, "stop1"},
		{"maxbattle", []string{"mute", "station", "st1"}, mute.ScopeStation, "st1"},
		{"raid", []string{"mute", "everything"}, mute.ScopeEverything, ""},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			ctx, store := muteTestCtx(t)
			replies := RouteToMuteFromType(ctx, tc.typ, tc.args)
			if len(replies) == 0 || replies[0].React != "🔇" {
				t.Fatalf("expected a mute, got %+v", replies)
			}
			e := onlyMute(t, store)
			if e.ScopeType != tc.wantScope || e.ScopeValue != tc.wantValue {
				t.Errorf("mute = %s/%q, want %s/%q", e.ScopeType, e.ScopeValue, tc.wantScope, tc.wantValue)
			}
		})
	}
}

// !track routes its alias as type "pokemon", which is also a scope noun:
// `!track mute id:N` became a pokemon mute named "id:N" (#280).
func TestTrackMuteByRuleID(t *testing.T) {
	ctx, store := muteTestCtx(t)
	withRules(ctx, 1, 0)
	replies := RouteToMuteFromType(ctx, "pokemon", []string{"mute", "id:1"})
	if len(replies) == 0 || replies[0].React != "🔇" {
		t.Fatalf("expected a mute, got %+v", replies)
	}
	e := onlyMute(t, store)
	if e.ScopeType != mute.ScopeTracking || e.ScopeValue != "1" || e.RuleTable != "pokemon" {
		t.Errorf("mute = %s/%s/%q, want tracking/pokemon/1", e.ScopeType, e.RuleTable, e.ScopeValue)
	}

	replies = RouteToMuteFromType(ctx, "pokemon", []string{"unmute", "id:1"})
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("expected unmute, got %+v", replies)
	}
	if n := len(store.List("user1")); n != 0 {
		t.Errorf("mutes after unmute = %d, want 0", n)
	}
}

// The unmute alias must strip the type the same way mute does.
func TestUnmuteTypeAliasWithScope(t *testing.T) {
	ctx, store := muteTestCtx(t)
	RouteToMuteFromType(ctx, "invasion", []string{"mute", "pokestop", "stop1"})
	onlyMute(t, store)
	replies := RouteToMuteFromType(ctx, "invasion", []string{"unmute", "pokestop", "stop1"})
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("expected unmute, got %+v", replies)
	}
	if n := len(store.List("user1")); n != 0 {
		t.Errorf("mutes after unmute = %d, want 0", n)
	}
}

// `!gym mute pokestop X` — the type token is also a scope noun; the scope
// after it must win.
func TestGymAliasWithOtherScope(t *testing.T) {
	ctx, store := muteTestCtx(t)
	RouteToMuteFromType(ctx, "gym", []string{"mute", "pokestop", "stop1"})
	e := onlyMute(t, store)
	if e.ScopeType != mute.ScopePokestop || e.ScopeValue != "stop1" {
		t.Errorf("mute = %s/%q, want pokestop/stop1", e.ScopeType, e.ScopeValue)
	}
}

// withRules gives the test user nMon pokemon rules and nRaid raid rules.
// The mock assigns uids per table from 1, so uid 1 exists in both tables when
// both counts are > 0.
func withRules(ctx *bot.CommandContext, nMon, nRaid int) {
	mon := store.NewMockTrackingStore[db.MonsterTrackingAPI](store.MonsterGetUID, store.MonsterSetUID)
	raid := store.NewMockTrackingStore[db.RaidTrackingAPI](store.RaidGetUID, store.RaidSetUID)
	for i := 0; i < nMon; i++ {
		_, _ = mon.Insert(&db.MonsterTrackingAPI{})
	}
	for i := 0; i < nRaid; i++ {
		_, _ = raid.Insert(&db.RaidTrackingAPI{})
	}
	ctx.Tracking = &store.TrackingStores{Monsters: mon, Raids: raid}
}

func TestTypedMuteByRuleID(t *testing.T) {
	ctx, ms := muteTestCtx(t)
	withRules(ctx, 1, 1)
	replies := RouteToMuteFromType(ctx, "raid", []string{"mute", "id:1"})
	if len(replies) == 0 || replies[0].React != "🔇" {
		t.Fatalf("expected a mute, got %+v", replies)
	}
	e := onlyMute(t, ms)
	if e.ScopeType != mute.ScopeTracking || e.RuleTable != "raid" || e.ScopeValue != "1" {
		t.Errorf("mute = %+v, want tracking/raid/1", e)
	}
}

func TestBareMuteByRuleIDAmbiguous(t *testing.T) {
	ctx, ms := muteTestCtx(t)
	withRules(ctx, 1, 1)
	replies := (&MuteCommand{}).Run(ctx, []string{"id:1"})
	if len(replies) == 0 || replies[0].React != "🙅" ||
		!strings.Contains(replies[0].Text, "more than one") ||
		!strings.Contains(replies[0].Text, "pokemon") || !strings.Contains(replies[0].Text, "raid") {
		t.Fatalf("expected ambiguous refusal listing both tables, got %+v", replies)
	}
	if n := len(ms.List("user1")); n != 0 {
		t.Errorf("mutes = %d, want 0", n)
	}
}

func TestBareMuteByRuleIDSingleTable(t *testing.T) {
	ctx, ms := muteTestCtx(t)
	withRules(ctx, 0, 1)
	replies := (&MuteCommand{}).Run(ctx, []string{"id:1"})
	if len(replies) == 0 || replies[0].React != "🔇" {
		t.Fatalf("expected a mute, got %+v", replies)
	}
	if e := onlyMute(t, ms); e.RuleTable != "raid" || e.ScopeValue != "1" {
		t.Errorf("mute = %+v, want raid/1", e)
	}
}

func TestMuteByRuleIDNotFound(t *testing.T) {
	ctx, ms := muteTestCtx(t)
	withRules(ctx, 1, 1)
	for _, args := range [][]string{{"id:999"}, {"raid", "id:999"}} {
		replies := (&MuteCommand{}).Run(ctx, args)
		if len(replies) == 0 || replies[0].React != "🙅" || !strings.Contains(replies[0].Text, "No rule id:999") {
			t.Fatalf("%v: expected not found, got %+v", args, replies)
		}
	}
	if n := len(ms.List("user1")); n != 0 {
		t.Errorf("mutes = %d, want 0", n)
	}
}

func TestUnmuteByRuleIDResolvesTable(t *testing.T) {
	ctx, ms := muteTestCtx(t)
	withRules(ctx, 1, 1)
	RouteToMuteFromType(ctx, "raid", []string{"mute", "id:1"})
	RouteToMuteFromType(ctx, "pokemon", []string{"mute", "id:1"})
	// Both tables muted: bare unmute is ambiguous, typed removes one.
	replies := (&UnmuteCommand{}).Run(ctx, []string{"id:1"})
	if len(replies) == 0 || replies[0].React != "🙅" || !strings.Contains(replies[0].Text, "more than one") {
		t.Fatalf("expected ambiguous, got %+v", replies)
	}
	replies = RouteToMuteFromType(ctx, "raid", []string{"unmute", "id:1"})
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("expected unmute, got %+v", replies)
	}
	if e := onlyMute(t, ms); e.RuleTable != "pokemon" {
		t.Errorf("remaining = %+v, want pokemon", e)
	}
	// Only one left: bare unmute removes it.
	replies = (&UnmuteCommand{}).Run(ctx, []string{"id:1"})
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("expected unmute, got %+v", replies)
	}
	if n := len(ms.List("user1")); n != 0 {
		t.Errorf("mutes = %d, want 0", n)
	}
}

func TestTrackedShowsTypedRuleMute(t *testing.T) {
	ctx, _ := muteTestCtx(t)
	e := mute.Entry{HumanID: "user1", ScopeType: mute.ScopeTracking, RuleTable: "raid", ScopeValue: "60"}
	if got := propertyMuteValueLabel(ctx, e); got != "raid rule 60" {
		t.Errorf("label = %q, want %q", got, "raid rule 60")
	}
}
