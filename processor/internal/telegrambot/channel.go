package telegrambot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/db"
)

var (
	telegramNameRe    = regexp.MustCompile(`(?i)^name[:<]?(\S+?)>?$`)
	telegramChannelRe = regexp.MustCompile(`^-?\d{1,20}$`)
)

// handleChannel handles the /channel command in Telegram.
// Telegram groups use telegram:group type; named channels use telegram:channel (admin only).
func (b *Bot) handleChannel(m *tgbotapi.Message, args []string) {
	if m.From == nil {
		return
	}

	userID := formatInt64(m.From.ID)
	chatID := m.Chat.ID
	isAdmin := bot.IsAdmin(b.Cfg, "telegram", userID)

	// Determine community admin status and build community/area restriction.
	var communityList []string
	var areaRestriction []string
	fullAdmin := false

	if b.Cfg.Area.Enabled {
		communityList = community.IsTelegramCommunityAdmin(b.Cfg.Area.Communities, userID)
		if len(communityList) > 0 {
			areaRestriction = community.CalculateLocationRestrictions(b.Cfg.Area.Communities, communityList)
		}
	}

	if isAdmin {
		communityList = []string{}
		areaRestriction = nil
		fullAdmin = true
	}

	if communityList == nil {
		// Not a recognised admin at all.
		return
	}

	// Parse name<value> and channel ID from args.
	var channelName string
	var channelIDStr string
	for _, arg := range args {
		if match := telegramNameRe.FindStringSubmatch(arg); match != nil {
			channelName = match[1]
		}
		if telegramChannelRe.MatchString(arg) && arg != "add" && arg != "remove" {
			channelIDStr = arg
		}
	}

	hasAdd := false
	hasRemove := false
	for _, arg := range args {
		if arg == "add" {
			hasAdd = true
		}
		if arg == "remove" {
			hasRemove = true
		}
	}

	// Determine target.
	var targetID, targetName, targetType string
	isNamedChannel := false

	if channelName != "" && channelIDStr == "" || channelName == "" && channelIDStr != "" {
		msg := tgbotapi.NewMessage(chatID, "To add a channel, provide both a name and a channel id")
		b.api.Send(msg)
		return
	}

	if channelName != "" && channelIDStr != "" {
		if !fullAdmin {
			msg := tgbotapi.NewMessage(chatID, "You are not a full poracle administrator")
			b.api.Send(msg)
			return
		}
		targetID = channelIDStr
		targetName = channelName
		targetType = "telegram:channel"
		isNamedChannel = true
	}

	if !isNamedChannel {
		if m.Chat.Type == "private" {
			msg := tgbotapi.NewMessage(chatID, "To add a group, please send /channel add in the group")
			b.api.Send(msg)
			return
		}
		targetID = formatInt64(m.Chat.ID)
		targetName = m.Chat.Title
		targetType = bot.TypeTelegramGroup
	}

	if hasAdd {
		b.handleChannelAdd(chatID, targetID, targetName, targetType, communityList, areaRestriction)
	} else if hasRemove {
		b.handleChannelRemove(chatID, targetID, targetName, targetType)
	}
}

func (b *Bot) handleChannelAdd(chatID int64, targetID, targetName, targetType string, communityList, areaRestriction []string) {
	// Check if already registered.
	existing, _ := db.SelectOneHumanFull(b.DB, targetID)
	if existing != nil {
		msg := tgbotapi.NewMessage(chatID, "👌")
		b.api.Send(msg)
		return
	}

	communityJSON, _ := json.Marshal(communityList)

	h := &db.HumanFull{
		ID:                  targetID,
		Type:                targetType,
		Name:                targetName,
		Enabled:             1,
		Area:                "[]",
		CommunityMembership: string(communityJSON),
	}
	if areaRestriction != nil {
		areaRestrictionJSON, _ := json.Marshal(areaRestriction)
		h.AreaRestriction.SetValid(string(areaRestrictionJSON))
	}

	if err := db.CreateHuman(b.DB, h); err != nil {
		log.Errorf("telegram bot: create human for %s %s: %v", targetType, targetID, err)
		msg := tgbotapi.NewMessage(chatID, "Failed to register, check logs")
		b.api.Send(msg)
		return
	}

	// Create default profile.
	if err := db.CreateDefaultProfile(b.DB, targetID, targetName, "[]", 0, 0); err != nil {
		log.Warnf("telegram bot: create default profile for %s: %v", targetID, err)
	}

	msg := tgbotapi.NewMessage(chatID, "✅")
	b.api.Send(msg)

	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}
}

func (b *Bot) handleChannelRemove(chatID int64, targetID, targetName, targetType string) {
	existing, _ := db.SelectOneHumanFull(b.DB, targetID)
	if existing == nil {
		msg := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("%s does not seem to be registered. Add it with /channel add", targetID))
		b.api.Send(msg)
		return
	}

	if err := db.DeleteHumanAndTracking(b.DB, targetID); err != nil {
		log.Errorf("telegram bot: delete %s %s: %v", targetType, targetID, err)
		msg := tgbotapi.NewMessage(chatID, "Failed to remove, check logs")
		b.api.Send(msg)
		return
	}

	msg := tgbotapi.NewMessage(chatID, "✅")
	b.api.Send(msg)

	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}
}

// parseTelegramChannelID parses a string that looks like a Telegram channel/group ID.
func parseTelegramChannelID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
