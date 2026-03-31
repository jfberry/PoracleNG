package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	for _, arg := range args {
		if arg == "list" {
			return listBackups(tr, backupDir)
		}
	}

	if len(args) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.restore.usage")}}
	}

	name := args[0]

	// Check backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []bot.Reply{{Text: tr.T("cmd.restore.no_backups_dir")}}
	}

	// List available backups for validation
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return []bot.Reply{{Text: tr.T("cmd.restore.no_backups_dir")}}
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
			name, tr.T("cmd.restore.not_found"),
			tr.T("cmd.restore.available"),
			strings.Join(availableNames, ",\n"))}}
	}

	// Read and parse backup file
	data, err := os.ReadFile(filepath.Join(backupDir, name+".json"))
	if err != nil {
		log.Errorf("restore: read %s: %v", name, err)
		return []bot.Reply{{React: "🙅"}}
	}

	var backup map[string][]map[string]interface{}
	if err := json.Unmarshal(data, &backup); err != nil {
		log.Errorf("restore: parse %s: %v", name, err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Restore each table
	for table, rows := range backup {
		// Set id and profile_no on each row
		for _, row := range rows {
			row["id"] = ctx.TargetID
			row["profile_no"] = ctx.ProfileNo
		}

		if len(rows) == 0 {
			continue
		}

		// Delete existing tracking for this profile
		_, err := ctx.DB.Exec(
			fmt.Sprintf("DELETE FROM `%s` WHERE id = ? AND profile_no = ?", table),
			ctx.TargetID, ctx.ProfileNo)
		if err != nil {
			log.Errorf("restore: delete from %s: %v", table, err)
			continue
		}

		// Insert rows one at a time using column names from the row map
		for _, row := range rows {
			// Remove uid if present
			delete(row, "uid")

			var cols []string
			var placeholders []string
			var vals []interface{}
			for k, v := range row {
				cols = append(cols, fmt.Sprintf("`%s`", k))
				placeholders = append(placeholders, "?")
				vals = append(vals, v)
			}

			query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
				table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
			if _, err := ctx.DB.Exec(query, vals...); err != nil {
				log.Warnf("restore: insert into %s: %v", table, err)
			}
		}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅"}}
}
