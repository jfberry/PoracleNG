package store

import "testing"

// Create must not store current_profile_no = 0 for a caller that leaves it
// unset. The column default (1) never applies because the INSERT names it.
func TestSQLHumanStore_CreateDefaultsProfileNo(t *testing.T) {
	dbx := openTestDB(t)
	s := &SQLHumanStore{db: dbx}
	t.Cleanup(func() { _, _ = dbx.Exec(`DELETE FROM humans WHERE id = 'u-profile-default'`) })

	if err := s.Create(&Human{ID: "u-profile-default", Type: "discord:user", Name: "tester", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetLite("u-profile-default")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.CurrentProfileNo != 1 {
		t.Fatalf("CurrentProfileNo = %+v, want 1", got)
	}
}
