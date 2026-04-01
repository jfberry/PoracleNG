package store

import (
	"github.com/pokemon/poracleng/processor/internal/db"
)

// TrackingStore provides typed CRUD operations over a single tracking table.
// T is the API struct type (e.g. db.EggTrackingAPI).
type TrackingStore[T any] interface {
	// SelectByIDProfile returns all tracking rows for a user+profile.
	SelectByIDProfile(id string, profileNo int) ([]T, error)

	// Insert inserts a single tracking row and returns the new UID.
	Insert(row *T) (int64, error)

	// DeleteByUIDs deletes rows by id + uid list.
	DeleteByUIDs(id string, uids []int64) error

	// DeleteByUID deletes a single row by id + uid.
	DeleteByUID(id string, uid int64) error
}

// DiffResult holds the classified results of comparing insert candidates
// against existing tracked rows.
type DiffResult[T any] struct {
	AlreadyPresent []T   // unchanged duplicates
	Updates        []T   // rows that differ only in updatable fields (UID set)
	Inserts        []T   // genuinely new rows
}

// DiffAndClassify runs the standard diff loop: for each insert candidate,
// compare against all existing rows using db.DiffTracking (struct-tag-driven).
// Returns classified results. The caller decides what to do with them.
//
// WARNING: This function modifies the candidates slice in place (removes
// elements classified as duplicates or updates). Callers must not read the
// original slice after this call; use the returned DiffResult instead.
//
// getUID and setUID access the UID field on the tracking struct. These are
// needed because generics can't access struct fields directly.
func DiffAndClassify[T any](
	existing []T,
	candidates []T,
	getUID func(*T) int64,
	setUID func(*T, int64),
) DiffResult[T] {
	var result DiffResult[T]

	for i := len(candidates) - 1; i >= 0; i-- {
		classified := false
		for j := range existing {
			noMatch, isDup, uid, isUpd := db.DiffTracking(&existing[j], &candidates[i])
			if noMatch {
				continue
			}
			if isDup {
				result.AlreadyPresent = append(result.AlreadyPresent, candidates[i])
				candidates = append(candidates[:i], candidates[i+1:]...)
				classified = true
				break
			}
			if isUpd {
				update := candidates[i]
				setUID(&update, uid)
				result.Updates = append(result.Updates, update)
				candidates = append(candidates[:i], candidates[i+1:]...)
				classified = true
				break
			}
		}
		if !classified {
			// Stays in candidates — will be a new insert
		}
	}

	result.Inserts = candidates
	return result
}

// ApplyDiff performs the full diff+delete+insert cycle for tracking mutations.
// Returns the diff result and any error. On success, the caller should trigger
// a state reload.
func ApplyDiff[T any](
	store TrackingStore[T],
	id string,
	existing []T,
	candidates []T,
	getUID func(*T) int64,
	setUID func(*T, int64),
) (DiffResult[T], error) {
	diff := DiffAndClassify(existing, candidates, getUID, setUID)

	// Delete UIDs of rows being updated
	if len(diff.Updates) > 0 {
		uids := make([]int64, len(diff.Updates))
		for i := range diff.Updates {
			uids[i] = getUID(&diff.Updates[i])
		}
		if err := store.DeleteByUIDs(id, uids); err != nil {
			return diff, err
		}
	}

	// Insert new + updated rows
	for i := range diff.Inserts {
		if _, err := store.Insert(&diff.Inserts[i]); err != nil {
			return diff, err
		}
	}
	for i := range diff.Updates {
		if _, err := store.Insert(&diff.Updates[i]); err != nil {
			return diff, err
		}
	}

	return diff, nil
}
