package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// UserlistCommand implements !userlist — admin-only listing of registered users.
type UserlistCommand struct{}

func (c *UserlistCommand) Name() string      { return "cmd.userlist" }
func (c *UserlistCommand) Aliases() []string { return nil }

func (c *UserlistCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if !ctx.IsAdmin && !ctx.IsCommunityAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_permission")}}
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

	allHumans, err := ctx.Humans.ListAll()
	if err != nil {
		log.Errorf("userlist: query: %v", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var filtered []*store.Human
	for _, h := range allHumans {
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
		isEnabled := h.Enabled && !h.AdminDisable
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
		return []bot.Reply{{Text: tr.T("msg.userlist.none")}}
	}

	var sb strings.Builder
	sb.WriteString(tr.T("msg.userlist.registered"))
	sb.WriteByte('\n')

	for _, h := range filtered {
		status := ""
		if h.AdminDisable && !h.Enabled {
			status = " ⛔stopped+disabled"
		} else if h.AdminDisable {
			status = " ⛔disabled"
		} else if !h.Enabled {
			status = " 🛑stopped"
		}

		areaJSON, _ := json.Marshal(h.Area)
		area := string(areaJSON)
		if area == "null" {
			area = "[]"
		}

		if h.Type == "webhook" {
			sb.WriteString(fmt.Sprintf("webhook • %s%s\n", h.Name, status))
		} else {
			displayName := h.Name
			if displayName == "" {
				displayName = h.ID
			}
			sb.WriteString(fmt.Sprintf("%s • %s | (%s) %s%s\n",
				h.Type, displayName, h.ID, area, status))
		}
	}

	return bot.SplitTextReply(sb.String())
}
