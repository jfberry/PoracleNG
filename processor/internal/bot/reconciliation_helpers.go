package bot

import (
	"encoding/json"
	"strings"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"
)

// HaveSameContents checks if two string slices contain the same elements (order-independent).
// Used by reconciliation in both Discord and Telegram bots.
func HaveSameContents(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}

	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// ParseJSONStringSlice parses a JSON string array, returning nil on error.
func ParseJSONStringSlice(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	json.Unmarshal([]byte(s), &result)
	return result
}

// UpdateHuman updates selected fields on a human record using a dynamic UPDATE query.
func UpdateHuman(dbx *sqlx.DB, id string, updates map[string]any) {
	if len(updates) == 0 {
		return
	}

	setClauses := make([]string, 0, len(updates))
	args := make([]any, 0, len(updates)+1)
	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	args = append(args, id)

	query := "UPDATE humans SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	if _, err := dbx.Exec(query, args...); err != nil {
		log.Errorf("Update human %s: %v", id, err)
	}
}
