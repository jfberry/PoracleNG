package discordbot

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// userRe matches user<id>, user:id, or userid (case-insensitive).
var userRe = regexp.MustCompile(`(?i)^user[:<]?(\S+?)>?$`)

func (b *Bot) handleRole(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	isDM := m.GuildID == ""
	isAdmin := bot.IsAdmin(b.Cfg, "discord", m.Author.ID)

	if !isAdmin && !isDM {
		s.ChannelMessageSend(m.ChannelID,
			b.Translations.For(b.Cfg.General.Locale).T("cmd.dm_only"))
		return
	}

	if len(b.Cfg.Discord.RoleSubscriptions) == 0 {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	// Determine target user ID
	targetID := m.Author.ID
	if isAdmin {
		for _, arg := range args {
			if match := userRe.FindStringSubmatch(arg); match != nil {
				targetID = match[1]
			}
		}
	}

	// Check user is registered and not admin-disabled
	var adminDisable int
	err := b.DB.Get(&adminDisable, `SELECT admin_disable FROM humans WHERE id = ? LIMIT 1`, targetID)
	if err != nil || adminDisable != 0 {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	// Get user language
	userLang, _, _, _, _ := bot.LookupUserStateFromStore(b.Humans, targetID, b.Cfg.General.Locale)
	tr := b.Translations.For(userLang)

	// Filter out user override args for subcommand detection
	var filteredArgs []string
	for _, arg := range args {
		if userRe.MatchString(arg) {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}

	subcommand := ""
	if len(filteredArgs) > 0 {
		subcommand = filteredArgs[0]
	}

	roleSubMap := b.Cfg.Discord.RoleSubscriptionMap()

	switch subcommand {
	case "", "list":
		b.handleRoleList(s, m, targetID, roleSubMap, tr, false)
	case "membership":
		b.handleRoleList(s, m, targetID, roleSubMap, tr, true)
	case "add":
		b.handleRoleToggle(s, m, targetID, filteredArgs[1:], roleSubMap, tr, true)
	case "remove":
		b.handleRoleToggle(s, m, targetID, filteredArgs[1:], roleSubMap, tr, false)
	default:
		prefix := b.Cfg.Discord.Prefix
		if prefix == "" {
			prefix = "!"
		}
		s.ChannelMessageSend(m.ChannelID,
			tr.Tf("cmd.role.usage", prefix))
	}
}

func (b *Bot) handleRoleList(s *discordgo.Session, m *discordgo.MessageCreate,
	targetID string, roleSubMap map[string]config.RoleSubscriptionEntry,
	tr interface{ T(string) string; Tf(string, ...interface{}) string },
	membershipOnly bool) {

	var sb strings.Builder
	if membershipOnly {
		sb.WriteString(tr.T("cmd.role.your_roles"))
	} else {
		sb.WriteString(tr.T("cmd.role.available"))
	}
	sb.WriteString(":\n")

	for guildID, entry := range roleSubMap {
		guild, err := s.Guild(guildID)
		if err != nil {
			log.Warnf("discord bot: fetch guild %s for role list: %v", guildID, err)
			continue
		}

		// Try to fetch member (may fail if user not in guild)
		member, err := s.GuildMember(guildID, targetID)
		if err != nil {
			log.Debugf("discord bot: fetch member %s in guild %s: %v", targetID, guildID, err)
			continue
		}

		memberRoles := make(map[string]bool)
		for _, r := range member.Roles {
			memberRoles[r] = true
		}

		sb.WriteString(fmt.Sprintf("**%s**\n", guild.Name))

		// Exclusive role sets
		for _, exSet := range entry.ExclusiveRoles {
			for desc, roleID := range exSet {
				hasRole := memberRoles[roleID]
				if membershipOnly && !hasRole {
					continue
				}
				displayName := strings.ReplaceAll(desc, " ", "_")
				if hasRole {
					sb.WriteString(fmt.Sprintf("   %s  ☑️\n", displayName))
				} else {
					sb.WriteString(fmt.Sprintf("   %s\n", displayName))
				}
			}
			if !membershipOnly {
				sb.WriteByte('\n')
			}
		}

		// General roles
		for desc, roleID := range entry.Roles {
			hasRole := memberRoles[roleID]
			if membershipOnly && !hasRole {
				continue
			}
			displayName := strings.ReplaceAll(desc, " ", "_")
			if hasRole {
				sb.WriteString(fmt.Sprintf("   %s  ☑️\n", displayName))
			} else {
				sb.WriteString(fmt.Sprintf("   %s\n", displayName))
			}
		}
		sb.WriteByte('\n')
	}

	text := sb.String()

	targetChannel := m.ChannelID

	if len(text) > 2000 {
		// Send as file attachment
		s.ChannelMessageSendComplex(targetChannel, &discordgo.MessageSend{
			Content: "Role List",
			Files: []*discordgo.File{{
				Name:   "rolelist.txt",
				Reader: strings.NewReader(text),
			}},
		})
		return
	}
	s.ChannelMessageSend(targetChannel, text)
}

func (b *Bot) handleRoleToggle(s *discordgo.Session, m *discordgo.MessageCreate,
	targetID string, roleArgs []string,
	roleSubMap map[string]config.RoleSubscriptionEntry,
	tr interface{ T(string) string; Tf(string, ...interface{}) string },
	set bool) {

	for _, roleArg := range roleArgs {
		// Skip user override args
		if userRe.MatchString(roleArg) {
			continue
		}

		roleName := strings.ToLower(roleArg)
		found := false

		for guildID, entry := range roleSubMap {
			guild, err := s.Guild(guildID)
			if err != nil {
				continue
			}

			member, err := s.GuildMember(guildID, targetID)
			if err != nil {
				continue
			}
			_ = guild

			memberRoles := make(map[string]bool)
			for _, r := range member.Roles {
				memberRoles[r] = true
			}

			// Check general roles
			for desc, roleID := range entry.Roles {
				if strings.ToLower(strings.ReplaceAll(desc, "_", " ")) == roleName {
					found = true
					if set {
						if err := s.GuildMemberRoleAdd(guildID, targetID, roleID); err != nil {
							log.Warnf("discord bot: add role %s to %s: %v", desc, targetID, err)
						} else {
							s.ChannelMessageSend(m.ChannelID,
								tr.Tf("cmd.role.granted", desc))
						}
					} else {
						if err := s.GuildMemberRoleRemove(guildID, targetID, roleID); err != nil {
							log.Warnf("discord bot: remove role %s from %s: %v", desc, targetID, err)
						} else {
							s.ChannelMessageSend(m.ChannelID,
								tr.Tf("cmd.role.removed", desc))
						}
					}
				}
			}

			// Check exclusive role sets
			for _, exSet := range entry.ExclusiveRoles {
				for desc, roleID := range exSet {
					if strings.ToLower(strings.ReplaceAll(desc, "_", " ")) == roleName {
						found = true
						if set {
							// Add this role and remove others in the same exclusive set
							for otherDesc, otherRoleID := range exSet {
								if otherRoleID == roleID {
									if err := s.GuildMemberRoleAdd(guildID, targetID, roleID); err != nil {
										log.Warnf("discord bot: add exclusive role %s to %s: %v", desc, targetID, err)
									} else {
										s.ChannelMessageSend(m.ChannelID,
											tr.Tf("cmd.role.granted", otherDesc))
									}
								} else if memberRoles[otherRoleID] {
									if err := s.GuildMemberRoleRemove(guildID, targetID, otherRoleID); err != nil {
										log.Warnf("discord bot: remove exclusive role %s from %s: %v", otherDesc, targetID, err)
									} else {
										s.ChannelMessageSend(m.ChannelID,
											tr.Tf("cmd.role.removed", otherDesc))
									}
								}
							}
						} else {
							if err := s.GuildMemberRoleRemove(guildID, targetID, roleID); err != nil {
								log.Warnf("discord bot: remove role %s from %s: %v", desc, targetID, err)
							} else {
								s.ChannelMessageSend(m.ChannelID,
									tr.Tf("cmd.role.removed", desc))
							}
						}
					}
				}
			}
		}

		if !found {
			s.ChannelMessageSend(m.ChannelID,
				tr.Tf("cmd.role.unknown", roleArg))
		}
	}
}
