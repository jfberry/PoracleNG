package commands

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// HelpCommand implements !help — renders DTS help/greeting templates.
// !help (no args) renders type="help" id="index" (a concise command
// list operators can override), falling back to the "greeting" template
// for installs that customised greeting before help/index shipped.
// !help <command> renders the "help" DTS template with id=<command>.
type HelpCommand struct{}

// adminOnlyHelpTopics — non-admins asking "!help enable" get the 🙅
// unknown-topic reply rather than the admin command surface.
var adminOnlyHelpTopics = map[string]bool{
	"enable":    true,
	"disable":   true,
	"broadcast": true,
	"userlist":  true,
	"community": true,
	"apply":     true,
	"backup":    true,
	"restore":   true,
}

func (c *HelpCommand) Name() string      { return "cmd.help" }
func (c *HelpCommand) Aliases() []string { return nil }

func (c *HelpCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	prefix := bot.CommandPrefix(ctx)
	platform := strings.SplitN(ctx.TargetType, ":", 2)[0]
	if platform == bot.TypeWebhook {
		platform = "discord"
	}

	view := map[string]any{
		"prefix":      prefix,
		"userIsAdmin": ctx.IsAdmin,
	}

	if len(args) > 0 {
		topic := strings.ToLower(args[0])
		if adminOnlyHelpTopics[topic] && !ctx.IsAdmin {
			tr := ctx.Tr()
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.help.unknown_topic", topic, prefix)}}
		}
		return c.renderHelpTemplate(ctx, "help", topic, platform, view)
	}

	// !help (no args) — prefer the dedicated help index, fall back to the
	// greeting template as a last resort so operators with legacy
	// customised greetings keep seeing a useful response.
	//
	// Resolve empty languages to the configured default locale BEFORE
	// calling Get. Otherwise an operator who customised help with
	// `language: "en"` loses to the shipped fallback whenever the
	// caller's ctx.Language is "" (unregistered users, users who never
	// ran !language): selectEntryPass's default-flag priority level
	// (#3) requires entry-language to match the query language exactly,
	// and level #4 requires entry-language="" — both fail when the
	// query is "" and the entry is "en". Falling back to the server
	// default gives the user's "en" entry a chance to match at level 3.
	if ctx.DTS != nil {
		language := helpEffectiveLanguage(ctx)
		if ctx.DTS.Get("help", platform, "index", language) != nil {
			return c.renderHelpTemplate(ctx, "help", "index", platform, view)
		}
	}
	return c.renderHelpTemplate(ctx, "greeting", "", platform, view)
}

// helpEffectiveLanguage resolves the language to use for help template
// lookups. Priority: language hint (from language-specific command
// variants like !dasporacle) → ctx.Language → server default locale.
// Without the default-locale fallback, operators whose custom help has
// an explicit language: "en" would lose to the shipped readonly
// fallback for any caller with ctx.Language == "" (unregistered users,
// users who never ran !language).
func helpEffectiveLanguage(ctx *bot.CommandContext) string {
	if hint := ctx.GetLanguageHint(); hint != "" {
		return hint
	}
	if ctx.Language != "" {
		return ctx.Language
	}
	if ctx.Config != nil && ctx.Config.General.Locale != "" {
		return ctx.Config.General.Locale
	}
	return "en"
}

func (c *HelpCommand) renderHelpTemplate(ctx *bot.CommandContext, templateType, templateID, platform string, view map[string]any) []bot.Reply {
	if ctx.DTS == nil {
		// No DTS system — fall back to i18n text
		tr := ctx.Tr()
		prefix := bot.CommandPrefix(ctx)
		return []bot.Reply{{Text: tr.Tf("msg.help.text", prefix)}}
	}

	language := helpEffectiveLanguage(ctx)

	// Look up the compiled template
	var tmpl = ctx.DTS.Get(templateType, platform, templateID, language)
	if tmpl == nil && templateID == "" {
		// For greeting with no ID, try common defaults
		tmpl = ctx.DTS.Get(templateType, platform, "1", language)
		if tmpl == nil {
			tmpl = ctx.DTS.Get(templateType, platform, "default", language)
		}
	}

	if tmpl == nil {
		if templateID != "" {
			// Specific help topic not found — try the usage key for this command
			tr := ctx.Tr()
			prefix := bot.CommandPrefix(ctx)
			usageKey := "cmd." + templateID + ".usage"
			text := tr.Tf(usageKey, prefix)
			if text != usageKey {
				return []bot.Reply{{Text: text}}
			}
			// Unknown topic
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.help.unknown_topic", templateID, prefix)}}
		}
		// No greeting template
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.Tf("msg.help.no_greeting", bot.CommandPrefix(ctx))}}
	}

	// Render the template
	result, err := tmpl.Exec(view)
	if err != nil {
		log.Warnf("help: render %s/%s: %v", templateType, templateID, err)
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.Tf("msg.help.text", bot.CommandPrefix(ctx))}}
	}

	// Parse the rendered JSON
	if platform == "discord" {
		// For Discord: return the rendered JSON as an embed
		var msg map[string]any
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			// Not valid JSON — treat as plain text
			return []bot.Reply{{Text: result}}
		}

		// Clear title/description (Discord requires the keys to exist) and
		// drop fields whose name+value both rendered empty — lets template
		// authors gate whole sections via {{#if ...}} around the strings.
		if embed, ok := msg["embed"].(map[string]any); ok {
			if _, has := embed["title"]; has {
				embed["title"] = ""
			}
			if _, has := embed["description"]; has {
				embed["description"] = ""
			}
			if rawFields, ok := embed["fields"].([]any); ok {
				var keep []any
				for _, f := range rawFields {
					field, ok := f.(map[string]any)
					if !ok {
						continue
					}
					name, _ := field["name"].(string)
					value, _ := field["value"].(string)
					if strings.TrimSpace(name) == "" && strings.TrimSpace(value) == "" {
						continue
					}
					keep = append(keep, field)
				}
				if len(keep) > 0 {
					embed["fields"] = keep
				} else {
					delete(embed, "fields")
				}
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

	// Build text from embed fields, splitting at 1024 chars per message.
	// Empty-on-both-sides fields drop out so {{#if}}-gated sections
	// don't leave blank gaps in the Telegram output.
	var replies []bot.Reply
	var current strings.Builder
	for _, f := range fields {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		value, _ := field["value"].(string)
		if strings.TrimSpace(name) == "" && strings.TrimSpace(value) == "" {
			continue
		}
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
