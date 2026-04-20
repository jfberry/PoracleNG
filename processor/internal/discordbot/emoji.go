package discordbot

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

// handleEmoji handles the !poracle-emoji command.
// Downloads emoji images from the configured uicons repository and uploads them to the Discord guild.
// Generates an emoji.json config snippet as output.
func (b *Bot) handleEmoji(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	guildID := m.GuildID
	if guildID == "" {
		// Check for guild<id> override
		if gid := parseGuildArg(args); gid != "" {
			guildID = gid
		}
		if guildID == "" {
			s.ChannelMessageSend(m.ChannelID, "No guild has been set, either execute inside a channel or specify guild<id>")
			return
		}
	}

	// Check if imgUrl is a uicons repository by checking for index.json.
	imgURL := b.Cfg.General.ImgURL
	if imgURL == "" {
		s.ChannelMessageSend(m.ChannelID, "No img_url configured")
		return
	}

	if !isUiconsRepository(imgURL) {
		s.ChannelMessageSend(m.ChannelID, "Currently configured img_url is not a uicons repository")
		return
	}

	// Create a temporary Uicons instance to resolve icon URLs.
	icons := uicons.New(imgURL, "png")

	// Load existing guild emojis.
	existingEmojis, err := s.GuildEmojis(guildID)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to load guild emojis: %v", err))
		return
	}
	emojiMap := make(map[string]*discordgo.Emoji, len(existingEmojis))
	for _, e := range existingEmojis {
		emojiMap[e.Name] = e
	}

	// Parse args.
	upload := false
	overwrite := false
	for _, arg := range args {
		if arg == "upload" {
			upload = true
		}
		if arg == "overwrite" {
			overwrite = true
		}
	}

	if upload {
		s.ChannelMessageSend(m.ChannelID, "Beginning upload of emojis, this may take a little while. Don't run a second time unless you are told it is finished!")
	}

	poracleEmoji := make(map[string]emojiInfo)
	client := &http.Client{Timeout: 15 * time.Second}

	// Helper to upload/register a single emoji.
	setEmoji := func(url, name string) {
		discordName := "poracle_" + strings.ReplaceAll(name, "-", "_")

		if upload && url != "" && !strings.HasSuffix(url, "/0.png") {
			if existing, ok := emojiMap[discordName]; ok && overwrite {
				if err := s.GuildEmojiDelete(guildID, existing.ID); err != nil {
					log.Warnf("discord bot: delete emoji %s: %v", discordName, err)
				}
				delete(emojiMap, discordName)
			}

			if _, exists := emojiMap[discordName]; !exists {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Uploading %s from %s", discordName, url))

				imageData, err := downloadImage(client, url)
				if err != nil {
					log.Warnf("discord bot: download emoji %s from %s: %v", discordName, url, err)
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to download %s", discordName))
					return
				}

				b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData)
				emoji, err := s.GuildEmojiCreate(guildID, &discordgo.EmojiParams{
					Name:  discordName,
					Image: b64,
				})
				if err != nil {
					log.Warnf("discord bot: create emoji %s: %v", discordName, err)
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to upload %s: %v", discordName, err))
					return
				}
				emojiMap[discordName] = emoji

				// Rate limit: Discord has a limit on emoji creation.
				time.Sleep(1 * time.Second)
			}
		} else if url != "" && strings.HasSuffix(url, "/0.png") {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Repository does not have a suitable emoji for %s", discordName))
		}

		if e, ok := emojiMap[discordName]; ok {
			poracleEmoji[name] = emojiInfo{name: discordName, id: e.ID}
		}
	}

	// Types
	if b.GameData != nil && b.GameData.Util != nil {
		for _, typInfo := range b.GameData.Util.Types {
			if typInfo.Emoji != "" {
				url := ""
				if upload {
					url = icons.TypeIcon(typInfo.ID)
				}
				setEmoji(url, typInfo.Emoji)
			}
		}

		// Weather
		for weatherID, weatherInfo := range b.GameData.Util.Weather {
			if weatherInfo.Emoji != "" {
				url := ""
				if upload {
					url = icons.WeatherIcon(weatherID)
				}
				setEmoji(url, weatherInfo.Emoji)
			}
		}

		// Lures
		for lureID, lureInfo := range b.GameData.Util.Lures {
			if lureInfo.Emoji != "" {
				url := ""
				if upload {
					url = icons.RewardItemIcon(lureID, 0)
				}
				setEmoji(url, lureInfo.Emoji)
			}
		}

		// Teams
		for teamID, teamInfo := range b.GameData.Util.Teams {
			if teamInfo.Emoji != "" {
				url := ""
				if upload {
					url = icons.TeamIcon(teamID)
				}
				setEmoji(url, teamInfo.Emoji)
			}
		}
	}

	// Generate emoji.json content.
	var sb strings.Builder
	sb.WriteString("{\n  \"discord\": {")
	first := true
	for poracleName, details := range poracleEmoji {
		if first {
			first = false
		} else {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("\n    \"%s\":\"<:%s:%s>\"", poracleName, details.name, details.id))
	}
	sb.WriteString("\n  }\n}\n")

	// Send the emoji.json as a file attachment.
	emojiJSON := sb.String()
	reader := strings.NewReader(emojiJSON)

	msg := &discordgo.MessageSend{
		Content: "Here's a nice new emoji.json for you!",
		Files: []*discordgo.File{{
			Name:   "emoji.json",
			Reader: reader,
		}},
	}
	if _, err := s.ChannelMessageSendComplex(m.ChannelID, msg); err != nil {
		log.Warnf("discord bot: send emoji.json: %v", err)
	}
}

type emojiInfo struct {
	name string
	id   string
}

// isUiconsRepository checks if the URL hosts a uicons-compatible repository
// by requesting its index.json.
func isUiconsRepository(url string) bool {
	url = strings.TrimRight(url, "/")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url + "/index.json")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// downloadImage downloads an image from a URL and returns the raw bytes.
func downloadImage(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	// Limit read to 256KB (Discord emoji size limit).
	return io.ReadAll(io.LimitReader(resp.Body, 256*1024))
}
