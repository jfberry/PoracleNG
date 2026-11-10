package bot

import "github.com/pokemon/poracleng/processor/internal/humanlock"

// TargetLockExempt is implemented by commands that never write their own
// target but act on other humans or on none. RunLocked does not take the
// target lock for them; they take the per-human lock themselves where needed.
type TargetLockExempt interface{ ExemptFromTargetLock() }

// RunLocked runs cmd while holding the per-human write lock for ctx.TargetID,
// serialising it against API writes and reconcilers touching the same human.
// Every host that dispatches commands must call this instead of cmd.Run.
// Commands that delegate to other commands internally keep calling Run: they
// already run under their host's lock, and the lock is not reentrant.
func RunLocked(cmd Command, ctx *CommandContext, args []string) []Reply {
	if _, exempt := cmd.(TargetLockExempt); exempt {
		return cmd.Run(ctx, args)
	}
	unlock := humanlock.Lock(ctx.TargetID)
	defer unlock()
	return cmd.Run(ctx, args)
}
