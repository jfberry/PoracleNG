package store

import (
	log "github.com/sirupsen/logrus"
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

// UIDReassigner is implemented by tracking stores that can move a row to a
// specific uid. A full replace (delete + insert) uses it to give the new row
// the replaced row's uid, so rule ids that clients, mutes and `id:N`
// commands hold on to stay valid.
type UIDReassigner interface {
	ReassignUID(id string, from, to int64) error
}

// DiffResult holds the classified results of comparing insert candidates
// against existing tracked rows.
type DiffResult[T any] struct {
	AlreadyPresent []T // unchanged duplicates
	Updates        []T // rows that differ only in updatable fields (UID set)
	Inserts        []T // genuinely new rows
	// Replaced[i] is a copy of the stored row that Updates[i] replaces (same uid).
	Replaced []T
}

// DiffAndClassify classifies insert candidates against existing rows using
// RowDiffPolicy (db.DiffTracking, struct-tag-driven). The caller decides what
// to do with the result.
//
// candidates is no longer modified in place; use the returned DiffResult.
//
// getUID and setUID access the UID field on the tracking struct. These are
// needed because generics can't access struct fields directly.
func DiffAndClassify[T any](
	existing []T,
	candidates []T,
	getUID func(*T) int64,
	setUID func(*T, int64),
) DiffResult[T] {
	return RowDiffPolicy[T]{GetUID: getUID, SetUID: setUID}.Classify(existing, candidates)
}

// ApplyDiff performs the full diff+delete+insert cycle for tracking mutations.
// Updated rules keep their uid when the store implements UIDReassigner.
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

	// Insert new + updated rows, writing each generated UID back into the
	// diff row. The SQL store's Insert returns the UID without mutating the
	// row (only the mock mutates), and callers build API responses from
	// these rows — without the write-back, inserts would report uid 0 and
	// updates the old, just-deleted uid.
	for i := range diff.Inserts {
		uid, err := store.Insert(&diff.Inserts[i])
		if err != nil {
			return diff, err
		}
		setUID(&diff.Inserts[i], uid)
	}
	// An update replaces the row (delete + insert); where the store can, the
	// replacement is moved back onto the old uid so the rule keeps its id —
	// tracking mutes, alert buttons and remembered `id:N` references stay
	// valid. Stores without UIDReassigner keep the freshly inserted uid.
	reassigner, canReassign := store.(UIDReassigner)
	for i := range diff.Updates {
		oldUID := getUID(&diff.Updates[i])
		uid, err := store.Insert(&diff.Updates[i])
		if err != nil {
			return diff, err
		}
		if canReassign && oldUID > 0 {
			// The rule is already replaced at this point; if moving it back
			// fails it simply keeps the new uid, so log rather than fail.
			if err := reassigner.ReassignUID(id, uid, oldUID); err != nil {
				log.Errorf("tracking update: keep uid %d for %s: %v", oldUID, id, err)
			} else {
				uid = oldUID
			}
		}
		setUID(&diff.Updates[i], uid)
	}

	return diff, nil
}
