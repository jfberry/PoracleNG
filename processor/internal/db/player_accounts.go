package db

import "time"

// PlayerAccount is a game account (player ID) linked to a human, from the
// player_accounts table. PlayerID is opaque and case-sensitive.
type PlayerAccount struct {
	UID       int64     `db:"uid"`
	ID        string    `db:"id"`
	PlayerID  string    `db:"player_id"`
	Label     string    `db:"label"`
	CreatedAt time.Time `db:"created_at"`
}
