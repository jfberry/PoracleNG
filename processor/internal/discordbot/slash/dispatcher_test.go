package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func TestNewDispatcherStoresConfig(t *testing.T) {
	cfg := Config{Enabled: true, Global: true}
	d := NewDispatcher(cfg)
	if !d.cfg.Enabled {
		t.Error("cfg.Enabled lost")
	}
}

func TestHandleCommandSkipsWhenNoCommand(t *testing.T) {
	d := NewDispatcher(Config{})
	// No registration; HandleCommand should return without panic
	d.HandleCommand(nil, nil)
}

func TestAttachStoresSessionAndDeps(t *testing.T) {
	d := NewDispatcher(Config{Enabled: true})
	s := &discordgo.Session{}
	deps := &bot.BotDeps{}
	reg := &bot.Registry{}
	bundle := &i18n.Bundle{}
	cfg := &config.Config{}

	d.Attach(s, deps, reg, bundle, cfg)

	if d.session != s {
		t.Error("session not stored")
	}
	if d.deps != deps {
		t.Error("deps not stored")
	}
	if d.registry != reg {
		t.Error("registry not stored")
	}
	if d.bundle != bundle {
		t.Error("bundle not stored")
	}
	if d.cfgRoot != cfg {
		t.Error("cfgRoot not stored")
	}
}
