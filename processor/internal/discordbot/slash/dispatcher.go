package slash

import (
	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

type Config struct {
	Enabled bool
	Global  bool
	Guilds  []string
	// Enable lists short command names this installation registers (e.g. "track").
	// Empty = nothing registered. Maps 1:1 from config's [discord.slash_commands] enable.
	Enable []string
	// Optional override paths for testing
	CachePath string
	ForceSync bool
}

type Dispatcher struct {
	cfg      Config
	session  *discordgo.Session // set by Attach()
	appID    string             // set after session.Open()
	deps     *bot.BotDeps
	registry *bot.Registry
	bundle   *i18n.Bundle
	cfgRoot  *config.Config
}

func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{cfg: cfg}
}

func (d *Dispatcher) Attach(s *discordgo.Session, deps *bot.BotDeps, registry *bot.Registry, bundle *i18n.Bundle, cfg *config.Config) {
	d.session = s
	d.deps = deps
	d.registry = registry
	d.bundle = bundle
	d.cfgRoot = cfg
}

// HandleCommand routes an ApplicationCommand interaction. No-op skeleton
// for Phase 0; subsequent tasks fill the body.
func (d *Dispatcher) HandleCommand(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if d == nil || ic == nil {
		return
	}
	// TODO: Task 11 — implement dispatch routing
}

// HandleAutocomplete routes an ApplicationCommandAutocomplete interaction.
// No-op skeleton for Phase 0.
func (d *Dispatcher) HandleAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if d == nil || ic == nil {
		return
	}
	// TODO: Task 28 — implement autocomplete routing
}
