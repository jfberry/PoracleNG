package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

type sqlPlayerAccountStore struct{ db *sqlx.DB }

// NewPlayerAccountStore returns the MySQL-backed store.
func NewPlayerAccountStore(dbx *sqlx.DB) PlayerAccountStore { return &sqlPlayerAccountStore{db: dbx} }

const playerAccountCols = `uid, id, player_id, COALESCE(label, '') AS label, created_at`

func (s *sqlPlayerAccountStore) List(humanID string) ([]db.PlayerAccount, error) {
	var rows []db.PlayerAccount
	err := s.db.Select(&rows,
		`SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? ORDER BY created_at, uid`, humanID)
	return rows, err
}

// duplicateKind maps a MySQL 1062 to the store error for the violated key.
func duplicateKind(err error) error {
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != mysqlErrDuplicate {
		return nil
	}
	if strings.Contains(me.Message, "uniq_id_label") {
		return ErrDuplicateAccountLabel
	}
	return ErrDuplicateAccount
}

func nullLabel(label string) any {
	if label == "" {
		return nil
	}
	return label
}

func (s *sqlPlayerAccountStore) Add(humanID, playerID, label string) error {
	if err := ValidatePlayerID(playerID); err != nil {
		return err
	}
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM player_accounts WHERE id = ?`, humanID); err != nil {
		return err
	}
	if n >= personalshiny.MaxPlayers {
		return ErrTooManyAccounts
	}
	// Case-insensitive uniqueness is enforced here, not by the column
	// collation (the unique key is only case-insensitive under *_ci). The
	// check-then-insert is not atomic: two concurrent adds could both pass
	// (tiny window, acceptable; the unique key still catches exact matches).
	if label != "" {
		var taken int
		if err := s.db.Get(&taken, `SELECT COUNT(*) FROM player_accounts WHERE id = ? AND LOWER(label) = LOWER(?)`,
			humanID, label); err != nil {
			return err
		}
		if taken > 0 {
			return ErrDuplicateAccountLabel
		}
	}
	if _, err := s.db.Exec(`INSERT INTO player_accounts (id, player_id, label) VALUES (?, ?, ?)`,
		humanID, playerID, nullLabel(label)); err != nil {
		if dup := duplicateKind(err); dup != nil {
			return dup
		}
		return err
	}
	return nil
}

func (s *sqlPlayerAccountStore) Remove(humanID, playerID string) error {
	res, err := s.db.Exec(`DELETE FROM player_accounts WHERE id = ? AND player_id = ?`, humanID, playerID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *sqlPlayerAccountStore) SetLabel(humanID, playerID, label string) error {
	if err := ValidateAccountLabel(label); err != nil {
		return err
	}
	var exists int
	if err := s.db.Get(&exists, `SELECT COUNT(*) FROM player_accounts WHERE id = ? AND player_id = ?`, humanID, playerID); err != nil {
		return err
	}
	if exists == 0 {
		return ErrAccountNotFound
	}
	if label != "" {
		var taken int
		if err := s.db.Get(&taken, `SELECT COUNT(*) FROM player_accounts WHERE id = ? AND LOWER(label) = LOWER(?) AND player_id <> ?`,
			humanID, label, playerID); err != nil {
			return err
		}
		if taken > 0 {
			return ErrDuplicateAccountLabel
		}
	}
	if _, err := s.db.Exec(`UPDATE player_accounts SET label = ? WHERE id = ? AND player_id = ?`,
		nullLabel(label), humanID, playerID); err != nil {
		if dup := duplicateKind(err); dup != nil {
			return dup
		}
		return err
	}
	return nil
}

func (s *sqlPlayerAccountStore) Resolve(humanID, ref string) (*db.PlayerAccount, error) {
	var row db.PlayerAccount
	err := s.db.Get(&row, `SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? AND player_id = ? LIMIT 1`, humanID, ref)
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	err = s.db.Get(&row, `SELECT `+playerAccountCols+` FROM player_accounts WHERE id = ? AND LOWER(label) = LOWER(?) LIMIT 1`, humanID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
