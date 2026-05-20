// Package buttonactions is the dispatch registry for button-triggered
// state changes. Click handlers in internal/discordbot resolve a Discord
// component click to a (Snapshot, button.Def) pair and call Dispatch;
// the registered Handler then writes to the in-memory mute store /
// tracking store / etc. and returns the ephemeral confirmation to send
// back to the clicker.
//
// Separated from internal/buttons (vocabulary types) so the dispatch
// layer can depend on snapshots + mute without dragging those into
// internal/dts (which loads button defs but doesn't dispatch them).
package buttonactions

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// Deps bundles the dependencies action handlers reach for. Set once at
// startup by main.go; passed to Dispatch for every click. Handlers must
// nil-check the fields they use — tests may pass a partial bag.
type Deps struct {
	MuteStore *mute.Store
}

// Response is the ephemeral message returned to the clicker after a
// successful dispatch. Text is what Discord renders in the ephemeral
// reply. Reaction is a short emoji shown alongside the text (operator
// convention: ✅ for success, 🔇 for mute, 🙅 for refusal).
//
// Mirrors the bot.Reply shape so the discordbot layer can stamp these
// straight into an interaction response.
type Response struct {
	Text     string
	Reaction string
}

// Handler is the signature every action implementation satisfies.
// snap is the loaded Snapshot for the message the user clicked; def is
// the button definition the click resolved to; clickerUserID is the
// platform user ID (Discord snowflake or Telegram user ID) of whoever
// clicked.
//
// Handlers run synchronously inside the InteractionCreate goroutine.
// Discord allows 3 seconds to respond before the interaction expires;
// keep work below that ceiling or return early with a "queued" message
// and finish async.
type Handler func(ctx context.Context, snap *snapshots.Snapshot, def buttons.Def, clickerUserID string, deps Deps) (Response, error)

// Registry routes a button's Action string to its registered Handler.
// Look-ups are read-locked; Register is the only writer and runs at
// startup. Built-ins are wired by RegisterBuiltins.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns an empty registry. Callers typically wire
// built-ins immediately via RegisterBuiltins, then register any
// host-specific actions on top.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register attaches fn under name. Replaces any existing handler — the
// last registration wins. Panics on a nil handler; that's an obvious
// programming error and silently ignoring it would mask real bugs.
func (r *Registry) Register(name string, fn Handler) {
	if fn == nil {
		panic("buttonactions: Register called with nil handler for " + name)
	}
	r.mu.Lock()
	r.handlers[name] = fn
	r.mu.Unlock()
}

// Lookup returns the handler for name, or nil when unregistered.
// Click dispatch checks for nil and returns ErrUnknownAction.
func (r *Registry) Lookup(name string) Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[name]
}

// Dispatch resolves and invokes the handler for def.Action. Returns
// ErrUnknownAction when no handler is registered; otherwise propagates
// whatever the handler returns.
func (r *Registry) Dispatch(ctx context.Context, snap *snapshots.Snapshot, def buttons.Def, clickerUserID string, deps Deps) (Response, error) {
	if def.Action == "" {
		return Response{}, ErrNoAction
	}
	h := r.Lookup(def.Action)
	if h == nil {
		return Response{}, fmt.Errorf("%w: %q", ErrUnknownAction, def.Action)
	}
	return h(ctx, snap, def, clickerUserID, deps)
}

// Errors surfaced to the click handler. The discordbot layer maps these
// to the canonical user-facing strings (msg.button.action_failed etc.).
var (
	ErrNoAction       = errors.New("buttonactions: button has no action")
	ErrUnknownAction  = errors.New("buttonactions: unknown action")
	ErrNotImplemented = errors.New("buttonactions: handler not yet implemented")
)

// RegisterBuiltins wires the v1 action handlers into a registry:
//   - "mute"        → mute store write
//   - "unsubscribe" → not yet (returns ErrNotImplemented)
//   - "redeliver"   → not yet
//   - "render"      → not yet
//
// The non-implemented stubs are registered (rather than left absent) so
// operators get a clear "not yet implemented" message instead of the
// generic "unknown action" — important during the rollout where the DTS
// vocabulary is published but some actions trail their handlers.
func RegisterBuiltins(r *Registry) {
	r.Register(buttons.ActionMute, HandleMute)
	r.Register(buttons.ActionUnsubscribe, notImplemented(buttons.ActionUnsubscribe))
	r.Register(buttons.ActionRedeliver, notImplemented(buttons.ActionRedeliver))
	r.Register(buttons.ActionRender, notImplemented(buttons.ActionRender))
}

func notImplemented(name string) Handler {
	return func(_ context.Context, _ *snapshots.Snapshot, _ buttons.Def, _ string, _ Deps) (Response, error) {
		return Response{
			Text:     fmt.Sprintf("The %q action isn't implemented yet — wired in a follow-up.", name),
			Reaction: "🙅",
		}, nil
	}
}
