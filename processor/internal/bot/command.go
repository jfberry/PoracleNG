// Package bot provides the platform-agnostic command framework for Discord and
// Telegram bot commands. Commands receive structured ParsedArgs (not raw strings)
// and return Reply slices. The framework reuses existing processor packages for
// database operations, diff logic, row descriptions, game data, and translations.
package bot

import (
	"encoding/json"
	"fmt"
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
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// BotDeps holds shared dependencies needed by both Discord and Telegram bots.
// Platform-specific bot Config structs embed this to avoid duplication.
type BotDeps struct {
	DB           *sqlx.DB
	Cfg          *config.Config
	StateMgr     *state.Manager
	GameData     *gamedata.GameData
	Translations *i18n.Bundle
	Dispatcher   *delivery.Dispatcher
	RowText      *rowtext.Generator
	Registry     *Registry
	Parser       *Parser
	ArgMatcher   *ArgMatcher
	Resolver     *PokemonResolver
	Geocoder     *geocoding.Geocoder
	StaticMap    *staticmap.Resolver
	Weather      *tracker.WeatherTracker
	Stats        *tracker.StatsTracker
	DTS          *dts.TemplateStore
	Emoji        *dts.EmojiLookup
	NLPParser    *nlp.Parser
	ReloadFunc   func()
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

	// Permissions (resolved before command runs)
	IsAdmin          bool
	IsCommunityAdmin bool
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

	// Injected dependencies
	DB           *sqlx.DB
	Config       *config.Config
	StateMgr     *state.Manager
	GameData     *gamedata.GameData
	Translations *i18n.Bundle
	Geofence     *geofence.SpatialIndex
	Fences       []geofence.Fence
	Dispatcher   *delivery.Dispatcher
	RowText      *rowtext.Generator
	Resolver     *PokemonResolver
	ArgMatcher   *ArgMatcher
	Geocoder     *geocoding.Geocoder
	StaticMap    *staticmap.Resolver
	Weather      *tracker.WeatherTracker
	Stats        *tracker.StatsTracker
	DTS          *dts.TemplateStore
	Emoji        *dts.EmojiLookup
	NLP          *nlp.Parser
	Registry     *Registry

	// Reload trigger — called after tracking mutations
	ReloadFunc func()
}

// DefaultTemplate returns the default template name from config as a string.
func (ctx *CommandContext) DefaultTemplate() string {
	if ctx.Config == nil {
		return "1"
	}
	switch v := ctx.Config.General.DefaultTemplateName.(type) {
	case string:
		if v != "" {
			return v
		}
	case float64:
		return fmt.Sprintf("%d", int(v))
	}
	return "1"
}

// Permissions holds resolved permission state for the current command invocation.
type Permissions struct {
	// ChannelTracking is true if the user has delegated admin permission
	// to manage tracking in the current channel (Discord only).
	ChannelTracking bool
	// WebhookAdmin is the webhook name the user can admin (empty = none).
	WebhookAdmin string
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

// Attachment is a file to attach to the reply message.
type Attachment struct {
	Filename string `json:"filename"`
	Content  []byte `json:"content"`
}

// Platform type constants used across bot commands and tracking.
const (
	TypeDiscordUser    = "discord:user"
	TypeDiscordChannel = "discord:channel"
	TypeTelegramUser   = "telegram:user"
	TypeTelegramGroup  = "telegram:group"
	TypeWebhook        = "webhook"
)

// WildcardID is the sentinel value meaning "any" for pokemon_id, move, evolution, etc.
const WildcardID = 9000

// IsCommandDisabled checks if a command key (e.g. "cmd.track") matches any entry
// in the disabled_commands list (which uses short names like "track", "raid").
func IsCommandDisabled(disabled []string, cmdKey string) bool {
	if len(disabled) == 0 {
		return false
	}
	name := strings.TrimPrefix(cmdKey, "cmd.")
	for _, d := range disabled {
		if d == name {
			return true
		}
	}
	return false
}
