package bot

import "time"

// PauseChecker is satisfied by *delivery.Dispatcher (and by test stubs).
// It is defined here in the bot package to avoid an import cycle (delivery
// already imports bot via the job type).
type PauseChecker interface {
	// IsPaused returns whether delivery is currently paused. Lock-free fast
	// path — suitable for the per-reply maintenance-suffix check.
	IsPaused() bool
	// PauseState returns whether delivery is currently paused, the reason,
	// and when the pause started. All zero-values when not paused.
	PauseState() (paused bool, reason string, since time.Time)
}

// ApplyMaintenanceSuffix appends the maintenance-mode suffix to the last
// reply in the slice when the dispatcher (or any PauseChecker) reports that
// delivery is currently paused.
//
// Rules:
//   - If replies is empty, it is returned unchanged.
//   - The suffix is added only to the LAST reply so multi-chunk commands
//     (e.g. !tracked chunking long output) do not repeat it N times.
//   - If the last reply already has a Text field, the suffix is appended on
//     a new line.
//   - If the last reply has no Text (embed-only / image-only), a new
//     text-only Reply carrying the suffix is appended.
//   - If checker is nil, the function returns replies unchanged (safe in
//     test contexts where no dispatcher is wired).
//   - If delivery is not paused, replies is returned unchanged.
func ApplyMaintenanceSuffix(replies []Reply, checker PauseChecker, suffix string) []Reply {
	if checker == nil || len(replies) == 0 {
		return replies
	}
	// Fast lock-free check: the 99.9 % not-paused case returns here without
	// ever acquiring pauseMu.
	if !checker.IsPaused() {
		return replies
	}

	last := &replies[len(replies)-1]

	// Embed-only or image/attachment-only reply: add a new trailing text reply.
	if last.Text == "" {
		return append(replies, Reply{Text: suffix})
	}

	// Text reply: append on a new line.
	last.Text += "\n" + suffix
	return replies
}
