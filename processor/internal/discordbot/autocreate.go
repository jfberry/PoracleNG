package discordbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// channelTemplate represents one entry in config/channelTemplate.json.
type channelTemplate struct {
	Name       string            `json:"name"`
	Definition channelDefinition `json:"definition"`
}

type channelDefinition struct {
	Category *categoryDefinition `json:"category"`
	Channels []channelEntry      `json:"channels"`
}

type categoryDefinition struct {
	CategoryName string      `json:"categoryName"`
	Roles        []roleEntry `json:"roles"`
}

type channelEntry struct {
	ChannelName string      `json:"channelName"`
	ChannelType string      `json:"channelType"` // "text" or "voice"
	Topic       string      `json:"topic"`
	ControlType string      `json:"controlType"` // "bot" or "webhook"
	WebhookName string      `json:"webhookName"`
	Roles       []roleEntry `json:"roles"`
	Commands    []string    `json:"commands"`

	// New thread-picker fields. Both are optional; an entry with no
	// Threads block behaves exactly as before.
	ThreadPicker *threadPickerDef `json:"threadPicker,omitempty"`
	Threads      []threadEntry    `json:"threads,omitempty"`
}

// threadPickerDef configures the per-master "click to join" embed.
// Strings support {0}-style placeholder expansion against the same
// template args as the rest of channelTemplate.
type threadPickerDef struct {
	EmbedTitle       string `json:"embedTitle"`
	EmbedDescription string `json:"embedDescription"`
	Pinned           bool   `json:"pinned"`
}

// threadEntry is one private thread under a parent text channel.
type threadEntry struct {
	Name        string   `json:"name"`        // thread name (also default button label)
	ButtonLabel string   `json:"buttonLabel"` // optional override
	ButtonStyle string   `json:"buttonStyle"` // "primary" / "secondary" / "success" / "danger" — secondary if blank
	Commands    []string `json:"commands"`    // run as the thread's human at creation
}

type roleEntry struct {
	Name string `json:"name"`

	// Permission flags — true = allow, false = deny, absent = inherit.
	View                 *bool `json:"view"`
	ViewHistory          *bool `json:"viewHistory"`
	Send                 *bool `json:"send"`
	React                *bool `json:"react"`
	PingEveryone         *bool `json:"pingEveryone"`
	EmbedLinks           *bool `json:"embedLinks"`
	AttachFiles          *bool `json:"attachFiles"`
	SendTTS              *bool `json:"sendTTS"`
	ExternalEmoji        *bool `json:"externalEmoji"`
	ExternalStickers     *bool `json:"externalStickers"`
	CreatePublicThreads  *bool `json:"createPublicThreads"`
	CreatePrivateThreads *bool `json:"createPrivateThreads"`
	SendThreads          *bool `json:"sendThreads"`
	SlashCommands        *bool `json:"slashCommands"`
	Connect              *bool `json:"connect"`
	Speak                *bool `json:"speak"`
	AutoMic              *bool `json:"autoMic"`
	Stream               *bool `json:"stream"`
	VCActivities         *bool `json:"vcActivities"`
	PrioritySpeaker      *bool `json:"prioritySpeaker"`
	CreateInvite         *bool `json:"createInvite"`
	Channels             *bool `json:"channels"`
	Messages             *bool `json:"messages"`
	Roles                *bool `json:"roles"`
	Webhooks             *bool `json:"webhooks"`
	Threads              *bool `json:"threads"`
	Events               *bool `json:"events"`
	Mute                 *bool `json:"mute"`
	Deafen               *bool `json:"deafen"`
	Move                 *bool `json:"move"`
}

// rolePermissionFlags maps JSON field names to Discord permission bit values.
var rolePermissionFlags = map[string]int64{
	"view":                 0x0000000000000400, // ViewChannel
	"viewHistory":          0x0000000000010000, // ReadMessageHistory
	"send":                 0x0000000000000800, // SendMessages
	"react":                0x0000000000000040, // AddReactions
	"pingEveryone":         0x0000000000020000, // MentionEveryone
	"embedLinks":           0x0000000000004000, // EmbedLinks
	"attachFiles":          0x0000000000008000, // AttachFiles
	"sendTTS":              0x0000000000001000, // SendTTSMessages
	"externalEmoji":        0x0000000000040000, // UseExternalEmojis
	"externalStickers":     0x0000002000000000, // UseExternalStickers
	"createPublicThreads":  0x0000000800000000, // CreatePublicThreads
	"createPrivateThreads": 0x0000001000000000, // CreatePrivateThreads
	"sendThreads":          0x0000004000000000, // SendMessagesInThreads
	"slashCommands":        0x0000000080000000, // UseApplicationCommands
	"connect":              0x0000000000100000, // Connect
	"speak":                0x0000000000200000, // Speak
	"autoMic":              0x0000000002000000, // UseVAD
	"stream":               0x0000000000000200, // Stream
	"vcActivities":         0x0000008000000000, // UseEmbeddedActivities
	"prioritySpeaker":      0x0000000000000100, // PrioritySpeaker
	"createInvite":         0x0000000000000001, // CreateInstantInvite
	"channels":             0x0000000000000010, // ManageChannels
	"messages":             0x0000000000002000, // ManageMessages
	"roles":                0x0000000010000000, // ManageRoles
	"webhooks":             0x0000000020000000, // ManageWebhooks
	"threads":              0x0000000400000000, // ManageThreads
	"events":               0x0000002000000000, // ManageEvents — Note: shares bit with externalStickers; match JS behavior
	"mute":                 0x0000000000400000, // MuteMembers
	"deafen":               0x0000000000800000, // DeafenMembers
	"move":                 0x0000000001000000, // MoveMembers
}

// handleAutocreate handles the !autocreate command.
// Loads a channel template, creates Discord categories and channels, and registers them.
func (b *Bot) handleAutocreate(s *discordgo.Session, m *discordgo.MessageCreate, args, rawArgs []string) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	guildID := m.GuildID
	if gid := parseGuildArg(args); gid != "" {
		guildID = gid
		// Remove guild arg from args (and rawArgs in lockstep so indices align)
		var filtered, filteredRaw []string
		for i, arg := range args {
			if parseGuildArg([]string{arg}) == "" {
				filtered = append(filtered, arg)
				if i < len(rawArgs) {
					filteredRaw = append(filteredRaw, rawArgs[i])
				}
			}
		}
		args = filtered
		rawArgs = filteredRaw
	}
	// Defensive: keep rawArgs aligned with args even if a caller passed a
	// shorter slice (we want substitutions to fall back to the lowercased
	// value rather than panic on an out-of-range access).
	if len(rawArgs) < len(args) {
		padded := make([]string, len(args))
		copy(padded, rawArgs)
		for i := len(rawArgs); i < len(args); i++ {
			padded[i] = args[i]
		}
		rawArgs = padded
	}

	if guildID == "" {
		s.ChannelMessageSend(m.ChannelID, "No guild has been set, either execute inside a channel or specify guild<id>")
		return
	}

	// Load channel template.
	templatePath := filepath.Join(b.Cfg.BaseDir, "config", "channelTemplate.json")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "No channel templates defined - create config/channelTemplate.json")
		return
	}

	var templates []channelTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to parse channel template: %v", err))
		return
	}

	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "Please specify a template name as the first parameter")
		return
	}

	templateName := args[0]
	// rawSubArgs preserves the original case the user typed (parser
	// otherwise lowercases). Channel/category/topic substitutions read
	// from this so "GentCentrum" stays "GentCentrum" instead of collapsing
	// to "gentcentrum". Discord forces channel names to lowercase server-
	// side anyway, but categories keep their case.
	rawSubArgs := rawArgs[1:]

	var tmpl *channelTemplate
	for i := range templates {
		if templates[i].Name == templateName {
			tmpl = &templates[i]
			break
		}
	}

	if tmpl == nil {
		s.ChannelMessageSend(m.ChannelID, "I can't find that channel template! (remember it has to be your first parameter)")
		return
	}

	// Build substitution args (restore underscores for names) — using
	// rawSubArgs as the source so case is preserved through the round trip.
	subArgsUnder := make([]string, len(rawSubArgs))
	for i, arg := range rawSubArgs {
		subArgsUnder[i] = strings.ReplaceAll(arg, " ", "_")
	}

	// Create category if defined — reuse an existing one with the same
	// name if present, so re-running !autocreate area X groups new
	// channels under the existing "X" category instead of spawning a
	// duplicate. Permission overwrites on a reused category are left
	// alone (changing them retroactively could clobber tweaks the admin
	// made post-creation).
	var categoryID string
	if tmpl.Definition.Category != nil {
		categoryName := formatTemplate(tmpl.Definition.Category.CategoryName, rawSubArgs)

		if existingID := b.findCategoryByName(s, guildID, categoryName); existingID != "" {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Reusing existing category %s", categoryName))
			categoryID = existingID
		} else {
			createData := discordgo.GuildChannelCreateData{
				Name: categoryName,
				Type: discordgo.ChannelTypeGuildCategory,
			}

			if len(tmpl.Definition.Category.Roles) > 0 {
				overwrites := b.buildPermissionOverwrites(s, guildID, tmpl.Definition.Category.Roles, rawSubArgs)
				createData.PermissionOverwrites = overwrites
			}

			cat, err := s.GuildChannelCreateComplex(guildID, createData)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create category %s: %v", categoryName, err))
				return
			}
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Creating %s", categoryName))
			categoryID = cat.ID
		}
	}

	// Create channels.
	for _, chDef := range tmpl.Definition.Channels {
		channelName := formatTemplate(chDef.ChannelName, rawSubArgs)

		createData := discordgo.GuildChannelCreateData{
			Name: channelName,
		}

		switch chDef.ChannelType {
		case "text", "":
			createData.Type = discordgo.ChannelTypeGuildText
		case "voice":
			createData.Type = discordgo.ChannelTypeGuildVoice
		}

		if categoryID != "" {
			createData.ParentID = categoryID
		}

		if chDef.Topic != "" {
			createData.Topic = formatTemplate(chDef.Topic, rawSubArgs)
		}

		if len(chDef.Roles) > 0 {
			createData.PermissionOverwrites = b.buildPermissionOverwrites(s, guildID, chDef.Roles, rawSubArgs)
		}

		// Reuse an existing channel with the same name under the same
		// parent (or top-level if no category) so re-running !autocreate
		// is idempotent. Tracking on the old human row is wiped first
		// — the template's commands re-add it from scratch, so accumu-
		// lated rules from prior runs don't pile up.
		var channel *discordgo.Channel
		if existingID := b.findChannelByName(s, guildID, categoryID, channelName); existingID != "" {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Reusing existing channel %s — resetting tracking", channelName))
			b.resetChannelTracking(s, existingID)
			ch, err := s.Channel(existingID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch existing channel %s: %v", channelName, err))
				continue
			}
			channel = ch
		} else {
			ch, err := s.GuildChannelCreateComplex(guildID, createData)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create channel %s: %v", channelName, err))
				continue
			}
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Creating %s", channelName))
			channel = ch
		}

		// No control type — plain channel, skip registration.
		if chDef.ControlType == "" {
			continue
		}

		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Adding control type: %s", chDef.ControlType))

		var targetID, targetType, targetName string

		if chDef.ControlType == "bot" {
			targetID = channel.ID
			targetType = bot.TypeDiscordChannel
			targetName = formatTemplate(chDef.ChannelName, subArgsUnder)
		} else {
			// Create webhook.
			webhookName := formatTemplate(chDef.ChannelName, subArgsUnder)
			if chDef.WebhookName != "" {
				webhookName = formatTemplate(chDef.WebhookName, subArgsUnder)
			}

			wh, err := s.WebhookCreate(channel.ID, "Poracle", "")
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to create webhook for %s: %v", channelName, err))
				continue
			}

			targetID = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
			targetType = bot.TypeWebhook
			targetName = webhookName
		}

		// Register in DB.
		// CurrentProfileNo: 1 must match the ProfileNo used when running
		// the configured commands below — otherwise the autocreate-time
		// inserts land on profile 1 but BuildTarget reads back 0 from
		// humans.current_profile_no on a later !tracked, finding nothing.
		h := &store.Human{
			ID:               targetID,
			Type:             targetType,
			Name:             targetName,
			Enabled:          true,
			CurrentProfileNo: 1,
		}
		if err := b.Humans.Create(h); err != nil {
			log.Errorf("discord bot: autocreate register %s: %v", targetName, err)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to register %s", targetName))
			continue
		}

		// Create default profile.
		if err := b.Humans.CreateDefaultProfile(targetID, targetName, nil, 0, 0); err != nil {
			log.Warnf("discord bot: autocreate default profile %s: %v", targetName, err)
		}

		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Executing as %s / %s %s",
			targetType, targetName, func() string {
				if targetType != bot.TypeWebhook {
					return targetID
				}
				return ""
			}()))

		// Execute configured commands for the channel.
		// Substitute with quoted values so the bot parser doesn't strip
		// underscores out of names like "gent_centrum" (its normal token
		// underscore→space conversion only applies to unquoted tokens).
		quotedSubArgs := quoteForCommand(subArgsUnder)
		for _, cmdText := range chDef.Commands {
			expanded := formatTemplate(cmdText, quotedSubArgs)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">>> Executing %s", expanded))
			b.runOneAutocreateCommand(s, m, guildID, targetID, targetName, targetType, expanded)
		}

		// Create configured threads under this master channel and emit
		// the picker if one is configured.
		threadEntries := b.createThreadsForChannel(s, m, guildID, channel.ID, chDef, subArgsUnder)
		if chDef.ThreadPicker != nil {
			b.emitPickerPost(s, channel.ID, chDef.ThreadPicker, threadEntries, subArgsUnder)
		}
	}

	// Trigger reload after all creations.
	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}

	s.ChannelMessageSend(m.ChannelID, "Autocreate complete!")
}

// runOneAutocreateCommand parses one !-prefixed command string and
// executes it through the shared registry as the named target. Used by
// both the per-channel and per-thread command-execution loops.
func (b *Bot) runOneAutocreateCommand(s *discordgo.Session, m *discordgo.MessageCreate, guildID, targetID, targetName, targetType, expanded string) {
	parsed := b.Parser.Parse(b.Cfg.Discord.Prefix + expanded)
	for _, pc := range parsed {
		handler := b.Registry.Lookup(pc.CommandKey)
		if handler == nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Unknown command: %s", pc.CommandKey))
			continue
		}

		// Refresh HasArea / HasLocation from the DB before each command.
		// Earlier commands in the same autocreate run (e.g. !area add)
		// mutate the human row, so a stale-false value would otherwise
		// cause the next command to spuriously warn "no area set".
		hasArea, hasLocation := false, false
		if h, err := b.Humans.Get(targetID); err == nil && h != nil {
			hasArea = len(h.Area) > 0
			hasLocation = h.Latitude != 0 || h.Longitude != 0
		}

		ctx := &bot.CommandContext{
			UserID:        m.Author.ID,
			UserName:      m.Author.Username,
			Platform:      "discord",
			ChannelID:     m.ChannelID,
			GuildID:       guildID,
			IsDM:          false,
			IsAdmin:       true,
			Language:      b.Cfg.General.Locale,
			ProfileNo:     1,
			TargetID:      targetID,
			TargetName:    targetName,
			TargetType:    targetType,
			HasArea:       hasArea,
			HasLocation:   hasLocation,
			DB:            b.DB,
			Humans:        b.Humans,
			Tracking:      b.Tracking,
			Config:        b.Cfg,
			StateMgr:      b.StateMgr,
			GameData:      b.GameData,
			Translations:  b.Translations,
			Dispatcher:    b.Dispatcher,
			RowText:       b.RowText,
			Resolver:      b.Resolver,
			ArgMatcher:    b.ArgMatcher,
			Geocoder:      b.Geocoder,
			StaticMap:     b.StaticMap,
			Weather:       b.Weather,
			Stats:         b.Stats,
			DTS:           b.DTS,
			Emoji:         b.Emoji,
			NLP:           b.nlpParser,
			TestProcessor: b.TestProcessor,
			Registry:      b.Registry,
			ReloadFunc:    b.ReloadFunc,
		}

		st := b.StateMgr.Get()
		if st != nil {
			ctx.Geofence = st.Geofence
			ctx.Fences = st.Fences
			ctx.AreaLogic = bot.NewAreaLogic(st.Fences, b.Cfg)
		}

		replies := handler.Run(ctx, pc.Args)
		b.sendReplies(s, m, replies)
	}
}

// createThreadsForChannel iterates chDef.Threads, creates each private
// thread under masterChannelID (or reuses cached ID), registers the
// thread as a discord:thread Poracle human, and runs its commands list
// against the shared execution path. Returns the cache entries for the
// threads it owns so the caller can emit the picker.
func (b *Bot) createThreadsForChannel(s *discordgo.Session, m *discordgo.MessageCreate, guildID, masterChannelID string, chDef channelEntry, subArgs []string) []threadCacheEntry {
	if len(chDef.Threads) == 0 {
		return nil
	}

	cached, _ := b.threadCache.master(masterChannelID)
	cachedByLabel := map[string]threadCacheEntry{}
	if cached != nil {
		for _, e := range cached.Threads {
			cachedByLabel[e.Label] = e
		}
	}

	var entries []threadCacheEntry
	for _, th := range chDef.Threads {
		threadName := formatTemplate(th.Name, subArgs)
		label := th.ButtonLabel
		if label == "" {
			label = threadName
		} else {
			label = formatTemplate(label, subArgs)
		}

		var threadID string
		if existing, ok := cachedByLabel[label]; ok {
			threadID = existing.ThreadID
			// Wipe the existing thread's tracking so the commands below
			// re-add from scratch — same idempotency guarantee as
			// channel reuse. The Humans.Create call further down
			// recreates the human row.
			if err := db.DeleteHumanAndTracking(b.DB, threadID); err != nil {
				log.Warnf("discord bot: autocreate reset thread %s tracking: %v", threadID, err)
			}
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Reusing thread %s (%s) — resetting tracking", threadName, threadID))
		} else {
			created, err := s.ThreadStartComplex(masterChannelID, &discordgo.ThreadStart{
				Name:                threadName,
				Type:                discordgo.ChannelTypeGuildPrivateThread,
				AutoArchiveDuration: 10080,
				Invitable:           false,
			})
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("❌ Failed to create thread %s: %v", threadName, err))
				continue
			}
			threadID = created.ID
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("✅ Created private thread %s (%s)", threadName, threadID))
		}

		// Register a discord:thread human row. CurrentProfileNo: 1 must
		// match the hardcoded ProfileNo used when the per-thread commands
		// run below — otherwise inserts land at profile 1 but later
		// !tracked target-resolution reads 0 from the DB, finding nothing.
		h := &store.Human{
			ID:               threadID,
			Type:             bot.TypeDiscordThread,
			Name:             threadName,
			Enabled:          true,
			CurrentProfileNo: 1,
		}
		if err := b.Humans.Create(h); err != nil {
			// Already-registered is benign on re-runs; log and continue.
			log.Warnf("discord bot: autocreate register thread %s: %v", threadName, err)
		} else {
			_ = b.Humans.CreateDefaultProfile(threadID, threadName, nil, 0, 0)
		}

		// Run the thread's commands against the thread's human. The
		// thread name is appended to subArgs as an extra placeholder so
		// commands can reference it (e.g. `!area add {0}` if the parent
		// args put the area name first). Quote so the bot parser keeps
		// the underscores intact — otherwise area names like
		// "gent_centrum" turn into "gent centrum" and area lookup fails.
		// (Caller passes subArgsUnder here, so subArgs locally is the
		// underscore-restored form.)
		threadArgs := append(append([]string{}, subArgs...), threadName)
		quotedThreadArgs := quoteForCommand(threadArgs)
		for _, cmdText := range th.Commands {
			expanded := formatTemplate(cmdText, quotedThreadArgs)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">>> [%s] %s", threadName, expanded))
			b.runOneAutocreateCommand(s, m, guildID, threadID, threadName, bot.TypeDiscordThread, expanded)
		}

		entry := threadCacheEntry{ThreadID: threadID, Label: label, Style: th.ButtonStyle}
		entries = append(entries, entry)
		b.threadCache.ensureMaster(guildID, masterChannelID)
		b.threadCache.upsertThread(masterChannelID, entry)
		if err := b.threadCache.save(); err != nil {
			log.Warnf("discord bot: persist thread cache: %v", err)
		}
	}
	return entries
}

// buildPermissionOverwrites resolves role names to IDs and builds permission overwrites.
func (b *Bot) buildPermissionOverwrites(s *discordgo.Session, guildID string, roles []roleEntry, args []string) []*discordgo.PermissionOverwrite {
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Warnf("discord bot: autocreate fetch guild %s: %v", guildID, err)
		return nil
	}

	var overwrites []*discordgo.PermissionOverwrite

	for _, role := range roles {
		roleName := formatTemplate(role.Name, args)

		// Find existing role by name.
		var roleID string
		for _, r := range guild.Roles {
			if r.Name == roleName {
				roleID = r.ID
				break
			}
		}

		// Create role if not found.
		if roleID == "" {
			newRole, err := s.GuildRoleCreate(guildID, &discordgo.RoleParams{
				Name: roleName,
			})
			if err != nil {
				log.Warnf("discord bot: autocreate create role %s: %v", roleName, err)
				continue
			}
			roleID = newRole.ID
		}

		allow, deny := computePermissions(role)
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:    roleID,
			Type:  discordgo.PermissionOverwriteTypeRole,
			Allow: allow,
			Deny:  deny,
		})
	}

	return overwrites
}

// computePermissions computes allow and deny permission bit fields from a role entry.
func computePermissions(role roleEntry) (allow, deny int64) {
	checkPerm := func(flag *bool, permName string) {
		if flag == nil {
			return
		}
		bits, ok := rolePermissionFlags[permName]
		if !ok {
			return
		}
		if *flag {
			allow |= bits
		} else {
			deny |= bits
		}
	}

	checkPerm(role.View, "view")
	checkPerm(role.ViewHistory, "viewHistory")
	checkPerm(role.Send, "send")
	checkPerm(role.React, "react")
	checkPerm(role.PingEveryone, "pingEveryone")
	checkPerm(role.EmbedLinks, "embedLinks")
	checkPerm(role.AttachFiles, "attachFiles")
	checkPerm(role.SendTTS, "sendTTS")
	checkPerm(role.ExternalEmoji, "externalEmoji")
	checkPerm(role.ExternalStickers, "externalStickers")
	checkPerm(role.CreatePublicThreads, "createPublicThreads")
	checkPerm(role.CreatePrivateThreads, "createPrivateThreads")
	checkPerm(role.SendThreads, "sendThreads")
	checkPerm(role.SlashCommands, "slashCommands")
	checkPerm(role.Connect, "connect")
	checkPerm(role.Speak, "speak")
	checkPerm(role.AutoMic, "autoMic")
	checkPerm(role.Stream, "stream")
	checkPerm(role.VCActivities, "vcActivities")
	checkPerm(role.PrioritySpeaker, "prioritySpeaker")
	checkPerm(role.CreateInvite, "createInvite")
	checkPerm(role.Channels, "channels")
	checkPerm(role.Messages, "messages")
	checkPerm(role.Roles, "roles")
	checkPerm(role.Webhooks, "webhooks")
	checkPerm(role.Threads, "threads")
	checkPerm(role.Events, "events")
	checkPerm(role.Mute, "mute")
	checkPerm(role.Deafen, "deafen")
	checkPerm(role.Move, "move")

	return allow, deny
}

// emitPickerPost creates or edits the picker message(s) for masterChannelID.
// Idempotent across re-runs: existing message IDs cached from a previous
// run are edited in place; new chunks are sent fresh; chunks no longer
// needed (because the thread count shrank) are deleted. Each step is
// best-effort — a single failure logs and moves on so subsequent chunks
// still get a chance.
func (b *Bot) emitPickerPost(s *discordgo.Session, masterChannelID string, picker *threadPickerDef, entries []threadCacheEntry, args []string) {
	if picker == nil || len(entries) == 0 {
		return
	}
	messages := buildPickerMessages(masterChannelID, picker, entries, args)

	cached, _ := b.threadCache.master(masterChannelID)
	var existingIDs []string
	guildID := ""
	if cached != nil {
		existingIDs = cached.PickerMessageIDs
		guildID = cached.GuildID
	}

	newIDs := make([]string, 0, len(messages))
	for i, msg := range messages {
		// Try to edit the corresponding existing message.
		if i < len(existingIDs) && existingIDs[i] != "" {
			embedsCopy := msg.Embeds
			componentsCopy := msg.Components
			_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel:    masterChannelID,
				ID:         existingIDs[i],
				Embeds:     &embedsCopy,
				Components: &componentsCopy,
			})
			if err == nil {
				newIDs = append(newIDs, existingIDs[i])
				continue
			}
			log.Warnf("discord bot: edit picker %s/%s failed (%v) — posting fresh", masterChannelID, existingIDs[i], err)
		}
		// Send fresh.
		sent, err := s.ChannelMessageSendComplex(masterChannelID, &discordgo.MessageSend{
			Embeds:     msg.Embeds,
			Components: msg.Components,
		})
		if err != nil {
			log.Warnf("discord bot: post picker chunk %d in %s: %v", i, masterChannelID, err)
			continue
		}
		newIDs = append(newIDs, sent.ID)
	}

	// Delete stale messages from a previous run that have no chunk now.
	for i := len(messages); i < len(existingIDs); i++ {
		if existingIDs[i] == "" {
			continue
		}
		if err := s.ChannelMessageDelete(masterChannelID, existingIDs[i]); err != nil {
			log.Warnf("discord bot: delete stale picker %s/%s: %v", masterChannelID, existingIDs[i], err)
		}
	}

	b.threadCache.ensureMaster(guildID, masterChannelID)
	b.threadCache.setPickerMessageIDs(masterChannelID, newIDs)
	if err := b.threadCache.save(); err != nil {
		log.Warnf("discord bot: persist picker message ids: %v", err)
	}

	// Pin the first message only — that's where the embed lives.
	if picker.Pinned && len(newIDs) > 0 {
		if err := s.ChannelMessagePin(masterChannelID, newIDs[0]); err != nil {
			log.Warnf("discord bot: pin picker %s/%s: %v", masterChannelID, newIDs[0], err)
		}
	}
}

// findCategoryByName searches the guild's existing channels for a category
// matching the given name (case-insensitive — Discord categories preserve
// case so a lower-cased compare is the safe form). Returns the channel ID
// of the first match, or empty when no category exists. A failed listing
// also returns empty so the caller falls back to creating fresh.
func (b *Bot) findCategoryByName(s *discordgo.Session, guildID, name string) string {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		log.Warnf("discord bot: autocreate list channels for category lookup: %v", err)
		return ""
	}
	wanted := strings.ToLower(name)
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildCategory && strings.ToLower(ch.Name) == wanted {
			return ch.ID
		}
	}
	return ""
}

// findChannelByName searches the guild's text/voice channels for one whose
// name matches and whose parent category matches. Discord forces channel
// names to lowercase so the comparison is exact-lower. parentID may be
// empty to look for top-level channels. Returns the channel ID, or empty
// when no match (or on listing error).
func (b *Bot) findChannelByName(s *discordgo.Session, guildID, parentID, name string) string {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		log.Warnf("discord bot: autocreate list channels for channel lookup: %v", err)
		return ""
	}
	wanted := strings.ToLower(name)
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildCategory {
			continue
		}
		if ch.ParentID != parentID {
			continue
		}
		if strings.ToLower(ch.Name) == wanted {
			return ch.ID
		}
	}
	return ""
}

// resetChannelTracking wipes any Poracle tracking attached to an existing
// channel — both bot-control (the human row keyed by channel ID) and
// webhook-control (one human row per Poracle webhook on the channel,
// keyed by the webhook URL) — and removes those webhooks. Used when
// !autocreate reuses an existing channel so the template's commands
// re-apply to a clean slate.
func (b *Bot) resetChannelTracking(s *discordgo.Session, channelID string) {
	if err := db.DeleteHumanAndTracking(b.DB, channelID); err != nil {
		log.Warnf("discord bot: autocreate reset channel %s tracking: %v", channelID, err)
	}
	webhooks, err := s.ChannelWebhooks(channelID)
	if err != nil {
		log.Warnf("discord bot: autocreate list webhooks on %s: %v", channelID, err)
		return
	}
	for _, wh := range webhooks {
		// Only touch webhooks Poracle created (named "Poracle"). Leaves
		// any third-party webhooks the operator may have added alone.
		if wh.Name != "Poracle" {
			continue
		}
		url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
		if err := db.DeleteHumanAndTracking(b.DB, url); err != nil {
			log.Warnf("discord bot: autocreate reset webhook %s tracking: %v", wh.ID, err)
		}
		if err := s.WebhookDelete(wh.ID); err != nil {
			log.Warnf("discord bot: autocreate delete webhook %s: %v", wh.ID, err)
		}
	}
}

// formatTemplate replaces {0}, {1}, etc. placeholders with the provided arguments.
func formatTemplate(s string, args []string) string {
	result := s
	for i := len(args) - 1; i >= 0; i-- {
		result = strings.ReplaceAll(result, fmt.Sprintf("{%d}", i), args[i])
	}
	return result
}

// quoteForCommand wraps each value in double quotes so it survives the bot
// parser's token-level underscore→space conversion. Embedded quotes in the
// value are stripped (the parser's tokenRe doesn't handle escaped quotes,
// and it's safer to drop them than emit malformed input). Used by autocreate
// when expanding {N} placeholders into commands like `area add {1}`.
func quoteForCommand(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = `"` + strings.ReplaceAll(a, `"`, "") + `"`
	}
	return out
}
