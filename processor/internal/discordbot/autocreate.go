package discordbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
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

// applyAutocreateOptions controls the per-fence application behaviour.
type applyAutocreateOptions struct {
	// ResetOnReuse, when true, wipes existing tracking on a reused
	// channel/thread and re-runs the template's commands. The interactive
	// !autocreate path sets this to true; the bulk sync path defaults to
	// false (preserve admin-tweaked tracking) and only sets true when the
	// `reset` keyword is on the trigger.
	ResetOnReuse bool

	// DryRun reports what would happen without touching Discord or the DB.
	DryRun bool
}

// applyAutocreateResult captures what one invocation did, for the caller's
// summary. Fields are populated regardless of DryRun.
type applyAutocreateResult struct {
	CategoryID      string                       // resolved or created category id (empty if template has no category)
	ChannelIDs      map[string]string            // channelName -> id
	MasterChannelID string                       // ID of the first channel in the template's Channels slice (stable master)
	ThreadIDs       map[string]map[string]string // channelName -> {label: thread_id}
	Errors          []error
}

// reporter abstracts user feedback. The interactive path uses a Discord
// channel writer; the bulk runner uses a structured collector.
type reporter interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
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

// autocreateActor identifies who is "running" the per-channel/per-thread
// commands. For interactive !autocreate it's the user; for bulk sync it's
// the bot's own identity.
type autocreateActor struct {
	UserID    string
	UserName  string
	ChannelID string // origin channel for replies; empty for bulk
}

// loadChannelTemplates reads and parses config/channelTemplate.json.
func (b *Bot) loadChannelTemplates() ([]channelTemplate, error) {
	path := filepath.Join(b.Cfg.BaseDir, "config", "channelTemplate.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read channel templates: %w", err)
	}
	var templates []channelTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("parse channel templates: %w", err)
	}
	return templates, nil
}

// handleAutocreate handles the !autocreate command.
// Loads a channel template, creates Discord categories and channels, and registers them.
func (b *Bot) handleAutocreate(s *discordgo.Session, m *discordgo.MessageCreate, args, rawArgs []string) {
	if !bot.IsAdmin(b.Cfg, "discord", m.Author.ID) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "🙅")
		return
	}

	// Sync subcommand: bulk-run [[autocreate.rules]] entries.
	if len(args) > 0 && strings.EqualFold(args[0], "sync") {
		b.handleAutocreateSync(s, m, args[1:])
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
	templates, err := b.loadChannelTemplates()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.ChannelMessageSend(m.ChannelID, "No channel templates defined - create config/channelTemplate.json")
		} else {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to parse channel template: %v", err))
		}
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

	// Hand off the per-fence body to applyAutocreate. The interactive path
	// always sets ResetOnReuse=true to preserve today's behaviour
	// (re-running !autocreate area X wipes the channel's existing tracking
	// and re-applies the template's commands).
	subArgs := args[1:]
	actor := &autocreateActor{
		UserID:    m.Author.ID,
		UserName:  m.Author.Username,
		ChannelID: m.ChannelID,
	}
	rep := newDiscordChannelReporter(s, m.ChannelID)
	snap := b.buildGuildSnapshot(guildID)
	result := b.applyAutocreate(s, actor, snap, tmpl, subArgs, rawSubArgs, guildID, rep, applyAutocreateOptions{
		ResetOnReuse: true,
		DryRun:       false,
	})

	// Match the pre-refactor early-return on category-create failure: when the
	// template defines a category and applyAutocreate didn't manage to produce
	// one, treat that as a fatal abort — skip the reload trigger and the
	// completion message so the user sees only the original failure message.
	if tmpl.Definition.Category != nil && result.CategoryID == "" {
		return
	}

	// Trigger reload after all creations.
	if b.ReloadFunc != nil {
		b.ReloadFunc()
	}

	s.ChannelMessageSend(m.ChannelID, "Autocreate complete!")
}

// applyAutocreate runs one channelTemplate entry against the supplied args
// inside the given guild. Used by both the interactive !autocreate command
// and the bulk sync runner. The caller controls reset semantics via
// applyAutocreateOptions.
//
// args is the lower-cased arg slice (matching the bot parser's Args);
// rawArgs preserves user-typed case (matching Parser.RawArgs). Both are
// indexed positionally into the template's {0}, {1}, ... placeholders.
//
// actor identifies who is "running" the commands — for interactive
// !autocreate it's the user; for bulk sync it's the bot's own identity.
//
// snap is the guild's channel/thread snapshot built once by the caller.
// Replaces the per-fence GuildChannels round-trips that used to dominate
// bulk-sync latency (146 fences × 2 lookups = 292 API calls → 1).
// Callers must pass a non-nil snapshot — see Bot.buildGuildSnapshot.
func (b *Bot) applyAutocreate(
	s *discordgo.Session,
	actor *autocreateActor,
	snap *guildSnapshot,
	tmpl *channelTemplate,
	args []string,
	rawArgs []string,
	guildID string,
	rep reporter,
	opts applyAutocreateOptions,
) applyAutocreateResult {
	// Underscore-restored args used by the existing per-channel target/webhook
	// naming code paths.
	subArgsUnder := make([]string, len(rawArgs))
	for i, a := range rawArgs {
		subArgsUnder[i] = strings.ReplaceAll(a, " ", "_")
	}
	result := applyAutocreateResult{
		ChannelIDs: map[string]string{},
		ThreadIDs:  map[string]map[string]string{},
	}

	// Create category if defined — reuse an existing one with the same
	// name if present, so re-running !autocreate area X groups new
	// channels under the existing "X" category instead of spawning a
	// duplicate. Permission overwrites on a reused category are left
	// alone (changing them retroactively could clobber tweaks the admin
	// made post-creation).
	var categoryID string
	if tmpl.Definition.Category != nil {
		categoryName := formatTemplate(tmpl.Definition.Category.CategoryName, rawArgs)

		if existingID := snap.findCategory(categoryName); existingID != "" {
			rep.Info(fmt.Sprintf(">> Reusing existing category %s", categoryName))
			categoryID = existingID
		} else {
			if opts.DryRun {
				rep.Info(fmt.Sprintf(">> [dry-run] Would create category %s", categoryName))
				// Synthetic ID so the next fence in the same sync sees this
				// category as "already there" — without this, every fence in
				// the same category prints "Would create" for the same name.
				categoryID = "(dry-run-cat-" + strings.ToLower(categoryName) + ")"
				snap.addCategory(categoryID, categoryName)
			} else {
				createData := discordgo.GuildChannelCreateData{
					Name: categoryName,
					Type: discordgo.ChannelTypeGuildCategory,
				}

				if len(tmpl.Definition.Category.Roles) > 0 {
					overwrites := b.buildPermissionOverwrites(s, guildID, snap, tmpl.Definition.Category.Roles, rawArgs, opts)
					createData.PermissionOverwrites = overwrites
				}

				cat, err := s.GuildChannelCreateComplex(guildID, createData)
				if err != nil {
					rep.Error(fmt.Sprintf("Failed to create category %s: %v", categoryName, err))
					result.Errors = append(result.Errors, err)
					return result
				}
				rep.Info(fmt.Sprintf(">> Creating %s", categoryName))
				categoryID = cat.ID
				// Make the new category visible to subsequent fences in this
				// sync so they slot into the same category instead of each
				// creating a duplicate "Aalst" / "Aalst" / "Aalst".
				snap.addCategory(cat.ID, categoryName)
			}
		}
	}
	result.CategoryID = categoryID

	// Create channels.
	for _, chDef := range tmpl.Definition.Channels {
		channelName := formatTemplate(chDef.ChannelName, rawArgs)

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
			createData.Topic = formatTemplate(chDef.Topic, rawArgs)
		}

		if len(chDef.Roles) > 0 {
			createData.PermissionOverwrites = b.buildPermissionOverwrites(s, guildID, snap, chDef.Roles, rawArgs, opts)
		}

		// Reuse an existing channel with the same name under the same
		// parent (or top-level if no category) so re-running !autocreate
		// is idempotent. When opts.ResetOnReuse is true (interactive
		// path), tracking on the old human row is wiped first — the
		// template's commands re-add it from scratch, so accumulated
		// rules from prior runs don't pile up. When false (bulk sync
		// path default), the existing channel is reused as-is so admin-
		// tweaked tracking is preserved.
		var channel *discordgo.Channel
		var channelReused bool
		// Look in the chosen category first; if the channel exists somewhere
		// else in the guild (stranded under a stale category from a previous
		// run, or moved by an admin), adopt it and move it into the canonical
		// category rather than creating a duplicate.
		existingID := snap.findChannel(categoryID, channelName)
		var movedFromParent string
		if existingID == "" {
			if anyID, otherParent := snap.findChannelAnyParent(channelName); anyID != "" && otherParent != categoryID {
				existingID = anyID
				movedFromParent = otherParent
			}
		}
		if existingID != "" {
			if movedFromParent != "" {
				if opts.DryRun {
					rep.Info(fmt.Sprintf(">> [dry-run] Would move existing channel %s into category %s", channelName, categoryID))
					snap.removeChannel(existingID, movedFromParent, channelName)
					snap.addChannel(existingID, categoryID, channelName)
				} else {
					if _, err := s.ChannelEditComplex(existingID, &discordgo.ChannelEdit{ParentID: categoryID}); err != nil {
						rep.Warn(fmt.Sprintf("Failed to move channel %s into %s: %v", channelName, categoryID, err))
						result.Errors = append(result.Errors, err)
					} else {
						rep.Info(fmt.Sprintf(">> Moved existing channel %s into %s", channelName, categoryID))
						snap.removeChannel(existingID, movedFromParent, channelName)
						snap.addChannel(existingID, categoryID, channelName)
					}
				}
			}
			if opts.ResetOnReuse {
				rep.Info(fmt.Sprintf(">> Reusing existing channel %s — resetting tracking", channelName))
				if !opts.DryRun {
					b.resetChannelTracking(s, existingID)
				}
			} else {
				rep.Info(fmt.Sprintf(">> Reusing existing channel %s — tracking left alone", channelName))
			}
			// Snapshot already has the full channel metadata — no need to
			// re-fetch via s.Channel(existingID).
			channel = snap.channels[existingID]
			channelReused = true
		} else {
			if opts.DryRun {
				rep.Info(fmt.Sprintf(">> [dry-run] Would create channel %s", channelName))
				// Synthetic channel so the thread-preview path below can still
				// run, and so subsequent fences see this channel under its
				// parent category instead of "would-create"-ing a duplicate.
				dryID := "(dry-run-ch-" + strings.ToLower(channelName) + ")"
				channel = &discordgo.Channel{ID: dryID, Name: channelName, ParentID: categoryID, Type: discordgo.ChannelTypeGuildText}
				snap.addChannel(dryID, categoryID, channelName)
			} else {
				ch, err := s.GuildChannelCreateComplex(guildID, createData)
				if err != nil {
					rep.Warn(fmt.Sprintf("Failed to create channel %s: %v", channelName, err))
					result.Errors = append(result.Errors, err)
					continue
				}
				rep.Info(fmt.Sprintf(">> Creating %s", channelName))
				channel = ch
				snap.addChannel(ch.ID, categoryID, channelName)
			}
		}

		result.ChannelIDs[channelName] = channel.ID
		// The first channel processed is the canonical master channel.
		if result.MasterChannelID == "" {
			result.MasterChannelID = channel.ID
		}

		// No control type — plain channel, skip registration.
		if chDef.ControlType == "" {
			continue
		}

		// Whether to (re-)run the template's per-channel registration and
		// command list. We always run on a fresh channel; on a reused channel
		// only when the caller asked for reset semantics.
		//
		// Thread creation and picker emit are NOT gated here — they run
		// unconditionally so that new threads added to a template after the
		// initial sync materialise on re-sync, and so an out-of-band-deleted
		// picker post gets re-emitted. Both createThreadsForChannel and
		// emitPickerPost are idempotent: they reuse cached IDs and only
		// create/edit as needed.
		runCommands := !channelReused || opts.ResetOnReuse

		if runCommands {
			if opts.DryRun {
				rep.Info(fmt.Sprintf(">> [dry-run] Would add control type: %s for %s", chDef.ControlType, channelName))
				// Fall through to createThreadsForChannel so the dry-run summary
				// shows what threads would be created. All Discord/DB writes inside
				// that function remain guarded by !opts.DryRun.
			} else {
				rep.Info(fmt.Sprintf(">> Adding control type: %s", chDef.ControlType))

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

					wh, err := s.WebhookCreate(channel.ID, bot.PoracleWebhookName, "")
					if err != nil {
						rep.Warn(fmt.Sprintf("Failed to create webhook for %s: %v", channelName, err))
						result.Errors = append(result.Errors, err)
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
					rep.Warn(fmt.Sprintf("Failed to register %s", targetName))
					result.Errors = append(result.Errors, err)
					continue
				}

				// Create default profile.
				if err := b.Humans.CreateDefaultProfile(targetID, targetName, nil, 0, 0); err != nil {
					log.Warnf("discord bot: autocreate default profile %s: %v", targetName, err)
				}

				rep.Info(fmt.Sprintf(">> Executing as %s / %s %s",
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
					rep.Info(fmt.Sprintf(">>> Executing %s", expanded))
					b.runOneAutocreateCommand(s, actor, rep, guildID, targetID, targetName, targetType, expanded)
				}
			}
		} else {
			// Registration and commands are skipped for a reused channel that
			// the caller did not ask to reset. Threads and picker still run
			// below so that template additions and deleted picker posts are
			// caught on every sync.
			rep.Info(fmt.Sprintf(">> Skipping command re-run for reused channel %s", channelName))
		}

		// Create configured threads under this master channel and emit
		// the picker if one is configured. Runs unconditionally regardless
		// of runCommands — both functions are idempotent: createThreadsForChannel
		// reuses cached thread IDs and only creates new ones; emitPickerPost
		// edits the existing picker post or posts a fresh one as needed.
		// Both functions respect opts.DryRun internally so a preview run
		// surfaces would-create / would-refresh entries without touching
		// Discord or the cache.
		threadEntries := b.createThreadsForChannel(s, actor, rep, guildID, channel.ID, chDef, subArgsUnder, opts, &result)
		if chDef.ThreadPicker != nil {
			b.emitPickerPost(s, rep, channel.ID, chDef.ThreadPicker, threadEntries, subArgsUnder, opts)
		}
		if len(threadEntries) > 0 {
			labelMap := result.ThreadIDs[channelName]
			if labelMap == nil {
				labelMap = map[string]string{}
			}
			for _, e := range threadEntries {
				labelMap[e.Label] = e.ThreadID
			}
			result.ThreadIDs[channelName] = labelMap
		}
	}

	return result
}

// runOneAutocreateCommand parses one !-prefixed command string and
// executes it through the shared registry as the named target. Used by
// both the per-channel and per-thread command-execution loops.
// rep is used to surface unknown-command warnings in both interactive and
// bulk paths (the bulk path has no Discord channel to send to directly).
func (b *Bot) runOneAutocreateCommand(s *discordgo.Session, actor *autocreateActor, rep reporter, guildID, targetID, targetName, targetType, expanded string) {
	parsed := b.Parser.Parse(b.Cfg.Discord.Prefix + expanded)
	for _, pc := range parsed {
		handler := b.Registry.Lookup(pc.CommandKey)
		if handler == nil {
			rep.Warn(fmt.Sprintf("Unknown command: %s", pc.CommandKey))
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
			UserID:        actor.UserID,
			UserName:      actor.UserName,
			Platform:      "discord",
			ChannelID:     actor.ChannelID,
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
		// For the interactive path (actor has a ChannelID) send replies back
		// to the originating channel. For the bulk path (no ChannelID) there
		// is no Discord message to reference, so replies are discarded here —
		// the reporter collects all user-visible output instead.
		if actor.ChannelID != "" {
			synth := &discordgo.MessageCreate{Message: &discordgo.Message{
				ChannelID: actor.ChannelID,
				Author:    &discordgo.User{ID: actor.UserID, Username: actor.UserName},
			}}
			b.sendReplies(s, synth, replies)
		}
	}
}

// createThreadsForChannel iterates chDef.Threads, creates each private
// thread under masterChannelID (or reuses cached ID), registers the
// thread as a discord:thread Poracle human, and runs its commands list
// against the shared execution path. Returns the cache entries for the
// threads it owns so the caller can emit the picker.
//
// rep emits user-visible progress for the active path (interactive Discord
// channel writer or bulk-runner collector). opts controls whether existing
// thread tracking is reset on reuse and whether DB/Discord writes happen.
// result, when non-nil, is incremented for each successfully executed
// thread command (so the caller's summary reports an accurate total).
func (b *Bot) createThreadsForChannel(
	s *discordgo.Session,
	actor *autocreateActor,
	rep reporter,
	guildID, masterChannelID string,
	chDef channelEntry,
	subArgs []string,
	opts applyAutocreateOptions,
	result *applyAutocreateResult,
) []threadCacheEntry {
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
		threadReused := false
		if existing, ok := cachedByLabel[label]; ok {
			threadID = existing.ThreadID
			threadReused = true
			// Wipe the existing thread's tracking when the caller asked
			// for reset semantics (interactive !autocreate path). Bulk
			// sync defaults to leaving tracking alone so admin tweaks
			// survive scheduled re-syncs. The Humans.Create call further
			// down only fires when we wiped (otherwise we'd hit a noisy
			// "already-registered" warning).
			if opts.ResetOnReuse {
				if !opts.DryRun {
					if err := db.DeleteHumanAndTracking(b.DB, threadID); err != nil {
						log.Warnf("discord bot: autocreate reset thread %s tracking: %v", threadID, err)
					}
				}
				rep.Info(fmt.Sprintf(">> Reusing thread %s (%s) — resetting tracking", threadName, threadID))
			} else {
				rep.Info(fmt.Sprintf(">> Reusing thread %s (%s) — tracking left alone", threadName, threadID))
			}
		} else {
			if opts.DryRun {
				rep.Info(fmt.Sprintf(">> [dry-run] Would create private thread %s", threadName))
				// Use a visibly synthetic ID so result.ThreadIDs shows what
				// would be created without touching Discord or the DB.
				threadID = "(dry-run)"
				// Fall through to the entry-creation block below.
			} else {
				created, err := s.ThreadStartComplex(masterChannelID, &discordgo.ThreadStart{
					Name:                threadName,
					Type:                discordgo.ChannelTypeGuildPrivateThread,
					AutoArchiveDuration: 10080,
					Invitable:           false,
				})
				if err != nil {
					rep.Warn(fmt.Sprintf("❌ Failed to create thread %s: %v", threadName, err))
					if result != nil {
						result.Errors = append(result.Errors, err)
					}
					continue
				}
				threadID = created.ID
				rep.Info(fmt.Sprintf("✅ Created private thread %s (%s)", threadName, threadID))
			}
		}

		// Decide whether to (re-)run the thread's per-thread command
		// list. Always for a freshly created thread; on a reused thread
		// only when the caller asked for reset.
		runThreadCommands := !threadReused || opts.ResetOnReuse

		if runThreadCommands && !opts.DryRun {
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
				rep.Info(fmt.Sprintf(">>> [%s] %s", threadName, expanded))
				b.runOneAutocreateCommand(s, actor, rep, guildID, threadID, threadName, bot.TypeDiscordThread, expanded)
			}
		}

		entry := threadCacheEntry{ThreadID: threadID, Label: label, Style: th.ButtonStyle}
		entries = append(entries, entry)
		if !opts.DryRun {
			b.threadCache.ensureMaster(guildID, masterChannelID)
			b.threadCache.upsertThread(masterChannelID, entry)
			if err := b.threadCache.save(); err != nil {
				log.Warnf("discord bot: persist thread cache: %v", err)
			}
		}
	}
	return entries
}

// buildPermissionOverwrites resolves role names to IDs and builds permission overwrites.
// snap is used to avoid a per-call s.GuildRoles round-trip — the snapshot is built once
// per sync in buildGuildSnapshot and passed down through applyAutocreate.
func (b *Bot) buildPermissionOverwrites(s *discordgo.Session, guildID string, snap *guildSnapshot, roles []roleEntry, args []string, opts applyAutocreateOptions) []*discordgo.PermissionOverwrite {
	var overwrites []*discordgo.PermissionOverwrite

	for _, role := range roles {
		roleName := formatTemplate(role.Name, args)

		// Find existing role by name via snapshot (O(1), no Discord round-trip).
		roleID := snap.findRole(roleName)

		// Create role if not found.
		if roleID == "" {
			if opts.DryRun {
				// On dry-run, log that the role would be created and skip the
				// permission overwrite — GuildRoleCreate must not fire on a
				// dry-run path.
				log.Infof("discord bot: autocreate [dry-run] would create role %s in guild %s", roleName, guildID)
				continue
			}
			newRole, err := s.GuildRoleCreate(guildID, &discordgo.RoleParams{
				Name: roleName,
			})
			if err != nil {
				log.Warnf("discord bot: autocreate create role %s: %v", roleName, err)
				continue
			}
			roleID = newRole.ID
			// Push the new role into the snapshot so subsequent fences in
			// the same sync see it and don't attempt a duplicate create.
			snap.addRole(roleID, roleName)
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
func (b *Bot) emitPickerPost(s *discordgo.Session, rep reporter, masterChannelID string, picker *threadPickerDef, entries []threadCacheEntry, args []string, opts applyAutocreateOptions) {
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

	// Dry-run: report what would happen without touching Discord or the
	// cache. We can't know whether the cached message IDs still resolve
	// in Discord without an API round-trip, so we report "would refresh"
	// and trust the real sync to fall back to "post fresh" if the edit
	// fails.
	if opts.DryRun {
		fresh := 0
		refresh := 0
		for i := range messages {
			if i < len(existingIDs) && existingIDs[i] != "" {
				refresh++
			} else {
				fresh++
			}
		}
		stale := 0
		for i := len(messages); i < len(existingIDs); i++ {
			if existingIDs[i] != "" {
				stale++
			}
		}
		if fresh > 0 {
			rep.Info(fmt.Sprintf(">> [dry-run] Would post %d new picker message(s) in %s", fresh, masterChannelID))
		}
		if refresh > 0 {
			rep.Info(fmt.Sprintf(">> [dry-run] Would refresh %d existing picker message(s) in %s", refresh, masterChannelID))
		}
		if stale > 0 {
			rep.Info(fmt.Sprintf(">> [dry-run] Would delete %d stale picker message(s) in %s", stale, masterChannelID))
		}
		if picker.Pinned && (fresh > 0 || refresh > 0) {
			rep.Info(fmt.Sprintf(">> [dry-run] Would pin first picker message in %s", masterChannelID))
		}
		return
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

// resetChannelTracking wipes any Poracle tracking attached to an existing
// channel — both bot-control (the human row keyed by channel ID) and
// webhook-control (one human row per Poracle webhook on the channel,
// keyed by the webhook URL) — and removes those webhooks. The channel
// itself is left in Discord. Used when !autocreate reuses an existing
// channel so the template's commands re-apply to a clean slate.
func (b *Bot) resetChannelTracking(s *discordgo.Session, channelID string) {
	// Errors are already logged by cascadeChannelDelete; reset is a
	// best-effort path with no caller summary to populate, so we can
	// safely discard the returned slice here.
	_ = b.cascadeChannelDelete(s, channelID, false)
}

// cascadeChannelDelete removes Poracle's DB and Discord state for a single
// channel. Per-thread deletion is the caller's responsibility (Discord
// cascades thread channels when the parent is deleted, but DB rows do not).
//
// Steps:
//   - List the channel's webhooks; for each named PoracleWebhookName,
//     delete the human row keyed by the webhook URL and delete the
//     webhook in Discord.
//   - Delete the human row keyed by the channel ID.
//   - If deleteChannel is true, delete the channel in Discord too.
//
// Errors are accumulated and returned so callers can surface them in
// per-fence results (the runner shows them in result.Errors). Each error
// is also logged. Partial cascades are preferable to leaving corrupt
// state, so each step continues past errors in earlier ones.
func (b *Bot) cascadeChannelDelete(s *discordgo.Session, channelID string, deleteChannel bool) []error {
	var errs []error
	webhooks, err := s.ChannelWebhooks(channelID)
	if err != nil {
		log.Warnf("discord bot: autocreate list webhooks on %s: %v", channelID, err)
		errs = append(errs, fmt.Errorf("list webhooks on %s: %w", channelID, err))
	}
	for _, wh := range webhooks {
		// Only touch webhooks Poracle created (named "Poracle"). Leaves any
		// third-party webhooks the operator may have added alone.
		if wh.Name != bot.PoracleWebhookName {
			continue
		}
		url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
		if err := db.DeleteHumanAndTracking(b.DB, url); err != nil {
			log.Warnf("discord bot: autocreate delete webhook %s tracking: %v", wh.ID, err)
			errs = append(errs, fmt.Errorf("delete webhook %s tracking: %w", wh.ID, err))
		}
		if err := s.WebhookDelete(wh.ID); err != nil {
			log.Warnf("discord bot: autocreate delete webhook %s: %v", wh.ID, err)
			errs = append(errs, fmt.Errorf("delete webhook %s: %w", wh.ID, err))
		}
	}
	if err := db.DeleteHumanAndTracking(b.DB, channelID); err != nil {
		log.Warnf("discord bot: autocreate delete channel %s tracking: %v", channelID, err)
		errs = append(errs, fmt.Errorf("delete channel %s tracking: %w", channelID, err))
	}
	if deleteChannel {
		if _, err := s.ChannelDelete(channelID); err != nil {
			log.Warnf("discord bot: autocreate delete Discord channel %s: %v", channelID, err)
			errs = append(errs, fmt.Errorf("delete Discord channel %s: %w", channelID, err))
		}
	}
	return errs
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

// discordChannelReporter implements reporter by sending each message to a
// Discord channel via the active session. Used by the interactive
// !autocreate path so the user sees live progress in the channel.
type discordChannelReporter struct {
	s         *discordgo.Session
	channelID string
}

func newDiscordChannelReporter(s *discordgo.Session, channelID string) reporter {
	return &discordChannelReporter{s: s, channelID: channelID}
}

func (r *discordChannelReporter) Info(msg string)  { r.s.ChannelMessageSend(r.channelID, msg) }
func (r *discordChannelReporter) Warn(msg string)  { r.s.ChannelMessageSend(r.channelID, msg) }
func (r *discordChannelReporter) Error(msg string) { r.s.ChannelMessageSend(r.channelID, msg) }

// handleAutocreateSync runs bulk syncs over [[autocreate.rules]]. Admin-
// only (gated by handleAutocreate's preamble). Arg shape:
//
//	<rule-name>?  <flag>* (where flag ∈ {dryrun, reset, removals, force},
//	                       translatable, order-independent)
//
// Empty rule name = run every rule in turn.
func (b *Bot) handleAutocreateSync(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	tr := b.Translations.For(b.Cfg.General.Locale)

	var ruleName string
	opts := SyncRuleOptions{}
	for _, a := range args {
		al := strings.ToLower(a)
		switch {
		case al == "dryrun" || al == strings.ToLower(tr.T("arg.dryrun")):
			opts.DryRun = true
		case al == "reset" || al == strings.ToLower(tr.T("arg.reset")):
			opts.Reset = true
		case al == "removals" || al == strings.ToLower(tr.T("arg.removals")):
			opts.Removals = true
		case al == "force" || al == strings.ToLower(tr.T("arg.force")):
			opts.Force = true
		default:
			if ruleName != "" {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Unknown argument: %s", a))
				return
			}
			ruleName = a
		}
	}

	rules := b.Cfg.Autocreate.Rules
	if ruleName != "" {
		var match *config.AutocreateRule
		for i := range rules {
			if rules[i].Name == ruleName {
				match = &rules[i]
				break
			}
		}
		if match == nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No autocreate rule named %q", ruleName))
			return
		}
		rules = []config.AutocreateRule{*match}
	}

	if len(rules) == 0 {
		s.ChannelMessageSend(m.ChannelID, "No [[autocreate.rules]] configured")
		return
	}

	for _, r := range rules {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(">> Sync %s (dry-run=%v reset=%v removals=%v force=%v)",
			r.Name, opts.DryRun, opts.Reset, opts.Removals, opts.Force))
		result := b.SyncOneRule(s, r, opts)
		s.ChannelMessageSend(m.ChannelID, formatSyncSummary(result))
	}
}

// formatSyncSummary produces the per-rule summary the user sees in the
// channel after a sync run.
func formatSyncSummary(r SyncOneRuleResult) string {
	var b strings.Builder
	matched := len(r.Created) + len(r.Reused)
	fmt.Fprintf(&b, "Sync %s — %d fences matched\n", r.Rule, matched)
	fmt.Fprintf(&b, "Created:  %d\n", len(r.Created))
	fmt.Fprintf(&b, "Reused:   %d\n", len(r.Reused))
	fmt.Fprintf(&b, "Removed:  %d\n", len(r.Removed))
	fmt.Fprintf(&b, "Orphans:  %d\n", len(r.Orphans))
	fmt.Fprintf(&b, "Skipped:  %d\n", len(r.Skipped))
	fmt.Fprintf(&b, "Errors:   %d\n", len(r.Errors))
	// Surface per-removal reasons when present — this is where "Removed: 1
	// but Discord channel still alive" gets explained (cascade error,
	// reconcile-cleared cache, remove_missing off, etc.).
	for _, e := range r.Removed {
		if e.Reason == "" {
			continue
		}
		fmt.Fprintf(&b, "  - %s: %s\n", e.Fence, e.Reason)
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", r.Note)
	}
	if r.DryRun {
		b.WriteString("(dry run — nothing was changed)\n")
	}
	return b.String()
}
