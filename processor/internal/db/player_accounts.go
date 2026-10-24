package db

import (
	"sort"
	"time"

	"github.com/jmoiron/sqlx"
)

// PlayerAccount is a game account (player ID) linked to a human, from the
// player_accounts table. PlayerID is opaque and case-sensitive.
type PlayerAccount struct {
	UID       int64     `db:"uid"`
	ID        string    `db:"id"`
	PlayerID  string    `db:"player_id"`
	Label     string    `db:"label"`
	CreatedAt time.Time `db:"created_at"`
}

// LoadPlayerAccounts loads every linked account, grouped by human id,
// oldest first.
func LoadPlayerAccounts(dbx *sqlx.DB) (map[string][]PlayerAccount, error) {
	var rows []PlayerAccount
	if err := dbx.Select(&rows,
		`SELECT uid, id, player_id, COALESCE(label, '') AS label, created_at FROM player_accounts`); err != nil {
		return nil, err
	}
	return GroupPlayerAccounts(rows), nil
}

// GroupPlayerAccounts groups rows by human id, each group ordered by
// created_at then uid.
func GroupPlayerAccounts(rows []PlayerAccount) map[string][]PlayerAccount {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].UID < rows[j].UID
	})
	out := make(map[string][]PlayerAccount)
	for _, r := range rows {
		out[r.ID] = append(out[r.ID], r)
	}
	return out
}
