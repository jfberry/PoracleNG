package discordbot

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// DiscordUserInfo holds the display name and roles for a Discord user across all guilds.
type DiscordUserInfo struct {
	Name  string
	Roles []string
}

// Reconciliation implements Discord user/channel reconciliation.
// It syncs Discord role membership with Poracle user registration.
type Reconciliation struct {
	session      *discordgo.Session
	db           *sqlx.DB
	cfg          *config.Config
	translations *i18n.Bundle
	dtsStore     *dts.TemplateStore
	log          *log.Entry

	// Greeting rate limiting: max 10 per minute to avoid Discord ban.
	greetingCount      int
	lastGreetingMinute int64
	mu                 sync.Mutex
}

// NewReconciliation creates a new Reconciliation instance.
func NewReconciliation(
	session *discordgo.Session,
	dbx *sqlx.DB,
	cfg *config.Config,
	translations *i18n.Bundle,
	dtsStore *dts.TemplateStore,
) *Reconciliation {
	return &Reconciliation{
		session:      session,
		db:           dbx,
		cfg:          cfg,
		translations: translations,
		dtsStore:     dtsStore,
		log:          log.WithField("component", "reconciliation-discord"),
	}
}

// LoadAllGuildUsers fetches all non-bot members across all configured guilds.
// Returns a map of userID -> DiscordUserInfo with merged roles from all guilds.
// Implements a 15-second stagger between guild fetches to avoid gateway rate limits.
func (r *Reconciliation) LoadAllGuildUsers() (map[string]*DiscordUserInfo, error) {
	userList := make(map[string]*DiscordUserInfo)

	r.log.Info("Loading all guild users...")

	for gi, guildID := range r.cfg.Discord.Guilds {
		if guildID == "" {
			continue
		}
		r.log.Infof("Loading guild id %s ...", guildID)

		// Stagger guild member fetches to avoid gateway opcode 8 rate limits.
		if gi > 0 {
			time.Sleep(15 * time.Second)
		}

		// Paginated member fetch.
		var allMembers []*discordgo.Member
		after := ""
		for {
			members, err := r.session.GuildMembers(guildID, after, 1000)
			if err != nil {
				r.log.Errorf("Problem accessing guild %s: %v", guildID, err)
				return nil, fmt.Errorf("fetch guild %s members: %w", guildID, err)
			}
			allMembers = append(allMembers, members...)
			if len(members) < 1000 {
				break
			}
			after = members[len(members)-1].User.ID
		}

		memberCount := 0
		for _, member := range allMembers {
			if member.User == nil || member.User.Bot {
				continue
			}
			memberCount++

			roleList := make([]string, len(member.Roles))
			copy(roleList, member.Roles)

			existing, ok := userList[member.User.ID]
			if !ok {
				name := member.Nick
				if name == "" {
					name = member.User.GlobalName
				}
				if name == "" {
					name = member.User.Username
				}
				userList[member.User.ID] = &DiscordUserInfo{
					Name:  stripEmojis(name),
					Roles: roleList,
				}
			} else {
				existing.Roles = append(existing.Roles, roleList...)
			}
		}
		r.log.Infof("Loading guild id %s complete with %d members...", guildID, memberCount)
	}

	r.log.Info("Loading all guild users complete...")
	return userList, nil
}

// SyncDiscordRole performs a full periodic reconciliation of Discord users.
// It loads all humans, loads all guild members, and reconciles each.
func (r *Reconciliation) SyncDiscordRole(registerNewUsers, syncNames, removeInvalidUsers bool) {
	r.log.Info("User role membership to Poracle users starting...")

	var usersToCheck []db.HumanFull
	err := r.db.Select(&usersToCheck, `SELECT * FROM humans WHERE type = 'discord:user'`)
	if err != nil {
		r.log.Errorf("User role check failed: load humans: %v", err)
		return
	}

	// Filter out admins.
	var filtered []db.HumanFull
	for _, u := range usersToCheck {
		if !isAdminID(r.cfg, u.ID) {
			filtered = append(filtered, u)
		}
	}
	usersToCheck = filtered

	discordUserList, err := r.LoadAllGuildUsers()
	if err != nil {
		r.log.Errorf("User role check failed: load guild users: %v", err)
		return
	}

	checked := make(map[string]bool, len(usersToCheck))

	for i := range usersToCheck {
		user := &usersToCheck[i]
		checked[user.ID] = true
		r.ReconcileUser(user.ID, user, discordUserList[user.ID], syncNames, removeInvalidUsers)
	}

	if registerNewUsers {
		r.log.Info("Find qualified users missing from Poracle users starting...")
		for id, discordUser := range discordUserList {
			if !isAdminID(r.cfg, id) && !checked[id] {
				r.ReconcileUser(id, nil, discordUser, syncNames, removeInvalidUsers)
			}
		}
		r.log.Info("Find qualified users missing from Poracle users complete...")
	}

	r.log.Info("User role membership to Poracle users complete...")
}

// ReconcileSingleUser fetches a single user's roles from all guilds and reconciles.
// Called from event handlers (guildMemberUpdate, guildMemberRemove).
func (r *Reconciliation) ReconcileSingleUser(id string, removeInvalidUsers bool) {
	// Never reconcile admins — they should not be disabled by role changes
	if isAdminID(r.cfg, id) {
		return
	}

	r.log.Debugf("Check (single) user %s", id)

	roleList := make([]string, 0)
	name := ""

	for _, guildID := range r.cfg.Discord.Guilds {
		if guildID == "" {
			continue
		}

		member, err := r.session.GuildMember(guildID, id)
		if err != nil {
			// Check if it's a 404 (user not in guild) — skip.
			if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Response != nil && restErr.Response.StatusCode == 404 {
				continue
			}
			r.log.Warnf("Cannot load guild %s member %s: %v", guildID, id, err)
			continue
		}

		if member != nil {
			if name == "" {
				n := member.Nick
				if n == "" && member.User != nil {
					n = member.User.GlobalName
				}
				if n == "" && member.User != nil {
					n = member.User.Username
				}
				name = stripEmojis(n)
			}
			roleList = append(roleList, member.Roles...)
		}
	}

	user, err := db.SelectOneHumanFull(r.db, id)
	if err != nil {
		r.log.Errorf("Check single user %s: select human: %v", id, err)
		return
	}

	r.ReconcileUser(id, user, &DiscordUserInfo{
		Name:  name,
		Roles: roleList,
	}, false, removeInvalidUsers)
}

// ReconcileUser is the core reconciliation logic for a single user.
// It compares the user's current state in the DB with their Discord roles
// and creates, reactivates, disables, or updates the user as needed.
func (r *Reconciliation) ReconcileUser(id string, user *db.HumanFull, discordUser *DiscordUserInfo, syncNames, removeInvalidUsers bool) {
	defer func() {
		if rv := recover(); rv != nil {
			r.log.Errorf("Synchronisation of Poracle user %s panicked: %v", id, rv)
		}
	}()

	r.log.Debugf("Check user %s", id)

	var roleList []string
	name := ""
	if discordUser != nil {
		roleList = discordUser.Roles
		name = discordUser.Name
	}

	// Compute blocked_alerts from command_security.
	blocked := bot.BlockedAlerts(r.cfg, id, roleList)

	if !r.cfg.Area.Enabled {
		r.reconcileNonAreaSecurity(id, user, name, roleList, blocked, syncNames, removeInvalidUsers)
	} else {
		r.reconcileAreaSecurity(id, user, name, roleList, blocked, syncNames, removeInvalidUsers)
	}
}

// reconcileNonAreaSecurity handles reconciliation when area security is disabled.
// Simple user_role membership check.
func (r *Reconciliation) reconcileNonAreaSecurity(id string, user *db.HumanFull, name string, roleList []string, blocked *string, syncNames, removeInvalidUsers bool) {
	if len(r.cfg.Discord.UserRole) == 0 {
		return
	}

	before := user != nil && user.AdminDisable == 0
	after := hasAnyRole(roleList, r.cfg.Discord.UserRole)

	if !before && after {
		if user == nil {
			// Create new user.
			r.log.Infof("Create user %s %s", id, name)

			h := &db.HumanFull{
				ID:                  id,
				Type:                bot.TypeDiscordUser,
				Name:                name,
				Enabled:             1,
				Area:                "[]",
				CommunityMembership: "[]",
			}
			if blocked != nil {
				h.BlockedAlerts.SetValid(*blocked)
			}

			if err := db.CreateHuman(r.db, h); err != nil {
				r.log.Errorf("Create user %s: %v", id, err)
				return
			}
			if err := db.CreateDefaultProfile(r.db, id, name, "[]", 0, 0); err != nil {
				r.log.Errorf("Create default profile for %s: %v", id, err)
			}
			r.SendGreetings(id)

		} else if user.AdminDisable == 1 && user.DisabledDate.Valid {
			// Reactivate user.
			r.log.Infof("Reactivate user %s %s", id, name)

			args := []interface{}{0, nil}
			setClauses := "admin_disable = ?, disabled_date = ?"
			if blocked != nil {
				setClauses += ", blocked_alerts = ?"
				args = append(args, *blocked)
			} else {
				setClauses += ", blocked_alerts = ?"
				args = append(args, nil)
			}
			args = append(args, id)
			if _, err := r.db.Exec("UPDATE humans SET "+setClauses+" WHERE id = ?", args...); err != nil {
				r.log.Errorf("Reactivate user %s: %v", id, err)
				return
			}
			r.SendGreetings(id)
		}
	}

	if before && !after {
		if user != nil && removeInvalidUsers {
			r.DisableUser(user)
		}
	}

	if before && after {
		updates := make(map[string]interface{})
		if syncNames && user.Name != name && name != "" {
			updates["name"] = name
		}

		blockedStr := nullableStr(blocked)
		userBlockedStr := user.BlockedAlerts.ValueOrZero()
		if blockedStr != userBlockedStr {
			if blocked != nil {
				updates["blocked_alerts"] = *blocked
			} else {
				updates["blocked_alerts"] = nil
			}
		}

		if len(updates) > 0 {
			bot.UpdateHuman(r.db, id, updates)
			r.log.Infof("Update user %s %s", id, name)
		}
	}
}

// reconcileAreaSecurity handles reconciliation when area security is enabled.
// Builds community membership from roles matching community discord.user_role.
func (r *Reconciliation) reconcileAreaSecurity(id string, user *db.HumanFull, name string, roleList []string, blocked *string, syncNames, removeInvalidUsers bool) {
	// Build community list from roles.
	var communityList []string
	for _, comm := range r.cfg.Area.Communities {
		if hasAnyRole(roleList, comm.Discord.UserRole) {
			communityList = append(communityList, strings.ToLower(comm.Name))
		}
	}

	before := user != nil && user.AdminDisable == 0
	after := len(communityList) > 0
	areaRestriction := community.CalculateLocationRestrictions(r.cfg.Area.Communities, communityList)

	areaRestrictionJSON, _ := json.Marshal(areaRestriction)
	communityJSON, _ := json.Marshal(communityList)

	if !before && after {
		if user == nil {
			// Create new user with community membership.
			r.log.Infof("Create user %s %s with communities %v", id, name, communityList)

			h := &db.HumanFull{
				ID:                  id,
				Type:                bot.TypeDiscordUser,
				Name:                name,
				Enabled:             1,
				Area:                "[]",
				CommunityMembership: string(communityJSON),
			}
			h.AreaRestriction.SetValid(string(areaRestrictionJSON))
			if blocked != nil {
				h.BlockedAlerts.SetValid(*blocked)
			}

			if err := db.CreateHuman(r.db, h); err != nil {
				r.log.Errorf("Create user %s: %v", id, err)
				return
			}
			if err := db.CreateDefaultProfile(r.db, id, name, "[]", 0, 0); err != nil {
				r.log.Errorf("Create default profile for %s: %v", id, err)
			}
			r.SendGreetings(id)

		} else if user.AdminDisable == 1 && user.DisabledDate.Valid {
			// Reactivate user with community membership.
			r.log.Infof("Reactivate user %s %s with communities %v", id, name, communityList)

			_, err := r.db.Exec(
				`UPDATE humans SET admin_disable = 0, disabled_date = NULL,
				 area_restriction = ?, community_membership = ?, blocked_alerts = ?
				 WHERE id = ?`,
				string(areaRestrictionJSON), string(communityJSON),
				nullableSqlArg(blocked), id)
			if err != nil {
				r.log.Errorf("Reactivate user %s: %v", id, err)
				return
			}
			r.SendGreetings(id)
		}
	}

	if before && !after {
		if user != nil && removeInvalidUsers {
			r.DisableUser(user)
		}
	}

	if before && after {
		updates := make(map[string]interface{})
		if syncNames && user.Name != name && name != "" {
			updates["name"] = name
		}

		blockedStr := nullableStr(blocked)
		userBlockedStr := user.BlockedAlerts.ValueOrZero()
		if blockedStr != userBlockedStr {
			updates["blocked_alerts"] = nullableSqlArg(blocked)
		}

		// Check area_restriction changes.
		if !user.AreaRestriction.Valid || !bot.HaveSameContents(areaRestriction, bot.ParseJSONStringSlice(user.AreaRestriction.ValueOrZero())) {
			updates["area_restriction"] = string(areaRestrictionJSON)
		}

		// Check community_membership changes.
		if !bot.HaveSameContents(communityList, bot.ParseJSONStringSlice(user.CommunityMembership)) {
			updates["community_membership"] = string(communityJSON)
		}

		if len(updates) > 0 {
			bot.UpdateHuman(r.db, id, updates)
			r.log.Infof("Update user %s %s with communities %v", id, name, communityList)
		}
	}
}

// DisableUser disables or deletes a user based on roleCheckMode.
func (r *Reconciliation) DisableUser(user *db.HumanFull) {
	switch r.cfg.General.RoleCheckMode {
	case "disable-user":
		if user.AdminDisable == 0 {
			_, err := r.db.Exec(
				`UPDATE humans SET admin_disable = 1, disabled_date = NOW() WHERE id = ?`,
				user.ID)
			if err != nil {
				r.log.Errorf("Disable user %s: %v", user.ID, err)
				return
			}
			r.log.Infof("Disable user %s %s", user.ID, user.Name)
			r.RemoveRoles(user)
			r.SendGoodbye(user.ID)
		}

	case "delete":
		if err := db.DeleteHumanAndTracking(r.db, user.ID); err != nil {
			r.log.Errorf("Delete user %s: %v", user.ID, err)
			return
		}
		r.log.Infof("Delete user %s %s", user.ID, user.Name)
		r.RemoveRoles(user)
		r.SendGoodbye(user.ID)

	default:
		r.log.Infof("Not removing invalid user %s [roleCheckMode is ignored]", user.ID)
	}
}

// SendGreetings sends a DTS greeting message via DM.
// Rate limited to max 10 per minute to avoid Discord ban.
func (r *Reconciliation) SendGreetings(id string) {
	if r.cfg.Discord.DisableAutoGreetings {
		return
	}

	r.mu.Lock()
	currentMinute := time.Now().Unix() / 60
	if r.lastGreetingMinute == currentMinute {
		r.greetingCount++
		if r.greetingCount > 10 {
			r.mu.Unlock()
			r.log.Warnf("Did not send greeting to %s - attempting to avoid ban (%d messages in this minute)", id, r.greetingCount)
			return
		}
	} else {
		r.greetingCount = 0
		r.lastGreetingMinute = currentMinute
	}
	r.mu.Unlock()

	// Render greeting template.
	platform := "discord"
	prefix := r.cfg.Discord.Prefix
	if prefix == "" {
		prefix = "!"
	}

	view := map[string]any{
		"prefix": prefix,
	}

	tmpl := r.dtsStore.Get("greeting", platform, "", "")
	if tmpl == nil {
		tmpl = r.dtsStore.Get("greeting", platform, "1", "")
	}
	if tmpl == nil {
		tmpl = r.dtsStore.Get("greeting", platform, "default", "")
	}
	if tmpl == nil {
		r.log.Debug("No greeting template found")
		return
	}

	result, err := tmpl.Exec(view)
	if err != nil {
		r.log.Errorf("Cannot render greeting for %s: %v", id, err)
		return
	}

	// Create DM channel and send.
	ch, err := r.session.UserChannelCreate(id)
	if err != nil {
		r.log.Errorf("Cannot create DM channel for %s: %v", id, err)
		return
	}

	// Parse the rendered JSON into a Discord message.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &raw); err != nil {
		// Not JSON, send as plain text.
		r.session.ChannelMessageSend(ch.ID, result)
		return
	}

	msg := &discordgo.MessageSend{}

	if c, ok := raw["content"]; ok {
		var content string
		json.Unmarshal(c, &content)
		msg.Content = content
	}

	// embed → embeds normalization.
	if e, ok := raw["embed"]; ok {
		var embed discordgo.MessageEmbed
		if json.Unmarshal(e, &embed) == nil {
			msg.Embeds = []*discordgo.MessageEmbed{&embed}
		}
	}
	if e, ok := raw["embeds"]; ok {
		var embeds []*discordgo.MessageEmbed
		if json.Unmarshal(e, &embeds) == nil {
			msg.Embeds = embeds
		}
	}

	if msg.Content != "" || len(msg.Embeds) > 0 {
		if _, err := r.session.ChannelMessageSendComplex(ch.ID, msg); err != nil {
			r.log.Errorf("Cannot send welcome message to %s: %v", id, err)
		}
	}
}

// SendGoodbye sends the configured lost role message via DM.
func (r *Reconciliation) SendGoodbye(id string) {
	if r.cfg.Discord.LostRoleMessage == "" {
		return
	}

	ch, err := r.session.UserChannelCreate(id)
	if err != nil {
		r.log.Warnf("Could not send goodbye to %s: create DM: %v", id, err)
		return
	}

	if _, err := r.session.ChannelMessageSend(ch.ID, r.cfg.Discord.LostRoleMessage); err != nil {
		r.log.Warnf("Could not send goodbye to %s: %v", id, err)
	}
}

// RemoveRoles removes all subscription roles and exclusive roles from a user
// across all guilds configured in role_subscriptions.
func (r *Reconciliation) RemoveRoles(user *db.HumanFull) {
	subscriptions := r.cfg.Discord.RoleSubscriptionMap()
	if len(subscriptions) == 0 {
		return
	}

	for guildID, guildDetails := range subscriptions {
		member, err := r.session.GuildMember(guildID, user.ID)
		if err != nil {
			// 404 = user not in guild, skip silently.
			if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Response != nil && restErr.Response.StatusCode == 404 {
				continue
			}
			r.log.Warnf("RemoveRoles: fetch member %s from guild %s: %v", user.ID, guildID, err)
			continue
		}

		memberRoleSet := make(map[string]bool, len(member.Roles))
		for _, roleID := range member.Roles {
			memberRoleSet[roleID] = true
		}

		// Remove subscription roles.
		for roleName, roleID := range guildDetails.Roles {
			if memberRoleSet[roleID] {
				r.log.Infof("Disable user %s removed role %s", user.ID, roleName)
				if err := r.session.GuildMemberRoleRemove(guildID, user.ID, roleID); err != nil {
					r.log.Warnf("RemoveRoles: remove role %s from %s: %v", roleID, user.ID, err)
				}
			}
		}

		// Remove exclusive roles.
		for _, exclusiveGroup := range guildDetails.ExclusiveRoles {
			for roleName, roleID := range exclusiveGroup {
				if memberRoleSet[roleID] {
					r.log.Infof("Disable user %s removed role %s", user.ID, roleName)
					if err := r.session.GuildMemberRoleRemove(guildID, user.ID, roleID); err != nil {
						r.log.Warnf("RemoveRoles: remove exclusive role %s from %s: %v", roleID, user.ID, err)
					}
				}
			}
		}
	}
}

// SyncDiscordChannels verifies registered channels still exist, updates names/notes.
func (r *Reconciliation) SyncDiscordChannels(syncNames, syncNotes, removeInvalidChannels bool) {
	r.log.Info("Channel membership to Poracle users starting...")

	var channels []db.HumanFull
	err := r.db.Select(&channels, `SELECT * FROM humans WHERE type = 'discord:channel' AND admin_disable = 0`)
	if err != nil {
		r.log.Errorf("Verification of Poracle channels failed: %v", err)
		return
	}

	for _, user := range channels {
		r.log.Debugf("Check channel %s %s", user.ID, user.Name)

		channel, err := r.session.Channel(user.ID)
		if err != nil {
			// Check for 10003 (Unknown Channel).
			if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Response != nil {
				if restErr.Response.StatusCode == 404 {
					if removeInvalidChannels {
						r.log.Infof("Disable channel %s %s", user.ID, user.Name)
						r.db.Exec(
							`UPDATE humans SET admin_disable = 1, disabled_date = NOW() WHERE id = ?`,
							user.ID)
					}
					continue
				}
				r.log.Infof("Problem accessing channel %s %s: %v", user.ID, user.Name, err)
				continue
			}
			r.log.Errorf("Verification of Poracle channels failed: channel %s: %v", user.ID, err)
			continue
		}

		name := channel.Name

		// Build notes describing the channel's location in the guild hierarchy.
		// Channels: "GuildName / CategoryName"
		// Threads:  "GuildName / CategoryName / ChannelName"
		isThread := channel.Type == discordgo.ChannelTypeGuildPublicThread ||
			channel.Type == discordgo.ChannelTypeGuildPrivateThread ||
			channel.Type == discordgo.ChannelTypeGuildNewsThread

		notes := ""
		if channel.GuildID != "" {
			guild, err := r.session.Guild(channel.GuildID)
			if err == nil && guild != nil {
				notes = guild.Name
			}
			if channel.ParentID != "" {
				parent, err := r.session.Channel(channel.ParentID)
				if err == nil && parent != nil {
					if isThread {
						// For threads, ParentID is the channel. Get the category from the channel's ParentID.
						if parent.ParentID != "" {
							category, err := r.session.Channel(parent.ParentID)
							if err == nil && category != nil {
								notes += " / " + category.Name
							}
						}
						notes += " / " + parent.Name
					} else {
						// For channels, ParentID is the category.
						notes += " / " + parent.Name
					}
				}
			}
		}

		updates := make(map[string]interface{})
		if syncNames && user.Name != name && name != "" {
			updates["name"] = name
		}
		if syncNotes && user.Notes != notes && notes != "" {
			updates["notes"] = notes
		}

		// If there is currently an area restriction for a channel, ensure the location restrictions are correct.
		if user.AreaRestriction.Valid && user.CommunityMembership != "" {
			membership := bot.ParseJSONStringSlice(user.CommunityMembership)
			if len(membership) > 0 {
				areaRestriction := community.CalculateLocationRestrictions(r.cfg.Area.Communities, membership)
				existing := bot.ParseJSONStringSlice(user.AreaRestriction.ValueOrZero())
				if !bot.HaveSameContents(areaRestriction, existing) {
					areaRestrictionJSON, _ := json.Marshal(areaRestriction)
					updates["area_restriction"] = string(areaRestrictionJSON)
				}
			}
		}

		if len(updates) > 0 {
			bot.UpdateHuman(r.db, user.ID, updates)
			r.log.Infof("Update channel %s %s", user.ID, name)
		}
	}

	r.log.Debug("Channel membership to Poracle users complete...")
}

// --- Helper functions ---

// isAdminID checks if a user ID is in the admin list.
func isAdminID(cfg *config.Config, id string) bool {
	for _, admin := range cfg.Discord.Admins {
		if admin == id {
			return true
		}
	}
	return false
}

// hasAnyRole checks if any role in the roleList matches any role in the required list.
func hasAnyRole(roleList, required []string) bool {
	for _, role := range roleList {
		for _, req := range required {
			if role == req {
				return true
			}
		}
	}
	return false
}

// nullableStr returns the dereferenced string, or "" if nil.
func nullableStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// nullableSqlArg returns the string value or nil for SQL NULL.
func nullableSqlArg(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// stripEmojis removes common emoji characters from a display name.
// Simplified version — strips Unicode emoji ranges.
func stripEmojis(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Skip common emoji ranges.
		if r >= 0x1F600 && r <= 0x1F64F { // Emoticons
			continue
		}
		if r >= 0x1F300 && r <= 0x1F5FF { // Misc Symbols
			continue
		}
		if r >= 0x1F680 && r <= 0x1F6FF { // Transport
			continue
		}
		if r >= 0x1F900 && r <= 0x1F9FF { // Supplemental Symbols
			continue
		}
		if r >= 0x2600 && r <= 0x26FF { // Misc Symbols
			continue
		}
		if r >= 0x2700 && r <= 0x27BF { // Dingbats
			continue
		}
		if r >= 0xFE00 && r <= 0xFE0F { // Variation Selectors
			continue
		}
		if r == 0x200D { // Zero Width Joiner
			continue
		}
		b.WriteRune(r)
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		return s // fallback to original if stripping removed everything
	}
	return result
}
