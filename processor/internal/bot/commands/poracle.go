package commands

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// PoracleCommand implements !poracle -- user registration.
// This command bypasses the normal registration check (handled by the bot handlers).
// It validates the CHANNEL the message was sent in, not the user.
type PoracleCommand struct{}

func (c *PoracleCommand) Name() string      { return "cmd.poracle" }
func (c *PoracleCommand) Aliases() []string { return nil }

func (c *PoracleCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	// Telegram: only works in groups, not DMs
	if ctx.Platform == "telegram" && ctx.IsDM {
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("msg.poracle.dm_only_telegram")}}
	}

	// Validate channel is a registration channel
	if !bot.IsRegistrationChannel(ctx.Config, ctx.Platform, ctx.ChannelID) {
		log.Infof("poracle: %s tried to register in channel %s (not a registration channel)", ctx.UserName, ctx.ChannelID)
		tr := ctx.Tr()
		return []bot.Reply{{Text: tr.T("msg.poracle.wrong_channel")}}
	}

	// Determine community to add (if area security is enabled)
	var communityToAdd string
	if ctx.Config.Area.Enabled {
		communityToAdd = bot.FindCommunityForChannel(ctx.Config, ctx.Platform, ctx.ChannelID)
		if communityToAdd == "" {
			log.Infof("poracle: %s tried to register in channel %s (no community found)", ctx.UserName, ctx.ChannelID)
			return nil
		}
		communityToAdd = strings.ToLower(communityToAdd)
	}

	// Check if user already exists
	existing, err := ctx.Humans.Get(ctx.UserID)
	if err != nil {
		log.Errorf("poracle: select human %s: %v", ctx.UserID, err)
		return []bot.Reply{{React: "🙅"}}
	}

	if existing != nil {
		return c.handleExistingUser(ctx, existing, communityToAdd)
	}
	return c.handleNewUser(ctx, communityToAdd)
}

func (c *PoracleCommand) handleExistingUser(ctx *bot.CommandContext, human *store.Human, communityToAdd string) []bot.Reply {
	// Admin-disabled without disabled_date: hard block, don't allow re-enable
	if human.AdminDisable && !human.DisabledDate.Valid {
		return []bot.Reply{{React: "🙅"}}
	}

	fields := make(map[string]any)

	// Re-enable if disabled
	if !human.Enabled {
		fields["enabled"] = 1
		fields["fails"] = 0
	}

	// Clear admin_disable if role_check_mode is "disable-user" and has disabled_date
	if ctx.Config.General.RoleCheckMode == "disable-user" {
		if human.AdminDisable && human.DisabledDate.Valid {
			fields["admin_disable"] = 0
			fields["disabled_date"] = nil
			log.Debugf("poracle: user %s re-enabled via poracle command (admin_disable cleared)", ctx.UserID)
		}
	}

	// Add community if area security is enabled
	if communityToAdd != "" {
		newCommunities := bot.AddCommunity(ctx.Config, human.CommunityMembership, communityToAdd)
		newRestrictions := bot.CalculateLocationRestrictions(ctx.Config, newCommunities)

		communityJSON, _ := json.Marshal(newCommunities)
		restrictionJSON, _ := json.Marshal(newRestrictions)

		fields["community_membership"] = string(communityJSON)
		fields["area_restriction"] = string(restrictionJSON)
	}

	if len(fields) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := ctx.Humans.Update(ctx.UserID, fields); err != nil {
		log.Errorf("poracle: update human %s: %v", ctx.UserID, err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	log.Infof("poracle: %s re-registered", ctx.UserName)

	replies := []bot.Reply{{React: "✅"}}
	if greeting := c.renderGreeting(ctx); greeting != nil {
		replies = append(replies, *greeting)
	}
	return replies
}

func (c *PoracleCommand) handleNewUser(ctx *bot.CommandContext, communityToAdd string) []bot.Reply {
	// Determine user type
	userType := bot.TypeDiscordUser
	if ctx.Platform == "telegram" {
		userType = bot.TypeTelegramUser
	}

	// Build community membership
	var communities []string
	var restrictions []string
	if communityToAdd != "" {
		communities = []string{communityToAdd}
		restrictions = bot.CalculateLocationRestrictions(ctx.Config, communities)
	}

	human := &store.Human{
		ID:                  ctx.UserID,
		Name:                ctx.UserName,
		Type:                userType,
		Enabled:             true,
		CommunityMembership: communities,
		AreaRestriction:     restrictions,
	}

	if err := ctx.Humans.Create(human); err != nil {
		log.Errorf("poracle: create human %s: %v", ctx.UserID, err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Create default profile
	if err := ctx.Humans.CreateDefaultProfile(ctx.UserID, ctx.UserName, nil, 0, 0); err != nil {
		log.Errorf("poracle: create default profile for %s: %v", ctx.UserID, err)
		// Don't fail the registration, the human was created
	}

	ctx.TriggerReload()
	log.Infof("poracle: %s registered as %s", ctx.UserName, userType)

	replies := []bot.Reply{{React: "✅"}}

	// Group welcome text — sent to the group (not DM) with {user} replaced
	if ctx.Platform == "telegram" && ctx.Config.Telegram.GroupWelcomeText != "" && !ctx.IsDM {
		welcome := strings.ReplaceAll(ctx.Config.Telegram.GroupWelcomeText, "{user}", ctx.UserName)
		replies = append(replies, bot.Reply{Text: welcome})
	}

	// Bot welcome text — custom DM message on registration (Telegram)
	if ctx.Platform == "telegram" && ctx.Config.Telegram.BotWelcomeText != "" {
		replies = append(replies, bot.Reply{Text: ctx.Config.Telegram.BotWelcomeText, IsDM: true})
	}

	// Greeting DTS — sent via DM
	if greeting := c.renderGreeting(ctx); greeting != nil {
		replies = append(replies, *greeting)
	}
	return replies
}

// renderGreeting renders the greeting DTS template and returns a DM reply.
func (c *PoracleCommand) renderGreeting(ctx *bot.CommandContext) *bot.Reply {
	if ctx.DTS == nil {
		return nil
	}

	prefix := commandPrefix(ctx)
	platform := ctx.Platform
	if platform == bot.TypeWebhook {
		platform = "discord"
	}

	view := map[string]any{
		"prefix": prefix,
	}

	// Look up the greeting template
	tmpl := ctx.DTS.Get("greeting", platform, "", ctx.Language)
	if tmpl == nil {
		tmpl = ctx.DTS.Get("greeting", platform, "1", ctx.Language)
	}
	if tmpl == nil {
		tmpl = ctx.DTS.Get("greeting", platform, "default", ctx.Language)
	}
	if tmpl == nil {
		return nil
	}

	result, err := tmpl.Exec(view)
	if err != nil {
		log.Warnf("poracle: render greeting: %v", err)
		return nil
	}

	if platform == "discord" {
		// Return as embed DM
		var msg map[string]any
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			return &bot.Reply{Text: result, IsDM: true}
		}
		embedJSON, err := json.Marshal(msg)
		if err != nil {
			return &bot.Reply{Text: result, IsDM: true}
		}
		return &bot.Reply{Embed: embedJSON, IsDM: true}
	}

	// Telegram: extract fields from embed and send as text DM
	var msg map[string]any
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		return &bot.Reply{Text: result, IsDM: true}
	}

	embed, _ := msg["embed"].(map[string]any)
	if embed == nil {
		return &bot.Reply{Text: result, IsDM: true}
	}

	fields, _ := embed["fields"].([]any)
	if len(fields) == 0 {
		if desc, ok := embed["description"].(string); ok && desc != "" {
			return &bot.Reply{Text: desc, IsDM: true}
		}
		return nil
	}

	var sb strings.Builder
	for _, f := range fields {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		value, _ := field["value"].(string)
		sb.WriteString("\n\n")
		sb.WriteString(name)
		sb.WriteString("\n\n")
		sb.WriteString(value)
	}
	if sb.Len() == 0 {
		return nil
	}
	return &bot.Reply{Text: sb.String(), IsDM: true}
}
