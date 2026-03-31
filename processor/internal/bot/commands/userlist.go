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
	var filterEnabled *bool   // nil = no filter, true = enabled only, false = disabled only
	var filterPlatform string // "" = all, "discord" or "telegram"
	var filterType string     // "" = all, or "discord:user", "discord:channel", etc.

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

	// Query humans table
	type humanRow struct {
		ID            string  `db:"id"`
		Name          string  `db:"name"`
		Type          string  `db:"type"`
		Enabled       int     `db:"enabled"`
		AdminDisable  int     `db:"admin_disable"`
	}

	var rows []humanRow
	err := ctx.DB.Select(&rows,
		"SELECT id, COALESCE(name, '') AS name, type, enabled, admin_disable FROM humans ORDER BY type, name")
	if err != nil {
		log.Errorf("userlist: query: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Apply filters
	var filtered []humanRow
	for _, h := range rows {
		// Platform filter
		if filterPlatform != "" {
			if !strings.HasPrefix(h.Type, filterPlatform+":") {
				continue
			}
		}

		// Type filter
		if filterType != "" {
			if !strings.HasSuffix(h.Type, ":"+filterType) {
				continue
			}
		}

		// Enabled filter (a user is effectively disabled if enabled=0 or admin_disable=1)
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
	sb.WriteString(fmt.Sprintf("%s (%d):\n", tr.T("cmd.userlist.registered"), len(filtered)))
	for _, h := range filtered {
		isEnabled := h.Enabled == 1 && h.AdminDisable == 0
		status := ""
		if !isEnabled {
			status = " \xF0\x9F\x9A\xAB" // prohibited sign emoji (U+1F6AB) as UTF-8
		}
		displayName := h.Name
		if displayName == "" {
			displayName = h.ID
		}
		// Show short type label
		typeLabel := h.Type
		parts := strings.SplitN(h.Type, ":", 2)
		if len(parts) == 2 {
			typeLabel = parts[1]
		}
		sb.WriteString(fmt.Sprintf("  %s — %s (%s)%s\n", h.ID, displayName, typeLabel, status))
	}
	return []bot.Reply{{Text: sb.String()}}
}
