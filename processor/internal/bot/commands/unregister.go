package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// UnregisterCommand implements !unregister -- delete user registration and tracking.
// Non-admin: can only unregister self.
// Admin: can unregister others by mention or ID. Will NOT unregister self (safety).
type UnregisterCommand struct{}

func (c *UnregisterCommand) Name() string      { return "cmd.unregister" }
func (c *UnregisterCommand) Aliases() []string { return nil }

func (c *UnregisterCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	var targets []string

	if ctx.IsAdmin {
		// Admin: extract mentions and numeric IDs from args
		for _, arg := range args {
			id := stripMention(arg)
			if id == "" {
				continue
			}
			targets = append(targets, id)
		}
		// Safety: admin with no targets won't unregister self
		if len(targets) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.unregister.admin_no_targets")}}
		}
		// Safety: filter out self
		var filtered []string
		for _, t := range targets {
			if t != ctx.UserID {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.unregister.admin_no_targets")}}
		}
		targets = filtered
	} else {
		// Non-admin: unregister self only
		targets = []string{ctx.TargetID}
	}

	var unregistered []string
	for _, id := range targets {
		if err := db.DeleteHumanAndTracking(ctx.DB, id); err != nil {
			log.Errorf("unregister: delete %s: %v", id, err)
			continue
		}
		log.Infof("unregister: %s unregistered %s", ctx.UserID, id)
		unregistered = append(unregistered, id)
	}

	if len(unregistered) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.unregister.success", strings.Join(unregistered, ", "))}}
}
