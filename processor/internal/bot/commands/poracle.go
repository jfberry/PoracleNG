package commands

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
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
		return []bot.Reply{{Text: tr.T("cmd.poracle.dm_only_telegram")}}
	}

	// Validate channel is a registration channel
	if !bot.IsRegistrationChannel(ctx.Config, ctx.Platform, ctx.ChannelID) {
		log.Infof("poracle: %s tried to register in channel %s (not a registration channel)", ctx.UserName, ctx.ChannelID)
		return nil // silently ignore, matching alerter behavior
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
	existing, err := db.SelectOneHumanFull(ctx.DB, ctx.UserID)
	if err != nil {
		log.Errorf("poracle: select human %s: %v", ctx.UserID, err)
		return []bot.Reply{{React: "🙅"}}
	}

	if existing != nil {
		return c.handleExistingUser(ctx, existing, communityToAdd)
	}
	return c.handleNewUser(ctx, communityToAdd)
}

func (c *PoracleCommand) handleExistingUser(ctx *bot.CommandContext, human *db.HumanFull, communityToAdd string) []bot.Reply {
	// Admin-disabled without disabled_date: hard block, don't allow re-enable
	if human.AdminDisable == 1 && !human.DisabledDate.Valid {
		return []bot.Reply{{React: "🙅"}}
	}

	updateRequired := false
	setClauses := make([]string, 0, 4)
	setArgs := make([]interface{}, 0, 4)

	// Re-enable if disabled
	if human.Enabled == 0 {
		setClauses = append(setClauses, "enabled = ?", "fails = ?")
		setArgs = append(setArgs, 1, 0)
		updateRequired = true
	}

	// Clear admin_disable if role_check_mode is "disable-user" and has disabled_date
	if ctx.Config.General.RoleCheckMode == "disable-user" {
		if human.AdminDisable == 1 && human.DisabledDate.Valid {
			setClauses = append(setClauses, "admin_disable = ?", "disabled_date = ?")
			setArgs = append(setArgs, 0, nil)
			updateRequired = true
			log.Debugf("poracle: user %s re-enabled via poracle command (admin_disable cleared)", ctx.UserID)
		}
	}

	// Add community if area security is enabled
	if communityToAdd != "" {
		existingCommunities := bot.ParseCommunityMembership(human.CommunityMembership)
		newCommunities := bot.AddCommunity(ctx.Config, existingCommunities, communityToAdd)
		newRestrictions := bot.CalculateLocationRestrictions(ctx.Config, newCommunities)

		communityJSON, _ := json.Marshal(newCommunities)
		restrictionJSON, _ := json.Marshal(newRestrictions)

		setClauses = append(setClauses, "community_membership = ?", "area_restriction = ?")
		setArgs = append(setArgs, string(communityJSON), string(restrictionJSON))
		updateRequired = true
	}

	if !updateRequired {
		return []bot.Reply{{React: "👌"}}
	}

	// Build and execute update query
	query := "UPDATE humans SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	setArgs = append(setArgs, ctx.UserID)
	if _, err := ctx.DB.Exec(query, setArgs...); err != nil {
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
	communityMembership := "[]"
	var areaRestrictionStr *string
	if communityToAdd != "" {
		communities := []string{communityToAdd}
		communityJSON, _ := json.Marshal(communities)
		communityMembership = string(communityJSON)

		restrictions := bot.CalculateLocationRestrictions(ctx.Config, communities)
		restrictionJSON, _ := json.Marshal(restrictions)
		s := string(restrictionJSON)
		areaRestrictionStr = &s
	}

	human := &db.HumanFull{
		ID:                  ctx.UserID,
		Name:                ctx.UserName,
		Type:                userType,
		Enabled:             1,
		Area:                "[]",
		CommunityMembership: communityMembership,
	}
	if areaRestrictionStr != nil {
		human.AreaRestriction.SetValid(*areaRestrictionStr)
	}

	if err := db.CreateHuman(ctx.DB, human); err != nil {
		log.Errorf("poracle: create human %s: %v", ctx.UserID, err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Create default profile
	if err := db.CreateDefaultProfile(ctx.DB, ctx.UserID, ctx.UserName, "[]", 0, 0); err != nil {
		log.Errorf("poracle: create default profile for %s: %v", ctx.UserID, err)
		// Don't fail the registration, the human was created
	}

	ctx.TriggerReload()
	log.Infof("poracle: %s registered as %s", ctx.UserName, userType)

	replies := []bot.Reply{{React: "✅"}}
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
