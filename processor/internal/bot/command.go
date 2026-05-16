// Package bot provides the platform-agnostic command framework for Discord and
// Telegram bot commands. Commands receive structured ParsedArgs (not raw strings)
// and return Reply slices. The framework reuses existing processor packages for
// database operations, diff logic, row descriptions, game data, and translations.
package bot

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
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
