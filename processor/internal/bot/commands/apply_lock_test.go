package commands

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/humanlock"
	"github.com/pokemon/poracleng/processor/internal/store"
)

type lockProbeCmd struct{ seen chan string }

func (c *lockProbeCmd) Name() string      { return "cmd.probe" }
func (c *lockProbeCmd) Aliases() []string { return nil }
func (c *lockProbeCmd) Run(ctx *bot.CommandContext, _ []string) []bot.Reply {
	c.seen <- ctx.TargetID
	return nil
}

func TestApply_LocksEachTargetIncludingSelf(t *testing.T) {
	humans := store.NewMockHumanStore()
	for _, id := range []string{"a1", "u2"} {
		humans.AddHuman(&store.Human{ID: id, Type: bot.TypeDiscordUser, Name: id, Enabled: true, CurrentProfileNo: 1})
	}
	// Name lookups fail fast against an unreachable DB and are skipped.
	db, err := sqlx.Open("mysql", "u:p@tcp(127.0.0.1:1)/x")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	probe := &lockProbeCmd{seen: make(chan string, 4)}
	reg := bot.NewRegistry()
	reg.Register(probe)
	ctx := &bot.CommandContext{
		IsAdmin: true, TargetID: "a1", Humans: humans, DB: db, Registry: reg,
	}

	unlockU2 := humanlock.Lock("u2")
	done := make(chan struct{})
	go func() {
		bot.RunLocked(&ApplyCommand{}, ctx, []string{"a1", "u2", "|", "probe"})
		close(done)
	}()

	select {
	case id := <-probe.seen:
		if id != "a1" {
			t.Fatalf("first target = %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("apply did not run against self (deadlock on own id?)")
	}
	select {
	case id := <-probe.seen:
		t.Fatalf("ran against %s while its lock was held", id)
	case <-time.After(100 * time.Millisecond):
	}
	unlockU2()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("apply did not finish after unlock")
	}
}
