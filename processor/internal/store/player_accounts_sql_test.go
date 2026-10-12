package store

import "testing"

func openPlayerAccountsDB(t *testing.T) PlayerAccountStore {
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
		"`label` VARCHAR(64) NULL," +
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
