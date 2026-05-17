// Package bot provides the platform-agnostic command framework for Discord and
// Telegram bot commands. Commands receive structured ParsedArgs (not raw strings)
// and return Reply slices. The framework reuses existing processor packages for
// database operations, diff logic, row descriptions, game data, and translations.
package bot

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/logbuffer"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// BotDeps holds shared dependencies needed by both Discord and Telegram bots.
// Platform-specific bot Config structs embed this to avoid duplication.
type BotDeps struct {
	DB            *sqlx.DB
	Humans        store.HumanStore
	Tracking      *store.TrackingStores
	Cfg           *config.Config
	StateMgr      *state.Manager
	GameData      *gamedata.GameData
	Translations  *i18n.Bundle
	Dispatcher    *delivery.Dispatcher
	RowText       *rowtext.Generator
	Registry      *Registry
	Parser        *Parser
	ArgMatcher    *ArgMatcher
	Resolver      *PokemonResolver
	Geocoder      *geocoding.Geocoder
	StaticMap     *staticmap.Resolver
	Weather       *tracker.WeatherTracker
	Stats         *tracker.StatsTracker
	DTS           *dts.TemplateStore
	Emoji         *dts.EmojiLookup
	NLPParser     *nlp.Parser
	TestProcessor TestProcessor
	ReloadFunc    func()
	// SummarySchedules backs the !summary command's CRUD operations.
	// nil disables the command (e.g. when quest_summary_enabled=false).
	SummarySchedules store.SummaryScheduleStore
	// SummaryBuffer is the raw summary buffer, used by !poracle-admin summary
	// subcommands (list/show/fire). May be nil in test contexts.
	SummaryBuffer *tracker.SummaryBuffer
	// SummaryBufferCount returns how many entries are currently buffered
	// for (humanID, alertType). Used by !summary to show status.
	// nil treated as zero by the command.
	SummaryBufferCount func(humanID, alertType string) int
	// SummaryDispatch forces immediate dispatch of the buffer for
	// (humanID, alertType). Bound to ProcessorService.DispatchQuestSummary
	// in main. nil treated as a no-op by the command.
	SummaryDispatch func(humanID, alertType string)
	// Scanner is the optional scanner-DB handle used by gym-aware
	// commands (!raid, !gym, !egg with a `gym:` argument). nil when
	// no scanner is configured — commands check and reject `gym:`
	// usage with a clear error.
	Scanner scanner.Scanner

	// RecentActivity tracks recently-seen pokemon/items/grunts (6h TTL)
	// used by slash autocomplete to prioritise currently-active entities.
	// Always non-nil; populated by webhook handlers post-dedup.
	RecentActivity *tracker.RecentActivity

	// Admin reload functions — wired by ProcessorService in main.go.
	// Called directly by !poracle-admin reload subcommands; they bypass
	// triggerReload's 500ms debouncer on purpose (operator typed it).
	ReloadDTS      func() (int, error) // returns template count
	ReloadGeofence func() error        // calls state.LoadWithGeofences
	ReloadState    func() error        // calls state.Load directly
	// EmojiReload reloads config/emoji.json and returns the key count.
	// nil when DTS renderer is not configured.
	EmojiReload func() (int, error)

	// EmojiOperation runs an emoji upload + emoji.json generation against
	// the caller's Discord guild. With upload=false it dumps the current
	// guild state as emoji.json (no writes). With upload+overwrite it
	// deletes and re-uploads existing emojis. Streams progress messages
	// directly to channelID via the gateway session; returns the final
	// emoji.json attachment in the reply slice. nil on Telegram-only deploys
	// or when the Discord bot is not configured.
	EmojiOperation func(channelID, guildID string, upload, overwrite bool) []Reply

	// Phase 2 introspection APIs — used by admin status/diagnostic subcommands.

	// WebhookRate returns a point-in-time snapshot of webhook arrival rates.
	// Always non-nil in production.
	WebhookRate func() webhook.RateSnapshot

	// AlertLimiter is the per-destination alert rate limiter.
	// Always non-nil in production.
	AlertLimiter *ratelimit.Limiter

	// DiscordRate returns a point-in-time snapshot of Discord API rate-limit
	// state. Always non-nil in production; returns a zero-value snapshot
	// when Discord delivery is not configured.
	DiscordRate func() delivery.DiscordRateSnapshot

	// TelegramRate returns a point-in-time snapshot of Telegram API rate-limit
	// state. Always non-nil in production; returns a zero-value snapshot
	// when Telegram delivery is not configured.
	TelegramRate func() delivery.TelegramRateSnapshot

	// GeocoderStats returns a point-in-time snapshot of geocoder cache health.
	// Always non-nil in production; returns a zero-value snapshot when no
	// geocoder is configured.
	GeocoderStats func() geocoding.CacheStats

	// GeocoderClear drops all entries from the in-memory geocoder cache layer.
	// Returns the number of entries cleared. Always non-nil in production.
	GeocoderClear func() int

	// Reconciler immediately reconciles a single Discord user's role membership.
	// Always non-nil. Returns discordbot.ErrReconciliationDisabled when Discord
	// reconciliation is not configured (Telegram-only deploys, or check_role=false).
	// Use errors.Is(err, discordbot.ErrReconciliationDisabled) to detect.
	Reconciler func(userID string) error

	// RunReconcile runs the full Discord role + channel reconciliation immediately
	// instead of waiting for the periodic timer. Always non-nil. Returns
	// discordbot.ErrReconciliationDisabled when Discord reconciliation is not
	// configured. Use errors.Is(err, discordbot.ErrReconciliationDisabled) to detect.
	RunReconcile func() error

	// LogBuffer holds the in-memory startup + rolling log capture.
	// Always non-nil in production.
	LogBuffer *logbuffer.Buffer

	// ProcessStart is the time at which the processor's main service
	// was constructed. Surfaced for uptime display in !poracle-admin status.
	// Zero value is acceptable (uptime will be reported as 0).
	ProcessStart time.Time

	// Slash command lifecycle — used by !poracle-admin slash subcommands.
	// All five are always non-nil in production. When slash is not
	// configured (Discord disabled, or [discord.slash_commands] enabled=false)
	// the closures return slash.ErrSlashNotConfigured.
	//
	// SlashStatus reads the on-disk fingerprint cache; all other operations
	// mutate Discord via the slash Dispatcher.
	SlashSync        func() error
	SlashForceResync func() error
	SlashClearGlobal func() error
	SlashClearGuild  func(guildID string) error
	SlashStatus      func() (global SlashScope, guilds []SlashScope, err error)
}

// SlashScope is a per-scope snapshot of slash registration state.
// "global" or a specific guild ID for Name.
// Defined here (not in internal/discordbot/slash) to avoid an import cycle:
// the slash package already imports internal/bot.
type SlashScope struct {
	Name         string    // "global" or guild ID
	LastSyncedAt time.Time // zero if never synced
	Fingerprint  string    // first 8 chars, or empty if never synced
}

// TestTarget specifies who to deliver a test alert to.
type TestTarget struct {
	ID        string
	Name      string
	Type      string // discord:user, telegram:user, etc.
	Language  string
	Template  string
	Latitude  float64
	Longitude float64
}

// TestProcessor processes a single test webhook through the enrichment pipeline.
type TestProcessor interface {
	ProcessTest(webhookType string, raw json.RawMessage, target TestTarget) error
}

// Command is implemented by every bot command handler.
type Command interface {
	// Name returns the primary identifier key (e.g. "cmd.track").
	Name() string
	// Aliases returns additional identifier keys that also invoke this command.
	Aliases() []string
	// Run executes the command. Args are the remaining tokens after the command
	// name has been stripped. Commands should use ctx.ArgMatcher to parse args
	// into structured values rather than doing their own regex parsing.
	Run(ctx *CommandContext, args []string) []Reply
}

// CommandContext carries user info, permissions, and injected dependencies.
// It is built fresh for each command invocation by the bot or API handler.
type CommandContext struct {
	// User identity (who sent the message)
	UserID    string
	UserName  string
	Platform  string // "discord" or "telegram"
	ChannelID string
	GuildID   string // Discord only, empty for Telegram
	IsDM      bool
	IsSlash   bool // Discord slash command invocation (vs. text message)

	// Permissions (resolved before command runs)
	IsAdmin          bool
	IsCommunityAdmin bool
	IsRegistered     bool
	Permissions      Permissions

	// User state (loaded from DB before command runs)
	Language    string
	ProfileNo   int
	HasLocation bool
	HasArea     bool

	// Target (who the command operates on — defaults to the sender)
	TargetID   string
	TargetName string
	TargetType string // "discord:user", "discord:channel", "webhook", "telegram:user", etc.

	// Pure-logic helpers (no DB access)
	AreaLogic *AreaLogic

	// Injected dependencies
	DB            *sqlx.DB // DEPRECATED — use Humans/Tracking stores. Kept during migration.
	Humans        store.HumanStore
	Tracking      *store.TrackingStores
	Config        *config.Config
	StateMgr      *state.Manager
	GameData      *gamedata.GameData
	Translations  *i18n.Bundle
	Geofence      *geofence.SpatialIndex
	Fences        []geofence.Fence
	Dispatcher    *delivery.Dispatcher
	RowText       *rowtext.Generator
	Resolver      *PokemonResolver
	ArgMatcher    *ArgMatcher
	Geocoder      *geocoding.Geocoder
	StaticMap     *staticmap.Resolver
	Weather       *tracker.WeatherTracker
	Stats         *tracker.StatsTracker
	DTS           *dts.TemplateStore
	Emoji         *dts.EmojiLookup
	NLP           *nlp.Parser
	TestProcessor TestProcessor
	Registry      *Registry
	// SummarySchedules backs the !summary command's CRUD ops. nil disables
	// the !summary command when the feature flag is off.
	SummarySchedules store.SummaryScheduleStore
	// SummaryBuffer is the raw buffer used by !poracle-admin summary subcommands.
	// May be nil in test contexts or when quest summaries are disabled.
	SummaryBuffer *tracker.SummaryBuffer
	// SummaryBufferCount returns buffered entry count for status display.
	// nil treated as zero by the command.
	SummaryBufferCount func(humanID, alertType string) int
	// SummaryDispatch fires immediate buffer dispatch on `!summary <type> now`.
	// nil treated as a no-op by the command.
	SummaryDispatch func(humanID, alertType string)
	// Scanner is the optional scanner-DB handle. nil when no scanner
	// is configured.
	Scanner scanner.Scanner

	// Reload trigger — called after tracking mutations
	ReloadFunc func()

	// Admin reload functions — live-ops commands only.
	// Each bypasses the debouncer and executes synchronously.
	ReloadDTS      func() (int, error) // returns template count
	ReloadGeofence func() error
	ReloadState    func() error
	// EmojiReload reloads config/emoji.json and returns the key count.
	// nil when DTS renderer is not configured.
	EmojiReload func() (int, error)

	// EmojiOperation — see BotDeps.EmojiOperation.
	EmojiOperation func(channelID, guildID string, upload, overwrite bool) []Reply

	// Phase 2 introspection APIs — used by admin status/diagnostic subcommands.

	// WebhookRate returns a point-in-time snapshot of webhook arrival rates.
	WebhookRate func() webhook.RateSnapshot

	// AlertLimiter is the per-destination alert rate limiter.
	AlertLimiter *ratelimit.Limiter

	// DiscordRate returns a point-in-time snapshot of Discord API rate-limit state.
	DiscordRate func() delivery.DiscordRateSnapshot

	// TelegramRate returns a point-in-time snapshot of Telegram API rate-limit state.
	TelegramRate func() delivery.TelegramRateSnapshot

	// GeocoderStats returns a point-in-time snapshot of geocoder cache health.
	GeocoderStats func() geocoding.CacheStats

	// GeocoderClear drops all entries from the in-memory geocoder cache layer.
	GeocoderClear func() int

	// Reconciler immediately reconciles a single Discord user's role membership.
	// Always non-nil. Returns discordbot.ErrReconciliationDisabled when Discord
	// reconciliation is not configured (Telegram-only deploys, or check_role=false).
	// Use errors.Is(err, discordbot.ErrReconciliationDisabled) to detect.
	Reconciler func(userID string) error

	// RunReconcile runs the full Discord role + channel reconciliation immediately
	// instead of waiting for the periodic timer. Always non-nil. Returns
	// discordbot.ErrReconciliationDisabled when Discord reconciliation is not
	// configured. Use errors.Is(err, discordbot.ErrReconciliationDisabled) to detect.
	RunReconcile func() error

	// LogBuffer holds the in-memory startup + rolling log capture.
	LogBuffer *logbuffer.Buffer

	// ProcessStart is the time at which the processor's main service was
	// constructed. Used by !poracle-admin status to compute uptime.
	ProcessStart time.Time

	// Slash command lifecycle — used by !poracle-admin slash subcommands.
	// Always non-nil in production; return slash.ErrSlashNotConfigured when
	// Discord is disabled or slash commands are not enabled.
	SlashSync        func() error
	SlashForceResync func() error
	SlashClearGlobal func() error
	SlashClearGuild  func(guildID string) error
	SlashStatus      func() (global SlashScope, guilds []SlashScope, err error)

	// PostRegister, when set, is invoked after !poracle creates a new
	// human row. The platform sets this to its single-user reconciliation
	// hook so a freshly-registered user has their community_membership /
	// area_restriction populated from current Discord roles or Telegram
	// channel memberships immediately, rather than waiting for the next
	// periodic sweep. nil when reconciliation isn't configured.
	PostRegister func(userID string)

	// languageHint from available_languages (set by bot when command uses a language variant)
	languageHint string
}

// NewCommandContext returns a CommandContext seeded with every BotDeps
// field, plus the static config pointer. Every command run on every
// surface (text bot, slash bot, Telegram bot) is constructed through
// here so additions to BotDeps reach all surfaces in one place.
// Returns an empty CommandContext when deps is nil.
func NewCommandContext(deps *BotDeps) *CommandContext {
	if deps == nil {
		return &CommandContext{}
	}
	return &CommandContext{
		Config:             deps.Cfg,
		DB:                 deps.DB,
		Humans:             deps.Humans,
		Tracking:           deps.Tracking,
		StateMgr:           deps.StateMgr,
		GameData:           deps.GameData,
		Translations:       deps.Translations,
		Dispatcher:         deps.Dispatcher,
		RowText:            deps.RowText,
		Resolver:           deps.Resolver,
		ArgMatcher:         deps.ArgMatcher,
		Geocoder:           deps.Geocoder,
		StaticMap:          deps.StaticMap,
		Weather:            deps.Weather,
		Stats:              deps.Stats,
		DTS:                deps.DTS,
		Emoji:              deps.Emoji,
		NLP:                deps.NLPParser,
		TestProcessor:      deps.TestProcessor,
		Registry:           deps.Registry,
		Scanner:            deps.Scanner,
		ReloadFunc:         deps.ReloadFunc,
		SummarySchedules:   deps.SummarySchedules,
		SummaryBuffer:      deps.SummaryBuffer,
		SummaryBufferCount: deps.SummaryBufferCount,
		SummaryDispatch:    deps.SummaryDispatch,
		ReloadDTS:          deps.ReloadDTS,
		ReloadGeofence:     deps.ReloadGeofence,
		ReloadState:        deps.ReloadState,
		EmojiReload:        deps.EmojiReload,
		EmojiOperation:     deps.EmojiOperation,
		WebhookRate:        deps.WebhookRate,
		AlertLimiter:       deps.AlertLimiter,
		DiscordRate:        deps.DiscordRate,
		TelegramRate:       deps.TelegramRate,
		GeocoderStats:      deps.GeocoderStats,
		GeocoderClear:      deps.GeocoderClear,
		Reconciler:         deps.Reconciler,
		RunReconcile:       deps.RunReconcile,
		LogBuffer:          deps.LogBuffer,
		ProcessStart:       deps.ProcessStart,
		SlashSync:          deps.SlashSync,
		SlashForceResync:   deps.SlashForceResync,
		SlashClearGlobal:   deps.SlashClearGlobal,
		SlashClearGuild:    deps.SlashClearGuild,
		SlashStatus:        deps.SlashStatus,
	}
}

// DefaultTemplate returns the template to store when the user doesn't specify one.
// Returns empty string — the renderer resolves empty to config default_template_name
// at render time, so changing the config retroactively applies to existing rules.
func (ctx *CommandContext) DefaultTemplate() string {
	return ""
}

// Permissions holds resolved permission state for the current command invocation.
type Permissions struct {
	// ChannelTracking is true if the user has delegated admin permission
	// to manage tracking in the current channel (Discord only).
	ChannelTracking bool
	// WebhookAdmin is the webhook name the user can admin (empty = none).
	WebhookAdmin string
	// UserTracking is true if the user can manage other users' tracking via user:ID.
	UserTracking bool
}

// LanguageHint is set when a language-specific command variant is used
// (from available_languages). The poracle command uses this to auto-set
// the user's language on registration.
// The help command uses this to return help in the hinted language.
func (ctx *CommandContext) SetLanguageHint(hint string) {
	ctx.languageHint = hint
}

// GetLanguageHint returns the language hint from available_languages, or "".
func (ctx *CommandContext) GetLanguageHint() string {
	return ctx.languageHint
}

// Tr returns a translator for the command's target language.
func (ctx *CommandContext) Tr() *i18n.Translator {
	return ctx.Translations.For(ctx.Language)
}

// TriggerReload signals that tracking state should be reloaded from DB.
func (ctx *CommandContext) TriggerReload() {
	if ctx.ReloadFunc != nil {
		ctx.ReloadFunc()
	}
}

// Reply represents a single response message to send back to the user.
type Reply struct {
	Text       string          `json:"text,omitempty"`
	Embed      json.RawMessage `json:"embed,omitempty"`
	React      string          `json:"react,omitempty"`
	ImageURL   string          `json:"imageUrl,omitempty"`
	Attachment *Attachment     `json:"attachment,omitempty"`
	IsDM       bool            `json:"isDM,omitempty"`
}

// SplitTextReply splits a long text into multiple Reply messages, breaking
// at line boundaries to stay within Discord's 2000 char limit.
func SplitTextReply(text string) []Reply {
	messages := SplitMessage(text, 2000)
	replies := make([]Reply, len(messages))
	for i, msg := range messages {
		replies[i] = Reply{Text: msg}
	}
	return replies
}

// Attachment is a file to attach to the reply message.
type Attachment struct {
	Filename string `json:"filename"`
	Content  []byte `json:"content"`
}

// Platform type constants used across bot commands and tracking.
const (
	TypeDiscordUser    = "discord:user"
	TypeDiscordChannel = "discord:channel"
	TypeDiscordThread  = "discord:thread"
	TypeTelegramUser   = "telegram:user"
	TypeTelegramGroup  = "telegram:group"
	// TypeTelegramTopic identifies a forum-supergroup topic. The human
	// row's ID is the composite "<chatID>:<topicID>" (e.g.
	// "-1001234567890:42") so chat and topic can be recovered without
	// a schema change.
	TypeTelegramTopic = "telegram:topic"
	TypeWebhook       = "webhook"
)

// PoracleWebhookName is the canonical name used when Poracle creates a
// Discord webhook on a managed channel. Used as both the create-time name
// and the filter when deleting Poracle-managed webhooks during channel
// reset / orphan removal — the filter must NOT touch unrelated webhooks
// the channel admin may have added.
const PoracleWebhookName = "Poracle"

// WildcardID is the sentinel value meaning "any" for pokemon_id, move, evolution, etc.
const WildcardID = 9000

// IsCommandDisabled checks if a command key (e.g. "cmd.track") matches any entry
// in the disabled_commands list (which uses short names like "track", "raid").
func IsCommandDisabled(disabled []string, cmdKey string) bool {
	if len(disabled) == 0 {
		return false
	}
	name := strings.TrimPrefix(cmdKey, "cmd.")
	return slices.Contains(disabled, name)
}
