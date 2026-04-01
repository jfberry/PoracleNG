package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// EnableCommand implements !enable — admin enables user(s).
type EnableCommand struct{}

func (c *EnableCommand) Name() string      { return "cmd.enable" }
func (c *EnableCommand) Aliases() []string { return nil }

func (c *EnableCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_permission")}}
	}

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.enable.specify")}}
	}

	var enabled []string
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
		if err := ctx.Humans.SetAdminDisable(id, false); err != nil {
			log.Errorf("enable: %v", err)
			continue
		}
		enabled = append(enabled, id)
	}

	if len(enabled) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.enable.success", strings.Join(enabled, ", "))}}
}

// DisableCommand implements !disable — admin disables user(s).
type DisableCommand struct{}

func (c *DisableCommand) Name() string      { return "cmd.disable" }
func (c *DisableCommand) Aliases() []string { return nil }

func (c *DisableCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_permission")}}
	}

	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.enable.specify")}}
	}

	var disabled []string
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
		if err := ctx.Humans.SetAdminDisable(id, true); err != nil {
			log.Errorf("disable: %v", err)
			continue
		}
		disabled = append(disabled, id)
	}

	if len(disabled) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.disable.success", strings.Join(disabled, ", "))}}
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
