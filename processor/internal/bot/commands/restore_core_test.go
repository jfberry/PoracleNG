package commands

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeRestoreDB records statements executed inside the restore transaction.
type fakeRestoreDB struct {
	columns   map[string][]string
	execs     []string
	args      [][]any
	began     bool
	committed bool
	rolled    bool
	failOn    string // substring; Exec of a matching query errors
}

func (f *fakeRestoreDB) TableColumns(table string) (map[string]bool, error) {
	cols, ok := f.columns[table]
	if !ok {
		return nil, errors.New("no such table")
	}
	m := make(map[string]bool, len(cols))
	for _, c := range cols {
		m[c] = true
	}
	return m, nil
}

func (f *fakeRestoreDB) Begin() (restoreTx, error) {
	f.began = true
	return f, nil
}

func (f *fakeRestoreDB) Exec(query string, args ...any) (sql.Result, error) {
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, errors.New("boom")
	}
	f.execs = append(f.execs, query)
	f.args = append(f.args, args)
	return nil, nil
}

func (f *fakeRestoreDB) Commit() error   { f.committed = true; return nil }
func (f *fakeRestoreDB) Rollback() error { f.rolled = true; return nil }

func newFakeRestoreDB() *fakeRestoreDB {
	cols := map[string][]string{}
	for _, t := range backupTables {
		cols[t] = []string{"uid", "id", "profile_no", "template", "distance", "clean", "ping"}
	}
	cols["monsters"] = append(cols["monsters"], "pokemon_id", "min_iv")
	cols["raid"] = append(cols["raid"], "level")
	return &fakeRestoreDB{columns: cols}
}

func TestRestore_RejectsUnknownTable(t *testing.T) {
	db := newFakeRestoreDB()
	backup := map[string][]map[string]any{
		"monsters": {{"pokemon_id": 1}},
		"humans":   {{"id": "x"}},
	}
	err := restoreBackup(db, backup, "u1", 1)
	if err == nil || !strings.Contains(err.Error(), "humans") {
		t.Fatalf("want rejection naming table, got %v", err)
	}
	if db.began || len(db.execs) != 0 {
		t.Fatalf("nothing should be executed, got began=%v execs=%v", db.began, db.execs)
	}
}

func TestRestore_RejectsMaliciousTableName(t *testing.T) {
	db := newFakeRestoreDB()
	backup := map[string][]map[string]any{"monsters` ; DROP TABLE humans; --": {}}
	if err := restoreBackup(db, backup, "u1", 1); err == nil {
		t.Fatal("want rejection")
	}
	if db.began || len(db.execs) != 0 {
		t.Fatalf("nothing should be executed: %v", db.execs)
	}
}

func TestRestore_RejectsBadColumns(t *testing.T) {
	for _, col := range []string{"pokemon_id` = 1; DROP TABLE humans; --", "has space", "Upper", "not_a_column"} {
		db := newFakeRestoreDB()
		backup := map[string][]map[string]any{
			"raid":     {{"level": 5}},
			"monsters": {{"pokemon_id": 1, col: 1}},
		}
		err := restoreBackup(db, backup, "u1", 1)
		if err == nil || !strings.Contains(err.Error(), "monsters") {
			t.Fatalf("col %q: want rejection naming table, got %v", col, err)
		}
		if db.began || len(db.execs) != 0 {
			t.Fatalf("col %q: nothing should be executed: %v", col, db.execs)
		}
	}
}

func TestRestore_ReplacesEveryTable(t *testing.T) {
	db := newFakeRestoreDB()
	backup := map[string][]map[string]any{
		"monsters": {
			{"uid": 99, "id": "0", "profile_no": 0, "pokemon_id": 25, "min_iv": 90},
			{"pokemon_id": 1, "min_iv": -1},
		},
		"raid": {}, // empty in backup → must still be cleared
		// all other tables absent → must still be cleared
	}
	if err := restoreBackup(db, backup, "u1", 3); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !db.committed || db.rolled {
		t.Fatalf("want commit without rollback (committed=%v rolled=%v)", db.committed, db.rolled)
	}

	var deletes, inserts []string
	for i, q := range db.execs {
		switch {
		case strings.HasPrefix(q, "DELETE"):
			deletes = append(deletes, q)
			if db.args[i][0] != "u1" || db.args[i][1] != 3 {
				t.Errorf("delete args = %v", db.args[i])
			}
		case strings.HasPrefix(q, "INSERT"):
			inserts = append(inserts, q)
		}
	}
	if len(deletes) != len(backupTables) {
		t.Fatalf("want a DELETE per backup table (%d), got %d: %v", len(backupTables), len(deletes), deletes)
	}
	for _, tbl := range backupTables {
		if !slices.ContainsFunc(deletes, func(q string) bool { return strings.Contains(q, "`"+tbl+"`") }) {
			t.Errorf("no DELETE for %s", tbl)
		}
	}
	if len(inserts) != 2 {
		t.Fatalf("want 2 inserts, got %v", inserts)
	}
	for i, q := range db.execs {
		if !strings.HasPrefix(q, "INSERT") {
			continue
		}
		if strings.Contains(q, "`uid`") {
			t.Errorf("uid must not be inserted: %s", q)
		}
		cols := strings.Split(q[strings.Index(q, "(")+1:strings.Index(q, ")")], ", ")
		for j, c := range cols {
			if c == "`id`" && db.args[i][j] != "u1" {
				t.Errorf("id not overwritten: %v", db.args[i][j])
			}
			if c == "`profile_no`" && db.args[i][j] != 3 {
				t.Errorf("profile_no not overwritten: %v", db.args[i][j])
			}
		}
	}
}

func TestRestore_RollsBackOnInsertFailure(t *testing.T) {
	db := newFakeRestoreDB()
	db.failOn = "INSERT INTO `monsters`"
	backup := map[string][]map[string]any{"monsters": {{"pokemon_id": 25}}}
	if err := restoreBackup(db, backup, "u1", 1); err == nil {
		t.Fatal("want error")
	}
	if db.committed || !db.rolled {
		t.Fatalf("want rollback (committed=%v rolled=%v)", db.committed, db.rolled)
	}
}

func TestBackupTablesCoverEveryTrackingTable(t *testing.T) {
	for _, tbl := range []string{"monsters", "raid", "egg", "quest", "invasion", "lures", "gym", "nests", "maxbattle", "forts", "tappable"} {
		if !slices.Contains(backupTables, tbl) {
			t.Errorf("backupTables missing %s", tbl)
		}
	}
	// !weather was removed; nothing can create weather rules (#315).
	if slices.Contains(backupTables, "weather") {
		t.Error("backupTables should no longer include weather")
	}
}

// Backups written before weather was dropped carry a "weather" key; restore
// must accept them, ignoring that key, and never touch the weather table.
func TestRestore_IgnoresLegacyWeatherKey(t *testing.T) {
	db := newFakeRestoreDB()
	backup := map[string][]map[string]any{
		"monsters": {{"pokemon_id": 25}},
		"weather":  {{"condition": 3, "cell": "123"}},
	}
	if err := restoreBackup(db, backup, "u1", 1); err != nil {
		t.Fatalf("restore with legacy weather key: %v", err)
	}
	if !db.committed {
		t.Fatal("want commit")
	}
	for _, q := range db.execs {
		if strings.Contains(q, "`weather`") {
			t.Errorf("weather table must not be touched: %s", q)
		}
	}
}
