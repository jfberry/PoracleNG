package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/humanlock"
)

// UnregisterCommand implements !unregister -- delete user registration and tracking.
// Non-admin: can only unregister self.
// Admin: can unregister others by mention or ID. Will NOT unregister self (safety).
type UnregisterCommand struct{}

func (c *UnregisterCommand) Name() string      { return "cmd.unregister" }
func (c *UnregisterCommand) Aliases() []string { return nil }

// ExemptFromTargetLock: an admin !unregister deletes the humans it names, not
// its own target, so Run locks each deleted human itself (spec §3.6).
func (c *UnregisterCommand) ExemptFromTargetLock() {}

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
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.unregister.admin_no_targets")}}
		}
		// Safety: filter out self
		var filtered []string
		for _, t := range targets {
			if t != ctx.UserID {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.unregister.admin_no_targets")}}
		}
		targets = filtered
	} else {
		// Non-admin: unregister self only
		targets = []string{ctx.TargetID}
	}

	var unregistered, unknown []string
	for _, id := range targets {
		registered, err := deleteHumanLocked(ctx, id)
		if !registered {
			unknown = append(unknown, id)
			continue
		}
		if err != nil {
			log.Errorf("unregister: delete %s: %v", id, err)
			continue
		}
		log.Infof("unregister: %s unregistered %s", ctx.UserID, id)
		unregistered = append(unregistered, id)
	}

	if len(unregistered) > 0 {
		ctx.TriggerReload()
	}

	if !ctx.IsAdmin {
		if len(unregistered) == 0 {
			return []bot.Reply{{React: "👌"}}
		}
		// Self-unregister: friendly goodbye
		return []bot.Reply{{React: "✅", Text: tr.T("msg.unregister.goodbye")}}
	}
	// Admin unregistering others: show IDs
	return adminTargetsReply(tr, "msg.unregister.success", unregistered, unknown)
}

// deleteHumanLocked deletes id (and its tracking) while holding id's write
// lock. registered is false (and nothing is deleted) when id has no humans row.
func deleteHumanLocked(ctx *bot.CommandContext, id string) (registered bool, err error) {
	unlock := humanlock.Lock(id)
	defer unlock()
	if !humanRegistered(ctx, id) {
		return false, nil
	}
	return true, ctx.Humans.Delete(id)
}
