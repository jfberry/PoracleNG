package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

var validBackupName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// backupTables lists all tracking tables to export/import.
var backupTables = []string{
	"monsters", "raid", "egg", "quest", "invasion", "weather", "lures", "gym", "nests", "maxbattle", "forts",
}

type BackupCommand struct{}

func (c *BackupCommand) Name() string      { return "cmd.backup" }
func (c *BackupCommand) Aliases() []string { return nil }

func (c *BackupCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	tr := ctx.Tr()
	backupDir := filepath.Join(ctx.Config.BaseDir, "backups")

	// Handle "remove" subcommand
	hasRemove := false
	var filteredArgs []string
	for _, arg := range args {
		if arg == "remove" {
			hasRemove = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	if hasRemove {
		if len(filteredArgs) == 0 || !validBackupName.MatchString(filteredArgs[0]) {
			return []bot.Reply{{Text: tr.T("msg.backup.invalid_name")}}
		}
		name := filteredArgs[0]
		filePath := filepath.Join(backupDir, name+".json")
		if _, err := os.Stat(filePath); err == nil {
			if err := os.Remove(filePath); err != nil {
				log.Errorf("backup: remove %s: %v", filePath, err)
				return []bot.Reply{{React: "🙅"}}
			}
			return []bot.Reply{{React: "✅"}}
		}
		return []bot.Reply{{React: "👌"}}
	}

	// Handle "list" subcommand
	if slices.Contains(args, "list") {
		return listBackups(tr, backupDir)
	}

	// Backup: need a name
	if len(args) == 0 || !validBackupName.MatchString(args[0]) {
		return []bot.Reply{{Text: tr.T("msg.backup.need_name")}}
	}

	name := args[0]

	// Export all tracking tables
	backup := make(map[string][]map[string]any)
	for _, table := range backupTables {
		rows, err := ctx.DB.Queryx(
			fmt.Sprintf("SELECT * FROM `%s` WHERE id = ? AND profile_no = ?", table),
			ctx.TargetID, ctx.ProfileNo)
		if err != nil {
			log.Errorf("backup: query %s: %v", table, err)
			continue
		}

		var tableRows []map[string]any
		for rows.Next() {
			row := make(map[string]any)
			if err := rows.MapScan(row); err != nil {
				log.Errorf("backup: scan %s row: %v", table, err)
				continue
			}
			// Strip uid, set id=0, profile_no=0
			delete(row, "uid")
			row["id"] = "0"
			row["profile_no"] = 0
			// Convert []byte to string for JSON serialization
			for k, v := range row {
				if b, ok := v.([]byte); ok {
					row[k] = string(b)
				}
			}
			tableRows = append(tableRows, row)
		}
		rows.Close()
		if tableRows == nil {
			tableRows = []map[string]any{}
		}
		backup[table] = tableRows
	}

	// Write to file
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		log.Errorf("backup: create dir %s: %v", backupDir, err)
		return []bot.Reply{{React: "🙅"}}
	}

	data, err := json.MarshalIndent(backup, "", "\t")
	if err != nil {
		log.Errorf("backup: marshal: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if err := os.WriteFile(filepath.Join(backupDir, name+".json"), data, 0o644); err != nil {
		log.Errorf("backup: write %s: %v", name, err)
		return []bot.Reply{{React: "🙅"}}
	}

	return []bot.Reply{{React: "✅"}}
}

// listBackups returns a reply listing available backup files.
func listBackups(tr interface{ T(string) string }, backupDir string) []bot.Reply {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return []bot.Reply{{Text: tr.T("msg.restore.no_backups_dir")}}
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}

	if len(names) == 0 {
		return []bot.Reply{{Text: tr.T("msg.restore.no_backups_dir")}}
	}

	return []bot.Reply{{Text: fmt.Sprintf("%s ```\n%s```",
		tr.T("msg.restore.available"), strings.Join(names, ",\n"))}}
}
