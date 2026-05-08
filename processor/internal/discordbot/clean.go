package discordbot

import (
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// bulkDeleteAgeLimit is Discord's 14-day cutoff for bulk-delete.
// Messages older than this must be deleted per-ID. We use 13 days as a
// safety margin against clock skew.
const bulkDeleteAgeLimit = 13 * 24 * time.Hour

// handleClean implements !poracle-clean. Removes the bot's own messages
// from the current channel, up to 100 most recent.
//
// Permissions:
//   - Admins: anywhere (guild channel or DM)
//   - Non-admins: DM only (so a regular user can tidy their own DM
//     history with the bot, but can't unleash cleanup on a guild
//     channel — even one they have access to)
//
// Cleanup strategy:
//   - Recent (≤13d) bot messages → one ChannelMessagesBulkDelete call
//     (~17× fewer API calls than per-message at 100 messages). Bulk
//     requires Manage Messages permission, which doesn't exist in
//     DMs — DM cleanup falls through to per-message.
//   - Older (>13d) bot messages → per-message ChannelMessageDelete.
//   - Single message can use either path; bulk with one ID works.
func (b *Bot) handleClean(s *discordgo.Session, m *discordgo.MessageCreate) {
	isAdmin := bot.IsAdmin(b.Cfg, "discord", m.Author.ID)
	isDM := m.GuildID == ""
	if !isAdmin && !isDM {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	userLang, _, _, _, _ := bot.LookupUserStateFromStore(b.Humans, m.Author.ID, b.Cfg.General.Locale)
	tr := b.Translations.For(userLang)

	messages, err := s.ChannelMessages(m.ChannelID, 100, "", "", "")
	if err != nil {
		log.Warnf("discord bot: fetch messages for clean in %s: %v", m.ChannelID, err)
		s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.failed"))
		return
	}

	// Partition the bot's own messages into bulk-eligible (recent) and
	// per-message-only (old). Discord snowflake IDs encode timestamps,
	// so msg.Timestamp is reliable without an extra fetch.
	cutoff := time.Now().Add(-bulkDeleteAgeLimit)
	var recent, old []string
	for _, msg := range messages {
		if msg.Author == nil || msg.Author.ID != s.State.User.ID {
			continue
		}
		if msg.Timestamp.After(cutoff) {
			recent = append(recent, msg.ID)
		} else {
			old = append(old, msg.ID)
		}
	}

	if len(recent)+len(old) == 0 {
		// Nothing to do — react and skip the start/finish noise.
		s.MessageReactionAdd(m.ChannelID, m.ID, "👌")
		return
	}

	startMsg, _ := s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.start"))

	// Bulk delete the recent batch. Bulk endpoint requires ≥2 IDs; for
	// exactly one, fall through to per-message. Bulk also fails in DMs
	// (no Manage Messages permission concept) — on error, push the IDs
	// into the per-message bucket and continue.
	if len(recent) >= 2 {
		if err := s.ChannelMessagesBulkDelete(m.ChannelID, recent); err != nil {
			log.Debugf("discord bot: bulk delete in %s failed (%v) — falling back to per-message", m.ChannelID, err)
			old = append(old, recent...)
		}
	} else {
		// 0 or 1 recent — handle via per-message path with the old set.
		old = append(old, recent...)
	}

	for _, id := range old {
		if err := s.ChannelMessageDelete(m.ChannelID, id); err != nil {
			log.Debugf("discord bot: delete message %s: %v", id, err)
		}
	}

	// Delete the start message we sent above (recent — bulk-eligible
	// but it's a single message so per-message is fine).
	if startMsg != nil {
		s.ChannelMessageDelete(m.ChannelID, startMsg.ID)
	}

	finishMsg, _ := s.ChannelMessageSend(m.ChannelID, tr.T("msg.poracle_clean.finished"))
	if finishMsg != nil {
		go func() {
			time.Sleep(15 * time.Second)
			s.ChannelMessageDelete(m.ChannelID, finishMsg.ID)
		}()
	}
}
