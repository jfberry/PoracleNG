package store

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
)

var (
	ErrInvalidPlayerID       = errors.New("player id must be 1-256 ASCII letters or digits")
	ErrInvalidAccountLabel   = errors.New("account label must be at most 64 characters")
	ErrDuplicateAccount      = errors.New("player account already linked")
	ErrDuplicateAccountLabel = errors.New("account label already in use")
	ErrTooManyAccounts       = fmt.Errorf("at most %d linked accounts", personalshiny.MaxPlayers)
	ErrAccountNotFound       = errors.New("player account not found")
)

// MaxAccountLabelLen matches player_accounts.label VARCHAR(64).
const MaxAccountLabelLen = 64

// PlayerAccountStore manages a human's linked game accounts
// (player_accounts). Player IDs are case-sensitive; labels are optional,
// unique per human and compared case-insensitively.
type PlayerAccountStore interface {
	List(humanID string) ([]db.PlayerAccount, error)
	Add(humanID, playerID, label string) error
	Remove(humanID, playerID string) error
	SetLabel(humanID, playerID, label string) error
	// Resolve finds an account by exact player ID, else by label
	// (case-insensitive). Returns nil, nil when neither matches.
	Resolve(humanID, ref string) (*db.PlayerAccount, error)
}

// ValidatePlayerID accepts only 1-256 ASCII letters and digits
// (case-sensitive). This is an input rule: the personalshiny roll still
// hashes whatever bytes it is given, exactly like Golbat.
func ValidatePlayerID(id string) error {
	if id == "" || len(id) > personalshiny.MaxPlayerIDBytes {
		return ErrInvalidPlayerID
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !isAlnum {
			return ErrInvalidPlayerID
		}
	}
	return nil
}

// ValidateAccountLabel allows "" (no label) up to 64 characters.
func ValidateAccountLabel(label string) error {
	if utf8.RuneCountInString(label) > MaxAccountLabelLen {
		return ErrInvalidAccountLabel
	}
	return nil
}
