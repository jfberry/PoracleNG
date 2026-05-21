package buttonactions

import (
	"context"
	"errors"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// HandleRender is the action handler for `action = "render"` and also
// the dispatch target for response-template buttons (response_template_id
// / response_template_inline / response_text). All three flow through
// the host's ResponseRender hook, which knows how to compile the
// requested template against snapshot.View and produce an ephemeral
// payload.
//
// The hook lives in the host (cmd/processor) because it needs the DTS
// renderer + selection chain — both of which would create import cycles
// if dragged into this package.
func HandleRender(_ context.Context, snap *snapshots.Snapshot, def buttons.Def, _ string, deps Deps) (Response, error) {
	tr := deps.Tr
	if snap == nil {
		return Response{Text: tr.T("msg.button.expired"), Reaction: "🙅"}, nil
	}
	if deps.ResponseRender == nil {
		return Response{
			Text:     tr.T("msg.button.responses_not_wired"),
			Reaction: "🙅",
		}, errors.New("buttonactions/render: nil ResponseRender in deps")
	}
	out, err := deps.ResponseRender(snap, def)
	if err != nil {
		return Response{Text: tr.Tf("msg.button.response_failed", err.Error()), Reaction: "🙅"}, err
	}
	if out == "" {
		return Response{Text: tr.T("msg.button.done"), Reaction: "✅"}, nil
	}
	return Response{Text: out, Reaction: "✅"}, nil
}
