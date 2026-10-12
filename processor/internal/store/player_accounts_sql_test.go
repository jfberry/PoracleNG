package store

import "testing"

func openPlayerAccountsDB(t *testing.T) PlayerAccountStore {
	t.Helper()
	return openPlayerAccountsDBCollation(t, "")
}

// openPlayerAccountsDBCollation creates the table with an explicit label
// collation clause (e.g. "COLLATE utf8mb4_bin"; empty = server default).
func openPlayerAccountsDBCollation(t *testing.T, labelCollation string) PlayerAccountStore {
	t.Helper()
	dbx := openTestDB(t)
	exec := func(q string) {
		t.Helper()
		if _, err := dbx.Exec(q); err != nil {
			t.Fatalf("setup query failed (%s): %v", q, err)
		}
	}
	exec("DROP TABLE IF EXISTS `player_accounts`")
	exec("CREATE TABLE `player_accounts` (" +
		"`uid` INT PRIMARY KEY AUTO_INCREMENT," +
		"`id` VARCHAR(255) NOT NULL," +
		"`player_id` VARBINARY(256) NOT NULL," +
		"`label` VARCHAR(64) " + labelCollation + " NULL," +
		"`created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP," +
		"UNIQUE KEY `uniq_id_player` (`id`, `player_id`)," +
		"UNIQUE KEY `uniq_id_label` (`id`, `label`)" +
		") ENGINE=InnoDB")
	t.Cleanup(func() { _, _ = dbx.Exec("DROP TABLE IF EXISTS `player_accounts`") })
	return NewPlayerAccountStore(dbx)
}

func TestSQLPlayerAccounts(t *testing.T) {
	runPlayerAccountStoreContract(t, openPlayerAccountsDB(t))
}

func TestSQLPlayerAccountsLimit(t *testing.T) {
	runPlayerAccountStoreLimitContract(t, openPlayerAccountsDB(t))
}

func TestSQLPlayerAccountsLongID(t *testing.T) {
	runPlayerAccountStoreLongIDContract(t, openPlayerAccountsDB(t))
}

// Label uniqueness must not depend on the column collation: with a binary
// (case-sensitive) label column the unique key alone would accept "MAIN"
// next to "main", so the store has to reject it in code.
func TestSQLPlayerAccountsCaseSensitiveCollation(t *testing.T) {
	runPlayerAccountStoreContract(t, openPlayerAccountsDBCollation(t, "COLLATE utf8mb4_bin"))
}
