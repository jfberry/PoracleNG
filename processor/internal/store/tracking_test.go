package store

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestMonstersInsertWithOverride(t *testing.T) {
	dbx := openTestDB(t)
	s := NewTrackingStores(dbx)
	uid, err := s.Monsters.Insert(&db.MonsterTrackingAPI{
		ID: "u1", ProfileNo: 0, PokemonID: 25,
		OverrideLocationLabel: "Home",
		OverrideAreas:         []string{"berlin", "munich"},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, _ := s.Monsters.SelectByIDProfile("u1", 0)
	if len(rows) != 1 || rows[0].UID != uid {
		t.Fatalf("expected 1 row uid=%d, got %+v", uid, rows)
	}
	if rows[0].OverrideLocationLabel != "Home" {
		t.Fatalf("label round-trip: got %q", rows[0].OverrideLocationLabel)
	}
	if len(rows[0].OverrideAreas) != 2 || rows[0].OverrideAreas[0] != "berlin" {
		t.Fatalf("areas round-trip: got %v", rows[0].OverrideAreas)
	}

	// Insert with no override — both fields nil/empty
	uid2, _ := s.Monsters.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 0, PokemonID: 26})
	rows, _ = s.Monsters.SelectByIDProfile("u1", 0)
	var got *db.MonsterTrackingAPI
	for i := range rows {
		if rows[i].UID == uid2 {
			got = &rows[i]
		}
	}
	if got == nil || got.OverrideLocationLabel != "" || len(got.OverrideAreas) != 0 {
		t.Fatalf("no-override row should have empty fields; got %+v", got)
	}
}

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

// sqlFaithfulLureStore mimics sqlTrackingStore's contract exactly: Insert
// returns the new UID but does NOT mutate the caller's row — only
// MockTrackingStore does that. Regression guard for the v2 POST uid:0 bug,
// which every MockTrackingStore-based test masked.
type sqlFaithfulLureStore struct {
	nextUID int64
	rows    map[int64]db.LureTrackingAPI
}

func newSQLFaithfulLureStore() *sqlFaithfulLureStore {
	return &sqlFaithfulLureStore{rows: map[int64]db.LureTrackingAPI{}}
}

func (s *sqlFaithfulLureStore) SelectByIDProfile(id string, profileNo int) ([]db.LureTrackingAPI, error) {
	var out []db.LureTrackingAPI
	for _, r := range s.rows {
		if r.ID == id && r.ProfileNo == profileNo {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *sqlFaithfulLureStore) Insert(row *db.LureTrackingAPI) (int64, error) {
	s.nextUID++
	stored := *row
	stored.UID = s.nextUID
	s.rows[s.nextUID] = stored
	return s.nextUID, nil // caller's row deliberately NOT mutated
}

func (s *sqlFaithfulLureStore) DeleteByUIDs(id string, uids []int64) error {
	for _, uid := range uids {
		delete(s.rows, uid)
	}
	return nil
}

func (s *sqlFaithfulLureStore) DeleteByUID(id string, uid int64) error {
	delete(s.rows, uid)
	return nil
}

// ApplyDiff must propagate the UIDs generated by Insert back into the
// returned diff rows — for inserts (fresh UID) and for updates (the
// replacement row's new UID, not the deleted old one). The v2 tracking API
// builds its POST response envelopes from these rows; without propagation,
// created rules report uid:0 and updated rules report a deleted uid,
// neither addressable by a follow-up GET/PUT/DELETE.
func TestApplyDiffPropagatesInsertUIDs(t *testing.T) {
	st := newSQLFaithfulLureStore()

	// Seed one existing rule: lure 502, template "1" → uid 1.
	if _, err := st.Insert(&db.LureTrackingAPI{ID: "u1", ProfileNo: 1, LureID: 502, Template: "1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	existing, _ := st.SelectByIDProfile("u1", 1)

	candidates := []db.LureTrackingAPI{
		{ID: "u1", ProfileNo: 1, LureID: 502, Template: "2"}, // update (template changed)
		{ID: "u1", ProfileNo: 1, LureID: 503, Template: "1"}, // genuinely new
	}

	diff, err := ApplyDiff(st, "u1", existing, candidates, LureGetUID, LureSetUID)
	if err != nil {
		t.Fatalf("ApplyDiff: %v", err)
	}
	if len(diff.Inserts) != 1 || len(diff.Updates) != 1 {
		t.Fatalf("expected 1 insert + 1 update, got %d/%d", len(diff.Inserts), len(diff.Updates))
	}

	ins := diff.Inserts[0]
	if ins.UID == 0 {
		t.Errorf("insert row has uid 0 — Insert's returned UID was not propagated")
	}
	if _, ok := st.rows[ins.UID]; !ok {
		t.Errorf("insert row uid %d does not address a stored row", ins.UID)
	}

	upd := diff.Updates[0]
	if upd.UID == 1 {
		t.Errorf("update row still carries the deleted uid 1 instead of its replacement's uid")
	}
	if stored, ok := st.rows[upd.UID]; !ok || stored.Template != "2" {
		t.Errorf("update row uid %d does not address the replacement row (stored: %+v)", upd.UID, st.rows)
	}
}
