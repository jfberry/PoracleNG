package buttonactions

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// HandleRedeliver renders the snapshot's original template into the
// clicker's DM. Uses snapshot.TemplateSelected to pick the exact entry
// the user originally saw; falls back to TemplateRequested + the
// selection chain when the resolved entry is no longer present.
//
// Re-renders against the stored Snapshot.View — the resolved enrichment
// the user saw at first send. Any post-fire data drift (RSVP counts,
// etc.) isn't reflected; if operators want a "refresh" semantics, they
// should configure a `render` button against current state rather than
// `redeliver`.
//
// Requires deps.RenderToView + deps.DispatcherDM hooks — neither of
// which exist in v1's Deps shape. Stubbed to return a clear "needs
// host wiring" message until the host plumbs them in. The infrastructure
// is in place (snapshot has TemplateSelected + View; the dispatcher
// has a DM path) — only the host glue is missing, kept out of v1 to
// avoid expanding the buttonactions surface before the wire is needed.
func HandleRedeliver(_ context.Context, snap *snapshots.Snapshot, _ buttons.Def, clicker string, deps Deps) (Response, error) {
	tr := deps.Tr
	if snap == nil {
		return Response{Text: tr.T("msg.button.expired"), Reaction: "🙅"}, nil
	}
	if deps.RenderToDM == nil {
		return Response{
			Text:     tr.T("msg.button.responses_not_wired"),
			Reaction: "🙅",
		}, errors.New("buttonactions/redeliver: nil RenderToDM in deps")
	}
	if clicker == "" {
		return Response{Text: tr.T("msg.button.no_dm_target"), Reaction: "🙅"}, nil
	}

	if err := deps.RenderToDM(snap, clicker); err != nil {
		return Response{
			Text:     tr.Tf("msg.button.dm_failed", err.Error()),
			Reaction: "🙅",
		}, err
	}
	return Response{
		Text:     tr.T("msg.button.redeliver_sent"),
		Reaction: "📬",
	}, nil
}

// RenderToDMFunc is the host-provided function that re-renders a
// snapshot's template into the clicker's DM. Used by HandleRedeliver.
// The host implementation lives in cmd/processor and reaches into the
// DTS renderer + delivery dispatcher; isolating the dependency behind
// this function pointer keeps the buttonactions package decoupled.
//
// Returns an error when render fails or the clicker can't be DM'd. The
// handler surfaces the error string in the ephemeral reply.
type RenderToDMFunc func(snap *snapshots.Snapshot, clickerUserID string) error

// (delivery is imported so the redeliver wire-up — when added — can
// reference delivery.Job types without an extra import cycle dance.)
var _ = delivery.Job{}
var _ = json.RawMessage(nil)
