package bot

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/humanlock"
)

type fakeCmd struct {
	run func(ctx *CommandContext, args []string) []Reply
}

func (f *fakeCmd) Name() string      { return "cmd.fake" }
func (f *fakeCmd) Aliases() []string { return nil }
func (f *fakeCmd) Run(ctx *CommandContext, args []string) []Reply {
	if f.run != nil {
		return f.run(ctx, args)
	}
	return nil
}

type exemptCmd struct{ fakeCmd }

func (*exemptCmd) ExemptFromTargetLock() {}

func TestRunLocked_SerialisesWithAPI(t *testing.T) {
	unlock := humanlock.Lock("u1")
	done := make(chan struct{})
	go func() {
		RunLocked(&fakeCmd{}, &CommandContext{TargetID: "u1"}, nil)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("RunLocked returned while lock was held")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunLocked did not return after unlock")
	}
}

func TestRunLocked_ExemptCommandCanLockItsOwnTarget(t *testing.T) {
	cmd := &exemptCmd{fakeCmd{run: func(ctx *CommandContext, _ []string) []Reply {
		defer humanlock.Lock(ctx.TargetID)()
		return nil
	}}}
	done := make(chan struct{})
	go func() {
		RunLocked(cmd, &CommandContext{TargetID: "admin"}, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("exempt command deadlocked locking its own target")
	}
}
