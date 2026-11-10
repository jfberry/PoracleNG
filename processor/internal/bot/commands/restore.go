package commands

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type RestoreCommand struct{}

func (c *RestoreCommand) Name() string      { return "cmd.restore" }
func (c *RestoreCommand) Aliases() []string { return nil }

func (c *RestoreCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	tr := ctx.Tr()
	backupDir := filepath.Join(ctx.Config.BaseDir, "backups")

	// Handle "list" subcommand
	if slices.Contains(args, "list") {
		return listBackups(tr, backupDir)
	}

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("msg.restore.usage")}}
	}

	name := args[0]

	// Check backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []bot.Reply{{Text: tr.T("msg.restore.no_backups_dir")}}
	}

	// List available backups for validation
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return []bot.Reply{{Text: tr.T("msg.restore.no_backups_dir")}}
	}

	var availableNames []string
	found := false
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			bName := strings.TrimSuffix(e.Name(), ".json")
			availableNames = append(availableNames, bName)
			if bName == name {
				found = true
			}
		}
	}

	if !found {
		return []bot.Reply{{Text: fmt.Sprintf("%s %s\n%s ```\n%s```",
			name, tr.T("msg.restore.not_found"),
			tr.T("msg.restore.available"),
			strings.Join(availableNames, ",\n"))}}
	}

	// Read and parse backup file
	data, err := os.ReadFile(filepath.Join(backupDir, name+".json"))
	if err != nil {
		log.Errorf("restore: read %s: %v", name, err)
		return []bot.Reply{{React: "🙅"}}
	}

	var backup map[string][]map[string]any
	if err := json.Unmarshal(data, &backup); err != nil {
		log.Errorf("restore: parse %s: %v", name, err)
		return []bot.Reply{{React: "🙅"}}
	}

	// The transaction is deliberately kept (spec §8): the per-human lock taken
	// by bot.RunLocked serialises this against API/reconciler writes, and the
	// transaction keeps the multi-table replace atomic.
	if err := restoreBackup(sqlxRestoreDB{ctx.DB}, backup, ctx.TargetID, ctx.ProfileNo); err != nil {
		log.Errorf("restore %s for %s: %v", name, ctx.TargetID, err)
		return []bot.Reply{{Text: tr.Tf("msg.restore.failed", name, err.Error())}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅"}}
}

// restoreIdent is the strict identifier shape accepted for column names read
// from a backup file before they are interpolated into SQL.
var restoreIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// restoreTx is the subset of *sqlx.Tx used by restoreBackup.
type restoreTx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

// restoreDB abstracts the database for restoreBackup so it can be tested
// without MySQL.
type restoreDB interface {
	// TableColumns returns the set of columns the table actually has.
	TableColumns(table string) (map[string]bool, error)
	Begin() (restoreTx, error)
}

type sqlxRestoreDB struct{ db *sqlx.DB }

func (d sqlxRestoreDB) TableColumns(table string) (map[string]bool, error) {
	var cols []string
	if err := d.db.Select(&cols,
		"SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
		table); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s not found", table)
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[strings.ToLower(c)] = true
	}
	return set, nil
}

func (d sqlxRestoreDB) Begin() (restoreTx, error) { return d.db.Beginx() }

// restoreBackup REPLACES the target's tracking rules for profileNo with the
// contents of backup. Every table and column name in the file is validated
// against backupTables and the table's real columns before anything is
// changed; any unknown name rejects the whole restore (legacyBackupTables
// keys from older files are ignored). Then, in a single
// transaction, every table in backupTables (whether present, empty or absent
// in the file) has the target's rows for the profile deleted and the
// backup's rows inserted with id/profile_no overwritten to the target.
func restoreBackup(db restoreDB, backup map[string][]map[string]any, targetID string, profileNo int) error {
	// Validate first — nothing is touched if anything is wrong.
	var unknown []string
	for table := range backup {
		if !slices.Contains(backupTables, table) && !slices.Contains(legacyBackupTables, table) {
			unknown = append(unknown, table)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown table(s) in backup: %s", strings.Join(unknown, ", "))
	}
	for _, table := range backupTables {
		rows := backup[table]
		if len(rows) == 0 {
			continue
		}
		cols, err := db.TableColumns(table)
		if err != nil {
			return fmt.Errorf("reading columns of %s: %w", table, err)
		}
		for _, row := range rows {
			for k := range row {
				if !restoreIdent.MatchString(k) || !cols[k] {
					return fmt.Errorf("unknown column %q in table %s", k, table)
				}
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, table := range backupTables {
		if _, err := tx.Exec(
			fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
			targetID, profileNo); err != nil {
			return fmt.Errorf("clearing %s: %w", table, err)
		}
		for _, row := range backup[table] {
			delete(row, "uid")
			row["id"] = targetID
			row["profile_no"] = profileNo

			keys := make([]string, 0, len(row))
			for k := range row {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			cols := make([]string, len(keys))
			placeholders := make([]string, len(keys))
			vals := make([]any, len(keys))
			for i, k := range keys {
				cols[i] = "`" + k + "`"
				placeholders[i] = "?"
				vals[i] = row[k]
			}
			query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
				table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
			if _, err := tx.Exec(query, vals...); err != nil {
				return fmt.Errorf("inserting into %s: %w", table, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}
