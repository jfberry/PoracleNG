// Package buttons defines the operator-authored vocabulary for interactive
// buttons attached to DTS alert templates (#109). A ButtonDef is the
// declarative form an operator writes in DTS; the render path consumes it
// to emit Discord components, the InteractionCreate handler resolves a
// click back to one of these, and the action registry dispatches the
// chosen handler against the per-delivery Snapshot stored at send time.
//
// Keeping the types in their own leaf package avoids import cycles
// between internal/dts (which loads them), internal/buttonactions (which
// dispatches them), and internal/discordbot (which wires the gateway
// handler). All four packages share the same vocabulary.
package buttons

import (
	"errors"
	"fmt"
	"strings"
)

// Discord Button Styles. Match the discordgo constants for direct use in
// the render layer without dragging that dependency into the buttons
// package itself. The operator writes the lowercase string; the
// component emitter maps to the discord int code.
const (
	StylePrimary   = "primary"
	StyleSecondary = "secondary"
	StyleSuccess   = "success"
	StyleDanger    = "danger"
)

// Action names. The action handler registry looks up by these strings.
const (
	ActionMute        = "mute"
	ActionUnsubscribe = "unsubscribe"
	ActionRedeliver   = "redeliver"
	ActionRender      = "render"
)

// Scope values — see internal/mute for the runtime semantics. We use the
// same strings to avoid translation between the command vocabulary, the
// button-config vocabulary, and the in-memory store.
const (
	ScopeGym        = "gym"
	ScopePokemon    = "pokemon"
	ScopeArea       = "area"
	ScopePokestop   = "pokestop"
	ScopeStation    = "station"
	ScopeEverything = "everything"
	ScopeTracking   = "tracking" // Phase 2.5 — accepted by validator but no-op at runtime
)

// AppliesTo values — controls which Snapshot.TargetType receives the button.
const (
	AppliesToDM      = "dm"
	AppliesToChannel = "channel"
	AppliesToWebhook = "webhook"
	AppliesToAny     = "any"
)

// VisibleTo values — controls click-time access.
const (
	VisibleTarget     = "target"
	VisibleAdmin      = "admin"
	VisibleRegistered = "registered"
	VisibleAnyone     = "anyone"
)

// Def is one operator-authored button. The renderer reads the
// pre-render fields (Label, Style, ShowIf, AppliesTo) plus the
// dispatch fields (Action / ResponseTemplateID / ResponseTemplateInline
// / ResponseText) to decide whether to attach this button to the
// outgoing message and, if so, with what custom_id payload.
//
// Exactly one of {Action, ResponseTemplateID, ResponseTemplateInline,
// ResponseText} must be non-empty per the design. Validate() enforces
// that invariant.
type Def struct {
	// Identity used in the click custom_id.
	ID string `json:"id"`

	// Visual properties — what Discord shows.
	Label string `json:"label"`
	Style string `json:"style,omitempty"` // default StyleSecondary when empty

	// Dispatch: pick exactly one.
	Action                 string `json:"action,omitempty"`
	ResponseTemplateID     string `json:"response_template_id,omitempty"`
	ResponseTemplateInline any    `json:"response_template_inline,omitempty"`
	ResponseText           string `json:"response_text,omitempty"`

	// Scope (action buttons only): names which Snapshot field the action
	// handler reads to identify the target. Required when Action is
	// "mute" or "unsubscribe"; unused by render-style buttons and the
	// redeliver action.
	Scope string `json:"scope,omitempty"`

	// Params is a free-form bag passed verbatim to the action handler.
	// Mute uses duration_min; render uses template_id. The DTS schema
	// stays action-agnostic — new actions can read whatever they need
	// without schema changes.
	Params map[string]any `json:"params,omitempty"`

	// AppliesTo restricts the destination types this button attaches to.
	// Empty slice defers to the action-level default (mute/unsubscribe
	// default to ["dm"]; response-style and redeliver/render default to
	// ["any"]).
	AppliesTo []string `json:"applies_to,omitempty"`

	// ShowIf is a Handlebars expression evaluated at render time against
	// the resolved view. Empty means always attach. Falsy result hides
	// the button entirely (no click-time "doesn't apply" message).
	ShowIf string `json:"show_if,omitempty"`

	// VisibleTo restricts who may click. Enforced server-side at click;
	// Discord doesn't support per-user button visibility so the button
	// is always physically present.
	VisibleTo string `json:"visible_to,omitempty"` // default VisibleTarget when empty
}

// Mode reports which dispatch field the button uses. Used by the render
// path and click handler to switch on shape without re-parsing the bag.
type Mode int

const (
	ModeInvalid Mode = iota
	ModeAction
	ModeResponseTemplateID
	ModeResponseTemplateInline
	ModeResponseText
)

// Mode returns the dispatch mode for this button. Returns ModeInvalid
// when the button doesn't pass Validate; callers should call Validate at
// load time so this never returns ModeInvalid at runtime.
func (d *Def) DispatchMode() Mode {
	switch {
	case d.Action != "":
		return ModeAction
	case d.ResponseTemplateID != "":
		return ModeResponseTemplateID
	case d.ResponseTemplateInline != nil:
		return ModeResponseTemplateInline
	case d.ResponseText != "":
		return ModeResponseText
	default:
		return ModeInvalid
	}
}

// EffectiveStyle returns the Discord button style as a lowercase string,
// substituting the default (StyleSecondary) when the operator omits one.
func (d *Def) EffectiveStyle() string {
	if d.Style == "" {
		return StyleSecondary
	}
	return d.Style
}

// EffectiveVisibility returns the visibility scope, substituting the
// default (VisibleTarget) when the operator omits it.
func (d *Def) EffectiveVisibility() string {
	if d.VisibleTo == "" {
		return VisibleTarget
	}
	return d.VisibleTo
}

// EffectiveAppliesTo returns the destination types this button is
// attached to, applying action-level defaults when AppliesTo is empty:
//
//   - mute / unsubscribe → ["dm"]
//   - everything else    → ["any"]
//
// Callers compare the result against Snapshot.TargetType ("dm",
// "channel", "webhook"). The "any" pseudo-value matches every type.
func (d *Def) EffectiveAppliesTo() []string {
	if len(d.AppliesTo) > 0 {
		return d.AppliesTo
	}
	if d.Action == ActionMute || d.Action == ActionUnsubscribe {
		return []string{AppliesToDM}
	}
	return []string{AppliesToAny}
}

// AppliesToTarget reports whether the button should attach to a message
// going to a destination of type targetType ("dm" / "channel" /
// "webhook"). The "any" pseudo-value in AppliesTo matches every type.
func (d *Def) AppliesToTarget(targetType string) bool {
	for _, t := range d.EffectiveAppliesTo() {
		if t == AppliesToAny || t == targetType {
			return true
		}
	}
	return false
}

// Validation errors. Returned by Validate; the loader logs them with
// context and skips the offending button (rest of the entry stays valid).
var (
	ErrMissingID         = errors.New("button: id is required")
	ErrMissingLabel      = errors.New("button: label is required")
	ErrAmbiguousDispatch = errors.New("button: declares more than one of action/response_template_id/response_template_inline/response_text — pick one")
	ErrNoDispatch        = errors.New("button: declares none of action/response_template_id/response_template_inline/response_text — pick one")
	ErrUnknownAction     = errors.New("button: unknown action — must be one of mute, unsubscribe, redeliver, render")
	ErrUnknownScope      = errors.New("button: unknown scope — must be one of gym, pokemon, area, pokestop, station, everything, tracking")
	ErrUnknownAppliesTo  = errors.New("button: unknown applies_to value — must be one of dm, channel, webhook, any")
	ErrUnknownVisibleTo  = errors.New("button: unknown visible_to value — must be one of target, admin, registered, anyone")
	ErrUnknownStyle      = errors.New("button: unknown style — must be one of primary, secondary, success, danger")
	ErrScopeRequired     = errors.New("button: scope is required for this action")
	ErrUnsubscribeScope  = errors.New("button: unsubscribe action only supports scope=tracking")
)

// Validate checks the button's invariants at DTS load time. Each error
// is returned individually so the loader can log a specific reason —
// not joined or wrapped to keep the surface stable for tests.
func (d *Def) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return ErrMissingID
	}
	if strings.TrimSpace(d.Label) == "" {
		return ErrMissingLabel
	}

	if d.Style != "" {
		switch d.Style {
		case StylePrimary, StyleSecondary, StyleSuccess, StyleDanger:
		default:
			return fmt.Errorf("%w: %q", ErrUnknownStyle, d.Style)
		}
	}

	// Exactly one dispatch field.
	var dispatchCount int
	if d.Action != "" {
		dispatchCount++
	}
	if d.ResponseTemplateID != "" {
		dispatchCount++
	}
	if d.ResponseTemplateInline != nil {
		dispatchCount++
	}
	if d.ResponseText != "" {
		dispatchCount++
	}
	switch dispatchCount {
	case 0:
		return ErrNoDispatch
	case 1: // ok
	default:
		return ErrAmbiguousDispatch
	}

	if d.Action != "" {
		switch d.Action {
		case ActionMute, ActionUnsubscribe, ActionRedeliver, ActionRender:
		default:
			return fmt.Errorf("%w: %q", ErrUnknownAction, d.Action)
		}
	}

	if d.Scope != "" {
		switch d.Scope {
		case ScopeGym, ScopePokemon, ScopeArea,
			ScopePokestop, ScopeStation, ScopeEverything, ScopeTracking:
		default:
			return fmt.Errorf("%w: %q", ErrUnknownScope, d.Scope)
		}
	}

	// mute / unsubscribe need an explicit scope.
	switch d.Action {
	case ActionMute:
		if d.Scope == "" {
			return ErrScopeRequired
		}
	case ActionUnsubscribe:
		if d.Scope == "" {
			return ErrScopeRequired
		}
		if d.Scope != ScopeTracking {
			return ErrUnsubscribeScope
		}
	}

	for _, t := range d.AppliesTo {
		switch t {
		case AppliesToDM, AppliesToChannel, AppliesToWebhook, AppliesToAny:
		default:
			return fmt.Errorf("%w: %q", ErrUnknownAppliesTo, t)
		}
	}

	if d.VisibleTo != "" {
		switch d.VisibleTo {
		case VisibleTarget, VisibleAdmin, VisibleRegistered, VisibleAnyone:
		default:
			return fmt.Errorf("%w: %q", ErrUnknownVisibleTo, d.VisibleTo)
		}
	}

	return nil
}
