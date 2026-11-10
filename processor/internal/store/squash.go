package store

import "github.com/pokemon/poracleng/processor/internal/db"

// SquashPolicy decides when a submitted tracking rule is merged into rules
// that already exist ("squashing"). Keeping it behind an interface lets the
// merge rule be replaced without touching the callers of ApplyDiff.
type SquashPolicy[T any] interface {
	// Classify sorts candidates into already-present duplicates, updates of
	// an existing row, and genuinely new inserts. An existing row is the
	// target of at most one update per call.
	Classify(existing, candidates []T) DiffResult[T]

	// Conflicts reports the first row other than editedUID that the edited
	// rule would be squashed into (as a duplicate or an update).
	Conflicts(existing []T, edited *T, editedUID int64) (otherUID int64, conflict bool)
}

// RowDiffPolicy is the current policy: the struct-tag driven row comparison
// of db.DiffTracking (a rule is a duplicate when every field matches, an
// update when exactly one updatable field differs).
type RowDiffPolicy[T any] struct {
	GetUID func(*T) int64
	SetUID func(*T, int64)
}

// Classify walks candidates in reverse and matches each against the existing
// rows in order, first match winning. Updates carry the replaced row's uid and
// Replaced[i] is a copy of the stored row Updates[i] replaces.
//
// Claiming and pinning keep the batch consistent and independent of the order
// candidates are submitted in:
//   - A row chosen as an update target is claimed: later candidates skip it, so
//     one call never produces two updates of one uid.
//   - A row that any candidate matches as a duplicate is pinned (decided up
//     front, before any update is chosen). A pinned row is never an update
//     target in the same call, otherwise ApplyDiff would delete a row the
//     response reports as unchanged. A candidate that would have updated it is
//     classified against the remaining rows. Pinned rows still match further
//     identical candidates, so several identical candidates all stay
//     AlreadyPresent.
//   - A candidate that exactly duplicates any existing row is AlreadyPresent
//     (carrying that row's uid) without an update scan, even when an earlier
//     row is one updatable field away from it.
//
// candidates is not modified; Inserts keeps the submission order.
func (p RowDiffPolicy[T]) Classify(existing, candidates []T) DiffResult[T] {
	var result DiffResult[T]

	// dupOf[i] is the uid of the first existing row candidate i duplicates
	// exactly (hasDup[i] set). Such a candidate is AlreadyPresent outright:
	// it must never be read as a one-field update of some other row that
	// happens to come earlier in existing.
	pinned := make(map[int64]bool)
	dupOf := make([]int64, len(candidates))
	hasDup := make([]bool, len(candidates))
	for i := range candidates {
		for j := range existing {
			if _, isDup, _, _ := db.DiffTracking(&existing[j], &candidates[i]); isDup {
				uid := p.GetUID(&existing[j])
				pinned[uid] = true
				if !hasDup[i] {
					dupOf[i], hasDup[i] = uid, true
				}
			}
		}
	}

	claimed := make(map[int64]bool)
	isInsert := make([]bool, len(candidates))

	for i := len(candidates) - 1; i >= 0; i-- {
		if hasDup[i] {
			present := candidates[i]
			p.SetUID(&present, dupOf[i])
			result.AlreadyPresent = append(result.AlreadyPresent, present)
			continue
		}
		matched := false
		for j := range existing {
			rowUID := p.GetUID(&existing[j])
			if claimed[rowUID] || pinned[rowUID] {
				continue
			}
			noMatch, _, uid, isUpd := db.DiffTracking(&existing[j], &candidates[i])
			if noMatch {
				continue
			}
			if isUpd {
				update := candidates[i]
				p.SetUID(&update, uid)
				claimed[uid] = true
				result.Updates = append(result.Updates, update)
				result.Replaced = append(result.Replaced, existing[j])
				matched = true
				break
			}
		}
		isInsert[i] = !matched
	}

	if candidates != nil {
		result.Inserts = make([]T, 0, len(candidates))
	}
	for i := range candidates {
		if isInsert[i] {
			result.Inserts = append(result.Inserts, candidates[i])
		}
	}
	return result
}

// Conflicts returns the first other row (uid != editedUID) that edited would
// be squashed into.
func (p RowDiffPolicy[T]) Conflicts(existing []T, edited *T, editedUID int64) (int64, bool) {
	for j := range existing {
		other := p.GetUID(&existing[j])
		if other == editedUID {
			continue
		}
		noMatch, isDup, _, isUpd := db.DiffTracking(&existing[j], edited)
		if noMatch {
			continue
		}
		if isDup || isUpd {
			return other, true
		}
	}
	return 0, false
}
