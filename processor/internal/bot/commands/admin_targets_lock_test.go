package commands

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/humanlock"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// adminTargetCase drives one admin command that writes humans other than its
// own target. written reports whether the command has touched human "u2".
type adminTargetCase struct {
	name    string
	cmd     bot.Command
	args    func(self string) []string
	admin   bool
	written func(m *store.MockHumanStore) bool
}

func adminTargetCases() []adminTargetCase {
	communityIn := func(m *store.MockHumanStore) bool {
		h, _ := m.Get("u2")
		return h != nil && len(h.CommunityMembership) == 1
	}
	return []adminTargetCase{
		{
			name: "community add", cmd: &CommunityCommand{}, admin: true,
			args:    func(id string) []string { return []string{"add", "teamcity", id} },
			written: communityIn,
		},
		{
			name: "community remove", cmd: &CommunityCommand{}, admin: true,
			args: func(id string) []string { return []string{"remove", "teamcity", id} },
			written: func(m *store.MockHumanStore) bool {
				h, _ := m.Get("u2")
				return h != nil && len(h.CommunityMembership) == 0
			},
		},
		{
			name: "community clear", cmd: &CommunityCommand{}, admin: true,
			args: func(id string) []string { return []string{"clear", id} },
			written: func(m *store.MockHumanStore) bool {
				h, _ := m.Get("u2")
				return h != nil && len(h.CommunityMembership) == 0
			},
		},
		{
			name: "enable", cmd: &EnableCommand{}, admin: true,
			args: func(id string) []string { return []string{id} },
			written: func(m *store.MockHumanStore) bool {
				h, _ := m.Get("u2")
				return h != nil && !h.AdminDisable
			},
		},
		{
			name: "disable", cmd: &DisableCommand{}, admin: true,
			args: func(id string) []string { return []string{id} },
			written: func(m *store.MockHumanStore) bool {
				h, _ := m.Get("u2")
				return h != nil && h.AdminDisable
			},
		},
		{
			name: "unregister", cmd: &UnregisterCommand{}, admin: true,
			args: func(id string) []string { return []string{id} },
			written: func(m *store.MockHumanStore) bool {
				h, _ := m.Get("u2")
				return h == nil
			},
		},
	}
}

func seedAdminTargetCtx(t *testing.T, c adminTargetCase) (*bot.CommandContext, *store.MockHumanStore) {
	t.Helper()
	ctx, mock := testCtx(t)
	ctx.IsAdmin = c.admin
	ctx.Config = &config.Config{Area: config.AreaConfig{
		Enabled:     true,
		Communities: []config.CommunityConfig{{Name: "teamcity"}},
	}}
	u2 := &store.Human{ID: "u2", Type: bot.TypeDiscordUser, Name: "u2", Enabled: true, CurrentProfileNo: 1}
	switch c.name {
	case "community remove", "community clear":
		u2.CommunityMembership = []string{"teamcity"}
	case "enable":
		u2.AdminDisable = true
	}
	mock.AddHuman(u2)
	return ctx, mock
}

// Commands that write other humans must lock each human they write, not
// only the admin that ran them (spec §3.6).
func TestAdminTargetCommands_LockEachWrittenHuman(t *testing.T) {
	for _, c := range adminTargetCases() {
		t.Run(c.name, func(t *testing.T) {
			ctx, mock := seedAdminTargetCtx(t, c)
			unlock := humanlock.Lock("u2")
			done := make(chan struct{})
			go func() {
				bot.RunLocked(c.cmd, ctx, c.args("u2"))
				close(done)
			}()
			select {
			case <-done:
				unlock()
				t.Fatal("command finished while the written human's lock was held")
			case <-time.After(150 * time.Millisecond):
			}
			if c.written(mock) {
				unlock()
				t.Fatal("u2 was written while its lock was held")
			}
			unlock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("command did not finish after unlock")
			}
			if !c.written(mock) {
				t.Fatal("u2 was not written")
			}
		})
	}
}

// An admin naming their own id must not self-deadlock (the lock is not
// reentrant, so the command must not also hold its target lock).
func TestAdminTargetCommands_OwnIDDoesNotDeadlock(t *testing.T) {
	for _, c := range adminTargetCases() {
		if c.name == "unregister" { // admin unregister filters out self
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			ctx, _ := seedAdminTargetCtx(t, c)
			ctx.TargetID = "u2"
			done := make(chan struct{})
			go func() {
				bot.RunLocked(c.cmd, ctx, c.args("u2"))
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("deadlock: command targeting its own id did not finish")
			}
		})
	}
}

// Non-admin !unregister writes its own target; it must still lock it and
// must not deadlock.
func TestUnregister_SelfLocksAndDoesNotDeadlock(t *testing.T) {
	ctx, mock := testCtx(t)
	ctx.IsAdmin = false
	unlock := humanlock.Lock("user1")
	done := make(chan struct{})
	go func() {
		bot.RunLocked(&UnregisterCommand{}, ctx, nil)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	if h, _ := mock.Get("user1"); h == nil {
		unlock()
		t.Fatal("deleted while lock held")
	}
	unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("unregister did not finish")
	}
	if h, _ := mock.Get("user1"); h != nil {
		t.Fatal("self not unregistered")
	}
}
