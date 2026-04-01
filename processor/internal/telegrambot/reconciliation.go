package telegrambot

import (
	"encoding/json"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// TelegramUserInfo holds the display name and channel memberships for a Telegram user.
type TelegramUserInfo struct {
	Name     string
	Channels []string // channel/group IDs where the user is a member
}

// TelegramReconciliation implements Telegram user reconciliation.
// It checks membership in configured Telegram channels/groups via getChatMember API.
type TelegramReconciliation struct {
	api          *tgbotapi.BotAPI
	db           *sqlx.DB
	cfg          *config.Config
	translations *i18n.Bundle
	dtsStore     *dts.TemplateStore
	log          *log.Entry
}

// NewTelegramReconciliation creates a new TelegramReconciliation instance.
func NewTelegramReconciliation(
	api *tgbotapi.BotAPI,
	dbx *sqlx.DB,
	cfg *config.Config,
	translations *i18n.Bundle,
	dtsStore *dts.TemplateStore,
) *TelegramReconciliation {
	return &TelegramReconciliation{
		api:          api,
		db:           dbx,
		cfg:          cfg,
		translations: translations,
		dtsStore:     dtsStore,
		log:          log.WithField("component", "reconciliation-telegram"),
	}
}

// getChannelList returns the list of channel/group IDs to check membership against.
func (r *TelegramReconciliation) getChannelList() []int64 {
	var channelList []int64

	if !r.cfg.Area.Enabled {
		for _, ch := range r.cfg.Telegram.Channels {
			if id, ok := parseTelegramChannelID(ch); ok {
				channelList = append(channelList, id)
			}
		}
	} else {
		// Area security: collect channels from all communities.
		seen := make(map[int64]bool)
		for _, comm := range r.cfg.Area.Communities {
			for _, ch := range comm.Telegram.Channels {
				if id, ok := parseTelegramChannelID(ch); ok && !seen[id] {
					seen[id] = true
					channelList = append(channelList, id)
				}
			}
		}
	}

	return channelList
}

// loadTelegramChannels checks which channels a user belongs to via getChatMember.
func (r *TelegramReconciliation) loadTelegramChannels(userID int64, channelList []int64) *TelegramUserInfo {
	var validChannels []string
	var name string

	for _, groupID := range channelList {
		member, err := r.api.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: groupID,
				UserID: userID,
			},
		})
		if err != nil {
			// 400 typically means user not found in chat — skip silently.
			if strings.Contains(err.Error(), "Bad Request") || strings.Contains(err.Error(), "user not found") {
				continue
			}
			r.log.Warnf("Load telegram channels for user %d failed - chat %d: %v", userID, groupID, err)
			continue
		}

		if member.User != nil && name == "" {
			n := member.User.FirstName
			if member.User.LastName != "" {
				n += " " + member.User.LastName
			}
			if member.User.UserName != "" {
				n += " [" + member.User.UserName + "]"
			}
			name = n
		}

		if member.Status != "left" && member.Status != "kicked" {
			validChannels = append(validChannels, formatInt64(groupID))
		}
	}

	return &TelegramUserInfo{
		Name:     name,
		Channels: validChannels,
	}
}

// SyncTelegramUsers performs a full periodic reconciliation of Telegram users.
func (r *TelegramReconciliation) SyncTelegramUsers(syncNames, removeInvalidUsers bool) {
	r.log.Info("User membership to Poracle users starting...")

	var usersToCheck []db.HumanFull
	if err := r.db.Select(&usersToCheck, `SELECT * FROM humans WHERE type = 'telegram:user'`); err != nil {
		r.log.Errorf("User check failed: load humans: %v", err)
		return
	}

	// Filter out admins.
	var filtered []db.HumanFull
	for _, u := range usersToCheck {
		if !isTelegramAdminID(r.cfg, u.ID) {
			filtered = append(filtered, u)
		}
	}
	usersToCheck = filtered

	channelList := r.getChannelList()

	for i := range usersToCheck {
		user := &usersToCheck[i]
		userID, ok := parseTelegramChannelID(user.ID)
		if !ok {
			continue
		}
		telegramInfo := r.loadTelegramChannels(userID, channelList)
		r.reconcileUser(user.ID, user, telegramInfo, syncNames, removeInvalidUsers)
	}

	r.log.Info("User membership to Poracle users complete...")
}

// reconcileUser is the core reconciliation logic for a single Telegram user.
func (r *TelegramReconciliation) reconcileUser(id string, user *db.HumanFull, telegramUser *TelegramUserInfo, syncNames, removeInvalidUsers bool) {
	defer func() {
		if rv := recover(); rv != nil {
			r.log.Errorf("Synchronisation of Poracle user %s panicked: %v", id, rv)
		}
	}()

	r.log.Debugf("Check user %s", id)

	channels := telegramUser.Channels
	name := telegramUser.Name

	if !r.cfg.Area.Enabled {
		r.reconcileNonAreaSecurity(id, user, name, channels, syncNames, removeInvalidUsers)
	} else {
		r.reconcileAreaSecurity(id, user, name, channels, syncNames, removeInvalidUsers)
	}
}

// reconcileNonAreaSecurity handles reconciliation when area security is disabled.
func (r *TelegramReconciliation) reconcileNonAreaSecurity(id string, user *db.HumanFull, name string, channels []string, syncNames, removeInvalidUsers bool) {
	before := user != nil && user.AdminDisable == 0
	after := hasAnyChannel(channels, r.cfg.Telegram.Channels)

	if !before && after {
		if user == nil {
			r.log.Infof("Create user %s %s", id, name)

			h := &db.HumanFull{
				ID:                  id,
				Type:                "telegram:user",
				Name:                name,
				Enabled:             1,
				Area:                "[]",
				CommunityMembership: "[]",
			}
			if err := db.CreateHuman(r.db, h); err != nil {
				r.log.Errorf("Create user %s: %v", id, err)
				return
			}
			if err := db.CreateDefaultProfile(r.db, id, name, "[]", 0, 0); err != nil {
				r.log.Errorf("Create default profile for %s: %v", id, err)
			}
			r.sendGreetings(id)

		} else if user.AdminDisable == 1 && user.DisabledDate.Valid {
			r.log.Infof("Reactivate user %s %s", id, name)

			if _, err := r.db.Exec(
				`UPDATE humans SET admin_disable = 0, disabled_date = NULL WHERE id = ?`,
				id); err != nil {
				r.log.Errorf("Reactivate user %s: %v", id, err)
				return
			}
			r.sendGreetings(id)
		}
	}

	if before && !after {
		if user != nil && removeInvalidUsers {
			r.disableUser(user)
		}
	}

	if before && after {
		if syncNames && user.Name != name && name != "" {
			bot.UpdateHuman(r.db, id, map[string]interface{}{"name": name})
		}
	}
}

// reconcileAreaSecurity handles reconciliation when area security is enabled.
func (r *TelegramReconciliation) reconcileAreaSecurity(id string, user *db.HumanFull, name string, channels []string, syncNames, removeInvalidUsers bool) {
	// Build community list from channel membership.
	var communityList []string
	for _, comm := range r.cfg.Area.Communities {
		if hasAnyChannel(channels, comm.Telegram.Channels) {
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
			r.log.Infof("Create user %s %s with communities %v", id, name, communityList)

			h := &db.HumanFull{
				ID:                  id,
				Type:                "telegram:user",
				Name:                name,
				Enabled:             1,
				Area:                "[]",
				CommunityMembership: string(communityJSON),
			}
			h.AreaRestriction.SetValid(string(areaRestrictionJSON))

			if err := db.CreateHuman(r.db, h); err != nil {
				r.log.Errorf("Create user %s: %v", id, err)
				return
			}
			if err := db.CreateDefaultProfile(r.db, id, name, "[]", 0, 0); err != nil {
				r.log.Errorf("Create default profile for %s: %v", id, err)
			}
			r.sendGreetings(id)

		} else if user.AdminDisable == 1 && user.DisabledDate.Valid {
			r.log.Infof("Reactivate user %s %s with communities %v", id, name, communityList)

			_, err := r.db.Exec(
				`UPDATE humans SET admin_disable = 0, disabled_date = NULL,
				 area_restriction = ?, community_membership = ?
				 WHERE id = ?`,
				string(areaRestrictionJSON), string(communityJSON), id)
			if err != nil {
				r.log.Errorf("Reactivate user %s: %v", id, err)
				return
			}
			r.sendGreetings(id)
		}
	}

	if before && !after {
		if user != nil && removeInvalidUsers {
			r.disableUser(user)
		}
	}

	if before && after {
		updates := make(map[string]interface{})
		if syncNames && user.Name != name && name != "" {
			updates["name"] = name
		}

		if !user.AreaRestriction.Valid || !bot.HaveSameContents(areaRestriction, bot.ParseJSONStringSlice(user.AreaRestriction.ValueOrZero())) {
			updates["area_restriction"] = string(areaRestrictionJSON)
		}

		if !bot.HaveSameContents(communityList, bot.ParseJSONStringSlice(user.CommunityMembership)) {
			updates["community_membership"] = string(communityJSON)
		}

		if len(updates) > 0 {
			bot.UpdateHuman(r.db, id, updates)
			r.log.Infof("Update user %s %s with communities %v", id, name, communityList)
		}
	}
}

// UpdateTelegramChannels verifies registered channels/groups, updates area restrictions.
func (r *TelegramReconciliation) UpdateTelegramChannels() {
	r.log.Info("Channel membership to Poracle users starting...")

	var channelsToCheck []db.HumanFull
	if err := r.db.Select(&channelsToCheck,
		`SELECT * FROM humans WHERE (type = 'telegram:channel' OR type = 'telegram:group') AND admin_disable = 0`); err != nil {
		r.log.Errorf("Verification of Poracle channels failed: %v", err)
		return
	}

	for _, user := range channelsToCheck {
		r.log.Debugf("Check channel %s %s", user.ID, user.Name)

		if user.AreaRestriction.Valid && user.CommunityMembership != "" {
			membership := bot.ParseJSONStringSlice(user.CommunityMembership)
			if len(membership) > 0 {
				areaRestriction := community.CalculateLocationRestrictions(r.cfg.Area.Communities, membership)
				existing := bot.ParseJSONStringSlice(user.AreaRestriction.ValueOrZero())
				if !bot.HaveSameContents(areaRestriction, existing) {
					areaRestrictionJSON, _ := json.Marshal(areaRestriction)
					bot.UpdateHuman(r.db, user.ID, map[string]interface{}{
						"area_restriction": string(areaRestrictionJSON),
					})
					r.log.Infof("Update channel %s %s", user.ID, user.Name)
				}
			}
		}
	}

	r.log.Debug("Channel membership to Poracle users complete...")
}

// disableUser disables or deletes a user based on roleCheckMode.
func (r *TelegramReconciliation) disableUser(user *db.HumanFull) {
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
			r.sendGoodbye(user.ID)
		}

	case "delete":
		if err := db.DeleteHumanAndTracking(r.db, user.ID); err != nil {
			r.log.Errorf("Delete user %s: %v", user.ID, err)
			return
		}
		r.log.Infof("Delete user %s %s", user.ID, user.Name)
		r.sendGoodbye(user.ID)

	default:
		r.log.Infof("Not removing invalid user %s [roleCheckMode is ignored]", user.ID)
	}
}

// sendGreetings sends a DTS greeting message via Telegram.
func (r *TelegramReconciliation) sendGreetings(id string) {
	tmpl := r.dtsStore.Get("greeting", "telegram", "", "")
	if tmpl == nil {
		tmpl = r.dtsStore.Get("greeting", "telegram", "1", "")
	}
	if tmpl == nil {
		tmpl = r.dtsStore.Get("greeting", "telegram", "default", "")
	}
	if tmpl == nil {
		r.log.Debug("No greeting template found")
		return
	}

	view := map[string]any{
		"prefix": "/",
	}

	result, err := tmpl.Exec(view)
	if err != nil {
		r.log.Errorf("Cannot render greeting for %s: %v", id, err)
		return
	}

	chatID, ok := parseTelegramChannelID(id)
	if !ok {
		return
	}

	// Try to parse as JSON (DTS template output). Extract text from embed fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &raw); err == nil {
		text := extractTelegramText(raw)
		if text != "" {
			msg := tgbotapi.NewMessage(chatID, text)
			r.api.Send(msg)
			return
		}
	}

	// Fallback: send as plain text.
	msg := tgbotapi.NewMessage(chatID, result)
	r.api.Send(msg)
}

// sendGoodbye sends the configured goodbye message via Telegram.
func (r *TelegramReconciliation) sendGoodbye(id string) {
	goodbyeMsg := r.cfg.Telegram.BotGoodbyeMessage
	if goodbyeMsg == "" {
		return
	}

	chatID, ok := parseTelegramChannelID(id)
	if !ok {
		return
	}

	msg := tgbotapi.NewMessage(chatID, goodbyeMsg)
	if _, err := r.api.Send(msg); err != nil {
		r.log.Warnf("Could not send goodbye to %s: %v", id, err)
	}
}

// extractTelegramText converts a DTS JSON template output to plain text for Telegram.
// Extracts content and embed fields, similar to the alerter's sendGreetings.
func extractTelegramText(raw map[string]json.RawMessage) string {
	var text string

	if c, ok := raw["content"]; ok {
		var content string
		json.Unmarshal(c, &content)
		text = content
	}

	if e, ok := raw["embed"]; ok {
		var embed struct {
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		}
		if json.Unmarshal(e, &embed) == nil {
			for _, field := range embed.Fields {
				text += "\n\n" + field.Name + "\n\n" + field.Value
			}
		}
	}

	return strings.TrimSpace(text)
}

// --- Helper functions ---

// isTelegramAdminID checks if a user ID is in the Telegram admin list.
func isTelegramAdminID(cfg *config.Config, id string) bool {
	for _, admin := range cfg.Telegram.Admins {
		if admin == id {
			return true
		}
	}
	return false
}

// hasAnyChannel checks if any channel in the user's list matches a configured channel.
func hasAnyChannel(userChannels, configuredChannels []string) bool {
	for _, uc := range userChannels {
		for _, cc := range configuredChannels {
			if uc == cc {
				return true
			}
		}
	}
	return false
}

