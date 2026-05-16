package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

func TestCommandPrefixDefaultDiscord(t *testing.T) {
	ctx := &CommandContext{Platform: "discord", Config: &config.Config{}}
	if got := CommandPrefix(ctx); got != "!" {
		t.Errorf("got %q, want !", got)
	}
}

func TestCommandPrefixConfiguredDiscord(t *testing.T) {
	cfg := &config.Config{}
	cfg.Discord.Prefix = "?"
	ctx := &CommandContext{Platform: "discord", Config: cfg}
	if got := CommandPrefix(ctx); got != "?" {
		t.Errorf("got %q, want ?", got)
	}
}

func TestCommandPrefixTelegramAlwaysSlash(t *testing.T) {
	cfg := &config.Config{}
	cfg.Discord.Prefix = "?" // should be ignored for telegram
	ctx := &CommandContext{Platform: "telegram", Config: cfg}
	if got := CommandPrefix(ctx); got != "/" {
		t.Errorf("got %q, want /", got)
	}
}

// Regression: /tracked and friends used to advise users with "!area", "!untrack"
// even when invoked via slash. Slash invocations must steer back to /.
func TestCommandPrefixSlashOverridesDiscord(t *testing.T) {
	cfg := &config.Config{}
	cfg.Discord.Prefix = "!"
	ctx := &CommandContext{Platform: "discord", IsSlash: true, Config: cfg}
	if got := CommandPrefix(ctx); got != "/" {
		t.Errorf("got %q, want / (IsSlash should win over discord prefix)", got)
	}
}

func TestCommandPrefixNilContext(t *testing.T) {
	if got := CommandPrefix(nil); got != "!" {
		t.Errorf("got %q, want ! for nil ctx", got)
	}
}
