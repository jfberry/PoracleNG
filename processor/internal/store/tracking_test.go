package store

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestDiffAndClassify_AllNew(t *testing.T) {
	existing := []db.LureTrackingAPI{}
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
		{ID: "1", ProfileNo: 1, LureID: 502, Distance: 100, Template: "1"},
	}

	result := DiffAndClassify(existing, candidates, LureGetUID, LureSetUID)

	if len(result.Inserts) != 2 {
		t.Fatalf("expected 2 inserts, got %d", len(result.Inserts))
	}
	if len(result.AlreadyPresent) != 0 {
		t.Fatalf("expected 0 already present, got %d", len(result.AlreadyPresent))
	}
	if len(result.Updates) != 0 {
		t.Fatalf("expected 0 updates, got %d", len(result.Updates))
	}
}

func TestDiffAndClassify_Duplicate(t *testing.T) {
	existing := []db.LureTrackingAPI{
		{UID: 10, ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
	}
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
	}

	result := DiffAndClassify(existing, candidates, LureGetUID, LureSetUID)

	if len(result.AlreadyPresent) != 1 {
		t.Fatalf("expected 1 already present, got %d", len(result.AlreadyPresent))
	}
	if len(result.Inserts) != 0 {
		t.Fatalf("expected 0 inserts, got %d", len(result.Inserts))
	}
}

func TestDiffAndClassify_Update(t *testing.T) {
	existing := []db.LureTrackingAPI{
		{UID: 10, ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
	}
	// Same match key (LureID=501) but different updatable field (Distance)
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 200, Template: "1"},
	}

	result := DiffAndClassify(existing, candidates, LureGetUID, LureSetUID)

	if len(result.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(result.Updates))
	}
	if result.Updates[0].UID != 10 {
		t.Errorf("expected UID 10 carried over, got %d", result.Updates[0].UID)
	}
	if result.Updates[0].Distance != 200 {
		t.Errorf("expected distance 200, got %d", result.Updates[0].Distance)
	}
	if len(result.Inserts) != 0 {
		t.Fatalf("expected 0 inserts, got %d", len(result.Inserts))
	}
}

func TestDiffAndClassify_Mixed(t *testing.T) {
	existing := []db.LureTrackingAPI{
		{UID: 10, ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
		{UID: 11, ID: "1", ProfileNo: 1, LureID: 502, Distance: 100, Template: "1"},
	}
	candidates := []db.LureTrackingAPI{
		// Duplicate of UID 10
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
		// Update of UID 11 (different distance)
		{ID: "1", ProfileNo: 1, LureID: 502, Distance: 500, Template: "1"},
		// New
		{ID: "1", ProfileNo: 1, LureID: 503, Distance: 100, Template: "1"},
	}

	result := DiffAndClassify(existing, candidates, LureGetUID, LureSetUID)

	if len(result.AlreadyPresent) != 1 {
		t.Errorf("expected 1 already present, got %d", len(result.AlreadyPresent))
	}
	if len(result.Updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(result.Updates))
	}
	if len(result.Inserts) != 1 {
		t.Errorf("expected 1 insert, got %d", len(result.Inserts))
	}
}

func TestDiffAndClassify_NoMatchKey(t *testing.T) {
	// LureID is diff:"match" — different LureIDs = noMatch
	existing := []db.LureTrackingAPI{
		{UID: 10, ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
	}
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 999, Distance: 100, Template: "1"},
	}

	result := DiffAndClassify(existing, candidates, LureGetUID, LureSetUID)

	if len(result.Inserts) != 1 {
		t.Fatalf("expected 1 insert (no match key overlap), got %d", len(result.Inserts))
	}
}

// mockTrackingStore is a simple in-memory TrackingStore for testing ApplyDiff.
type mockTrackingStore[T any] struct {
	inserted   []T
	deletedIDs []int64
	nextUID    int64
}

func (m *mockTrackingStore[T]) SelectByIDProfile(id string, profileNo int) ([]T, error) {
	return nil, nil // not used by ApplyDiff
}

func (m *mockTrackingStore[T]) Insert(row *T) (int64, error) {
	m.nextUID++
	m.inserted = append(m.inserted, *row)
	return m.nextUID, nil
}

func (m *mockTrackingStore[T]) DeleteByUIDs(id string, uids []int64) error {
	m.deletedIDs = append(m.deletedIDs, uids...)
	return nil
}

func (m *mockTrackingStore[T]) DeleteByUID(id string, uid int64) error {
	m.deletedIDs = append(m.deletedIDs, uid)
	return nil
}

func TestApplyDiff_UpdateDeletesAndReinserts(t *testing.T) {
	ms := &mockTrackingStore[db.LureTrackingAPI]{nextUID: 100}

	existing := []db.LureTrackingAPI{
		{UID: 10, ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
	}
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 500, Template: "1"},
	}

	result, err := ApplyDiff(ms, "1", existing, candidates, LureGetUID, LureSetUID)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(result.Updates))
	}

	// Should have deleted the old UID
	if len(ms.deletedIDs) != 1 || ms.deletedIDs[0] != 10 {
		t.Errorf("expected deletion of UID 10, got %v", ms.deletedIDs)
	}

	// Should have inserted the updated row
	if len(ms.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(ms.inserted))
	}
	if ms.inserted[0].Distance != 500 {
		t.Errorf("expected distance 500, got %d", ms.inserted[0].Distance)
	}
}

func TestApplyDiff_NewInsertsOnly(t *testing.T) {
	ms := &mockTrackingStore[db.LureTrackingAPI]{nextUID: 100}

	existing := []db.LureTrackingAPI{}
	candidates := []db.LureTrackingAPI{
		{ID: "1", ProfileNo: 1, LureID: 501, Distance: 100, Template: "1"},
		{ID: "1", ProfileNo: 1, LureID: 502, Distance: 200, Template: "1"},
	}

	result, err := ApplyDiff(ms, "1", existing, candidates, LureGetUID, LureSetUID)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Inserts) != 2 {
		t.Fatalf("expected 2 inserts, got %d", len(result.Inserts))
	}
	if len(ms.deletedIDs) != 0 {
		t.Errorf("expected 0 deletions, got %d", len(ms.deletedIDs))
	}
	if len(ms.inserted) != 2 {
		t.Fatalf("expected 2 inserts to store, got %d", len(ms.inserted))
	}
}
