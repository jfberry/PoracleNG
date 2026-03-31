package commands

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// HelpCommand implements !help — renders DTS help/greeting templates.
// !help (no args) renders the "greeting" DTS template.
// !help <command> renders the "help" DTS template with id=<command>.
type HelpCommand struct{}

func (c *HelpCommand) Name() string      { return "cmd.help" }
func (c *HelpCommand) Aliases() []string { return nil }

func (c *HelpCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	prefix := commandPrefix(ctx)
	platform := strings.SplitN(ctx.TargetType, ":", 2)[0]
	if platform == "webhook" {
		platform = "discord"
	}

	// View data for template rendering
	view := map[string]any{
		"prefix": prefix,
	}

	if len(args) > 0 {
		// !help <command> — render help DTS for that command
		return c.renderHelpTemplate(ctx, "help", args[0], platform, view)
	}

	// !help (no args) — render greeting DTS
	return c.renderHelpTemplate(ctx, "greeting", "", platform, view)
}

func (c *HelpCommand) renderHelpTemplate(ctx *bot.CommandContext, templateType, templateID, platform string, view map[string]any) []bot.Reply {
	if ctx.DTS == nil {
		// No DTS system — fall back to i18n text
		tr := ctx.Tr()
		prefix := commandPrefix(ctx)
		return []bot.Reply{{Text: tr.Tf("cmd.help.text", prefix)}}
	}

	language := ctx.Language

	// Look up the template — the TemplateStore.Get handles the fallback chain:
	// 1. type + platform + id + language
	// 2. type + platform + id (any language)
	// 3. type + platform + default + language
	// 4. type + platform + default
	// 5. type + any platform + id
	var tmpl interface{ Exec(ctx interface{}) (string, error) }

	if templateID != "" {
		tmpl = ctx.DTS.Get(templateType, platform, templateID, language)
	} else {
		// For greeting, use default template
		tmpl = ctx.DTS.Get(templateType, platform, "1", language)
		if tmpl == nil {
			tmpl = ctx.DTS.Get(templateType, platform, "default", language)
		}
	}

	if tmpl == nil {
		// No template found — fall back to i18n
		tr := ctx.Tr()
		prefix := commandPrefix(ctx)
		if templateID != "" {
			// Try the usage key for this specific command
			usageKey := "cmd." + templateID + ".usage"
			text := tr.Tf(usageKey, prefix)
			if text != usageKey {
				return []bot.Reply{{Text: text}}
			}
		}
		return []bot.Reply{{Text: tr.Tf("cmd.help.text", prefix)}}
	}

	// Render the template
	result, err := tmpl.Exec(view)
	if err != nil {
		log.Warnf("help: render %s/%s: %v", templateType, templateID, err)
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.Tf("cmd.help.text", commandPrefix(ctx))}}
	}

	// Parse the rendered JSON
	if platform == "discord" {
		// For Discord: return the rendered JSON as an embed
		var msg map[string]any
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			// Not valid JSON — treat as plain text
			return []bot.Reply{{Text: result}}
		}

		// For help/greeting templates, clear title and description if present
		// (set to empty string, not delete — Discord requires the field to exist)
		if embed, ok := msg["embed"].(map[string]any); ok {
			if _, has := embed["title"]; has {
				embed["title"] = ""
			}
			if _, has := embed["description"]; has {
				embed["description"] = ""
			}
		}

		embedJSON, err := json.Marshal(msg)
		if err != nil {
			return []bot.Reply{{Text: result}}
		}
		return []bot.Reply{{Embed: embedJSON}}
	}

	// For Telegram: extract embed fields and send as text
	var msg map[string]any
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		return []bot.Reply{{Text: result}}
	}

	embed, _ := msg["embed"].(map[string]any)
	if embed == nil {
		return []bot.Reply{{Text: result}}
	}

	fields, _ := embed["fields"].([]any)
	if len(fields) == 0 {
		// No fields — try description
		if desc, ok := embed["description"].(string); ok && desc != "" {
			return []bot.Reply{{Text: desc}}
		}
		return []bot.Reply{{Text: result}}
	}

	// Build text from embed fields, splitting at 1024 chars per message
	var replies []bot.Reply
	var current strings.Builder
	for _, f := range fields {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		value, _ := field["value"].(string)
		fieldText := "\n\n" + name + "\n\n" + value

		if current.Len()+len(fieldText) > 1024 && current.Len() > 0 {
			replies = append(replies, bot.Reply{Text: current.String()})
			current.Reset()
		}
		current.WriteString(fieldText)
	}
	if current.Len() > 0 {
		replies = append(replies, bot.Reply{Text: current.String()})
	}

	return replies
}
