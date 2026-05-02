package telegrambot

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/go-telegram/bot/models"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

var (
	telegramNameRe    = regexp.MustCompile(`(?i)^name[:<]?(\S+?)>?$`)
	telegramChannelRe = regexp.MustCompile(`^-?\d{1,20}$`)
)

// handleChannel handles the /channel command in Telegram.
// Telegram groups use telegram:group type; forum topics use telegram:topic
// with a composite "<chatID>:<threadID>" id; named channels use
// telegram:channel (admin only).
func (b *Bot) handleChannel(m *models.Message, threadID int, args []string) {
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
		_, _ = b.sendTopicMessage(chatID, threadID, "To add a channel, provide both a name and a channel id")
		return
	}

	if channelName != "" && channelIDStr != "" {
		if !fullAdmin {
			_, _ = b.sendTopicMessage(chatID, threadID, "You are not a full poracle administrator")
			return
		}
		targetID = channelIDStr
		targetName = channelName
		targetType = "telegram:channel"
		isNamedChannel = true
	}

	if !isNamedChannel {
		if m.Chat.Type == models.ChatTypePrivate {
			_, _ = b.sendTopicMessage(chatID, 0, "To add a group, please send /channel add in the group")
			return
		}
		if threadID > 0 {
			// Run inside a forum topic — register the topic itself
			// rather than the parent supergroup.
			targetID = composeTopicChannelID(m.Chat.ID, threadID)
			topicName := m.Chat.Title
			if topicName == "" {
				topicName = "topic"
			}
			targetName = fmt.Sprintf("%s [topic %d]", topicName, threadID)
			targetType = bot.TypeTelegramTopic
		} else {
			targetID = formatInt64(m.Chat.ID)
			targetName = m.Chat.Title
			targetType = bot.TypeTelegramGroup
		}
	}

	if hasAdd {
		b.handleChannelAdd(chatID, threadID, targetID, targetName, targetType, communityList, areaRestriction)
	} else if hasRemove {
		b.handleChannelRemove(chatID, threadID, targetID, targetName, targetType)
	}
}

func (b *Bot) handleChannelAdd(chatID int64, threadID int, targetID, targetName, targetType string, communityList, areaRestriction []string) {
	// Check if already registered.
	existing, _ := b.Humans.Get(targetID)
	if existing != nil {
		_, _ = b.sendTopicMessage(chatID, threadID, "👌")
		return
	}

	h := &store.Human{
		ID:                  targetID,
		Type:                targetType,
		Name:                targetName,
		Enabled:             true,
		CommunityMembership: communityList,
		AreaRestriction:     areaRestriction,
	}

	if err := b.Humans.Create(h); err != nil {
		log.Errorf("telegram bot: create human for %s %s: %v", targetType, targetID, err)
		_, _ = b.sendTopicMessage(chatID, threadID, "Failed to register, check logs")
		return
	}

	// Create default profile.
	if err := b.Humans.CreateDefaultProfile(targetID, targetName, nil, 0, 0); err != nil {
		log.Warnf("telegram bot: create default profile for %s: %v", targetID, err)
	}

	_, _ = b.sendTopicMessage(chatID, threadID, "✅")

	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}
}

func (b *Bot) handleChannelRemove(chatID int64, threadID int, targetID, targetName, targetType string) {
	existing, _ := b.Humans.Get(targetID)
	if existing == nil {
		_, _ = b.sendTopicMessage(chatID, threadID, fmt.Sprintf("%s does not seem to be registered. Add it with /channel add", targetID))
		return
	}

	if err := db.DeleteHumanAndTracking(b.DB, targetID); err != nil {
		log.Errorf("telegram bot: delete %s %s: %v", targetType, targetID, err)
		_, _ = b.sendTopicMessage(chatID, threadID, "Failed to remove, check logs")
		return
	}

	_, _ = b.sendTopicMessage(chatID, threadID, "✅")

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
