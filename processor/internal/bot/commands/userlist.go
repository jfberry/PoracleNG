package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// UserlistCommand implements !userlist — admin-only listing of registered users.
type UserlistCommand struct{}

func (c *UserlistCommand) Name() string      { return "cmd.userlist" }
func (c *UserlistCommand) Aliases() []string { return nil }

func (c *UserlistCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if !ctx.IsAdmin && !ctx.IsCommunityAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_permission")}}
	}

	// Parse filter arguments
	var filterEnabled *bool
	var filterPlatform string
	var filterType string

	for _, arg := range args {
		switch strings.ToLower(arg) {
		case "enabled":
			v := true
			filterEnabled = &v
		case "disabled":
			v := false
			filterEnabled = &v
		case "discord":
			filterPlatform = "discord"
		case "telegram":
			filterPlatform = "telegram"
		case "user":
			filterType = "user"
		case "channel":
			filterType = "channel"
		case "group":
			filterType = "group"
		case "webhook":
			filterType = "webhook"
		}
	}

	type humanRow struct {
		ID           string  `db:"id"`
		Name         string  `db:"name"`
		Type         string  `db:"type"`
		Enabled      int     `db:"enabled"`
		AdminDisable int     `db:"admin_disable"`
		Area         *string `db:"area"`
	}

	var rows []humanRow
	err := ctx.DB.Select(&rows,
		"SELECT id, COALESCE(name, '') AS name, type, enabled, admin_disable, area FROM humans ORDER BY type, name")
	if err != nil {
		log.Errorf("userlist: query: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var filtered []humanRow
	for _, h := range rows {
		if filterPlatform != "" && !strings.HasPrefix(h.Type, filterPlatform+":") && h.Type != filterPlatform {
			continue
		}
		if filterType != "" {
			if filterType == "webhook" {
				if h.Type != "webhook" {
					continue
				}
			} else if !strings.HasSuffix(h.Type, ":"+filterType) {
				continue
			}
		}
		isEnabled := h.Enabled == 1 && h.AdminDisable == 0
		if filterEnabled != nil {
			if *filterEnabled && !isEnabled {
				continue
			}
			if !*filterEnabled && isEnabled {
				continue
			}
		}
		filtered = append(filtered, h)
	}

	if len(filtered) == 0 {
		return []bot.Reply{{Text: tr.T("cmd.userlist.none")}}
	}

	var sb strings.Builder
	sb.WriteString(tr.T("cmd.userlist.registered"))
	sb.WriteByte('\n')

	for _, h := range filtered {
		isEnabled := h.Enabled == 1 && h.AdminDisable == 0
		disabled := ""
		if !isEnabled {
			disabled = " 🚫"
		}

		area := "[]"
		if h.Area != nil && *h.Area != "" {
			area = *h.Area
		}

		if h.Type == "webhook" {
			// Webhooks: just show "webhook • name" (no ID, no URL)
			sb.WriteString(fmt.Sprintf("webhook • %s%s\n", h.Name, disabled))
		} else {
			// Users/channels/groups: "type • name [username] | (id) [areas]"
			displayName := h.Name
			if displayName == "" {
				displayName = h.ID
			}
			sb.WriteString(fmt.Sprintf("%s • %s | (%s) %s%s\n",
				h.Type, displayName, h.ID, area, disabled))
		}
	}

	text := sb.String()
	if len(text) > 2000 {
		return []bot.Reply{{
			Text: tr.T("cmd.userlist.registered"),
			Attachment: &bot.Attachment{
				Filename: "userlist.txt",
				Content:  []byte(text),
			},
		}}
	}

	return []bot.Reply{{Text: text}}
}
