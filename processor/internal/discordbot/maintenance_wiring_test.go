package discordbot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// TestMaintenanceWiring_EndToEnd verifies that pausing the same Dispatcher
// instance that's wired into Bot.BotDeps causes the suffix to fire on the
// next reply. Catches regressions where the dispatcher reference gets
// copied/replaced/lost between Bot construction and Pause/IsPaused.
func TestMaintenanceWiring_EndToEnd(t *testing.T) {
	disp, err := delivery.NewDispatcher(delivery.DispatcherConfig{})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	defer disp.Stop()

	// Simulate what main.go does: put dispatcher into BotDeps, then into a Bot.
	deps := bot.BotDeps{Dispatcher: disp}
	b := &Bot{BotDeps: deps}

	// Sanity: same instance reachable via embed.
	if b.Dispatcher != disp {
		t.Fatalf("Bot.Dispatcher pointer (%p) differs from disp (%p)", b.Dispatcher, disp)
	}

	// Initially not paused.
	if b.Dispatcher.IsPaused() {
		t.Fatal("expected fresh dispatcher to be unpaused")
	}

	// A maintenance command would do this via ctx.Dispatcher (= deps.Dispatcher = same instance).
	deps.Dispatcher.Pause("test")

	// IsPaused must now return true via the Bot's view.
	if !b.Dispatcher.IsPaused() {
		t.Fatal("after Pause, b.Dispatcher.IsPaused() = false — wiring is broken")
	}

	// Suffix should apply on the next reply.
	replies := []bot.Reply{{Text: "tracked: pikachu"}}
	got := bot.ApplyMaintenanceSuffix(replies, b.Dispatcher, "🔧 SUFFIX")
	if len(got) != 1 || got[0].Text != "tracked: pikachu\n🔧 SUFFIX" {
		t.Errorf("suffix not applied via b.Dispatcher: got %+v", got)
	}
}
