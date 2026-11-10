package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/humanlock"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// EnableCommand implements !enable — admin enables user(s).
type EnableCommand struct{}

func (c *EnableCommand) Name() string      { return "cmd.enable" }
func (c *EnableCommand) Aliases() []string { return nil }

// ExemptFromTargetLock: !enable writes the humans it names, not its own
// target; it locks each of them around its write (spec §3.6).
func (c *EnableCommand) ExemptFromTargetLock() {}

func (c *EnableCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_permission")}}
	}

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.enable.specify")}}
	}

	var enabled, unknown []string
	for _, arg := range args {
		// Strip mention formatting
		id := stripMention(arg)
		if id == "" {
			continue
		}
		// If not a numeric ID, try to resolve as a webhook name
		if !isNumeric(id) {
			if webhookID, err := ctx.Humans.LookupWebhookByName(id); err == nil && webhookID != "" {
				id = webhookID
			}
		}
		// Clears admin_disable and also sets enabled = 1, so this lifts a
		// rate-limit soft ban (enabled = 0) as well as a hard one.
		registered, err := setAdminDisableLocked(ctx, id, false)
		if !registered {
			unknown = append(unknown, id)
			continue
		}
		if err != nil {
			log.Errorf("enable: %v", err)
			continue
		}
		clearRateLimitBan(ctx, id)
		enabled = append(enabled, id)
	}

	if len(enabled) > 0 {
		ctx.TriggerReload()
	}
	return adminTargetsReply(tr, "msg.enable.success", enabled, unknown)
}

// DisableCommand implements !disable — admin disables user(s).
type DisableCommand struct{}

func (c *DisableCommand) Name() string      { return "cmd.disable" }
func (c *DisableCommand) Aliases() []string { return nil }

// ExemptFromTargetLock: see EnableCommand.
func (c *DisableCommand) ExemptFromTargetLock() {}

func (c *DisableCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_permission")}}
	}

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.enable.specify")}}
	}

	var disabled, unknown []string
	for _, arg := range args {
		id := stripMention(arg)
		if id == "" {
			continue
		}
		// If not a numeric ID, try to resolve as a webhook name
		if !isNumeric(id) {
			if webhookID, err := ctx.Humans.LookupWebhookByName(id); err == nil && webhookID != "" {
				id = webhookID
			}
		}
		registered, err := setAdminDisableLocked(ctx, id, true)
		if !registered {
			unknown = append(unknown, id)
			continue
		}
		if err != nil {
			log.Errorf("disable: %v", err)
			continue
		}
		disabled = append(disabled, id)
	}

	if len(disabled) > 0 {
		ctx.TriggerReload()
	}
	return adminTargetsReply(tr, "msg.disable.success", disabled, unknown)
}

// setAdminDisableLocked sets admin_disable on id while holding id's write
// lock. registered is false (and nothing is written) when id has no humans row.
func setAdminDisableLocked(ctx *bot.CommandContext, id string, disable bool) (registered bool, err error) {
	unlock := humanlock.Lock(id)
	defer unlock()
	if !humanRegistered(ctx, id) {
		return false, nil
	}
	return true, ctx.Humans.SetAdminDisable(id, disable)
}

// humanRegistered reports whether id has a humans row.
func humanRegistered(ctx *bot.CommandContext, id string) bool {
	h, err := ctx.Humans.GetLite(id)
	if err != nil {
		log.Errorf("lookup human %s: %v", id, err)
		return false
	}
	return h != nil
}

// clearRateLimitBan forgets a re-enabled target's rate-limit breach history
// so the ban re-arms: without it a banned user who is re-enabled can't be
// banned again until the 24h violation window ends.
func clearRateLimitBan(ctx *bot.CommandContext, id string) {
	if ctx.Admin != nil && ctx.Admin.AlertLimiter != nil {
		ctx.Admin.AlertLimiter.ClearViolations(id)
	}
}

// adminTargetsReply reports an admin action over several IDs: the ones it
// applied to (successKey) and any that aren't registered.
func adminTargetsReply(tr *i18n.Translator, successKey string, done, unknown []string) []bot.Reply {
	if len(done) == 0 && len(unknown) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	var lines []string
	if len(done) > 0 {
		lines = append(lines, tr.Tf(successKey, strings.Join(done, ", ")))
	}
	if len(unknown) > 0 {
		lines = append(lines, tr.Tf("msg.user.not_registered", strings.Join(unknown, ", ")))
	}
	react := "✅"
	if len(done) == 0 {
		react = "🙅"
	}
	return []bot.Reply{{React: react, Text: strings.Join(lines, "\n")}}
}

// isNumeric returns true if the string contains only digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// stripMention removes Discord mention formatting: <@123> → 123, <@!123> → 123
func stripMention(s string) string {
	s = strings.TrimPrefix(s, "<@")
	s = strings.TrimPrefix(s, "!")
	s = strings.TrimSuffix(s, ">")
	if s == "" {
		return ""
	}
	// Validate it's numeric
	for _, c := range s {
		if c < '0' || c > '9' {
			return s // might be a name, pass through
		}
	}
	return s
}
