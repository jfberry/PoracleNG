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
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// Deps bundles the dependencies action handlers reach for. Set once at
// startup by main.go; passed to Dispatch for every click. Handlers must
// nil-check the fields they use — tests may pass a partial bag.
type Deps struct {
	MuteStore *mute.Store

	// Tracking is the per-type CRUD aggregator. The unsubscribe action
	// uses it to delete rows by UID. Nil disables unsubscribe — the
	// handler returns "feature unavailable" rather than panicking.
	Tracking *store.TrackingStores

	// TriggerReload is called after destructive mutations (unsubscribe)
	// so the in-memory state picks up the change immediately. Wired to
	// ProcessorService.triggerReload in main.go; nil disables reload
	// triggering (tests, etc.).
	TriggerReload func()

	// RenderToDM is the host hook the redeliver action calls to re-send
	// a snapshot's template into the clicker's DM. nil disables the
	// redeliver action (it returns a clear "not wired" message).
	RenderToDM RenderToDMFunc

	// ResponseRender renders a button's response template/text against
	// the snapshot view and returns the rendered ephemeral payload.
	// nil disables response-template buttons (the click returns "not
	// wired").
	ResponseRender ResponseRenderFunc

	// Tr is the translator for the clicker's language. Set by the host
	// (discordbot) from snap.Language before each dispatch. Translator
	// is nil-safe — handlers can call deps.Tr.Tf(...) directly; a nil
	// receiver returns the key verbatim (acceptable in tests that
	// don't assert on response text).
	Tr *i18n.Translator
}

// ResponseRenderFunc is the host-provided render function for button
// response messages. Takes the button definition (which carries the
// response_template_id / response_template_inline / response_text) and
// the snapshot, and returns the rendered ephemeral payload to send back
// to the clicker.
type ResponseRenderFunc func(snap *snapshots.Snapshot, def buttons.Def) (string, error)

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

// Names returns the list of registered action names, sorted for stable
// output. Used by the /api/dts/actions endpoint so config editors can
// surface a dropdown of available actions to operators.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny inline sort to keep the package import surface
// minimal (sort would be the only stdlib import added otherwise).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
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
//   - "unsubscribe" → unsubscribe handler
//   - "redeliver"   → redeliver handler
//   - "render"      → render handler
func RegisterBuiltins(r *Registry) {
	r.Register(buttons.ActionMute, HandleMute)
	r.Register(buttons.ActionUnsubscribe, HandleUnsubscribe)
	r.Register(buttons.ActionRedeliver, HandleRedeliver)
	r.Register(buttons.ActionRender, HandleRender)
}
