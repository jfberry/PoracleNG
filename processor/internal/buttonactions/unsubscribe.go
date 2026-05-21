package buttonactions

import (
	"context"
	"errors"
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// HandleUnsubscribe is the action handler for `action = "unsubscribe"`
// with `scope = "tracking"`. Deletes every tracking-rule UID listed in
// snapshot.TrackingUIDs from the appropriate per-type tracking table.
//
// Reads snapshot.AlertType to pick the right table — the same
// information the renderer used to choose the alert template. Unknown
// alert types return an error rather than guessing; that's a real bug
// in the rendering path, not user error.
//
// Per the #109 design, unsubscribe only supports scope=tracking. Other
// scope values are rejected at DTS-load time by buttons.Def.Validate,
// so we don't bother re-checking here.
func HandleUnsubscribe(_ context.Context, snap *snapshots.Snapshot, _ buttons.Def, _ string, deps Deps) (Response, error) {
	tr := deps.Tr
	if deps.Tracking == nil {
		return Response{Text: tr.T("msg.button.actions_not_wired"), Reaction: "🙅"}, errors.New("buttonactions/unsubscribe: nil TrackingStores in deps")
	}
	if snap == nil {
		return Response{Text: tr.T("msg.button.expired"), Reaction: "🙅"}, nil
	}
	if len(snap.TrackingUIDs) == 0 {
		return Response{
			Text:     tr.Tf("msg.button.missing_target", tr.T("msg.mute.scope_tracking")),
			Reaction: "🙅",
		}, nil
	}

	deleted, err := deleteTrackingByAlertType(snap.AlertType, snap.Target, snap.TrackingUIDs, deps)
	if err != nil {
		return Response{Text: tr.T("msg.button.unsubscribe_failed"), Reaction: "🙅"}, err
	}
	if deleted == 0 {
		// Rule(s) already removed — possibly by !untrack from another
		// surface since the snapshot was written. Friendly soft-fail.
		return Response{Text: tr.T("msg.button.unsubscribe_already"), Reaction: "👌"}, nil
	}

	if deps.TriggerReload != nil {
		deps.TriggerReload()
	}

	return Response{
		Text:     tr.Tf("msg.button.unsubscribed", deleted),
		Reaction: "✅",
	}, nil
}

// deleteTrackingByAlertType dispatches the delete to the right per-type
// store based on the alert type. Returns the number of rules deleted.
func deleteTrackingByAlertType(alertType, humanID string, uids []int64, deps Deps) (int, error) {
	if deps.Tracking == nil {
		return 0, errors.New("nil Tracking")
	}
	switch alertType {
	case "monster", "monsterChanged":
		return deleteUIDsAndReport(deps.Tracking.Monsters.DeleteByUIDs, humanID, uids)
	case "raid":
		return deleteUIDsAndReport(deps.Tracking.Raids.DeleteByUIDs, humanID, uids)
	case "egg":
		return deleteUIDsAndReport(deps.Tracking.Eggs.DeleteByUIDs, humanID, uids)
	case "quest":
		return deleteUIDsAndReport(deps.Tracking.Quests.DeleteByUIDs, humanID, uids)
	case "invasion", "incident":
		return deleteUIDsAndReport(deps.Tracking.Invasions.DeleteByUIDs, humanID, uids)
	case "pokestop", "lure":
		return deleteUIDsAndReport(deps.Tracking.Lures.DeleteByUIDs, humanID, uids)
	case "nest":
		return deleteUIDsAndReport(deps.Tracking.Nests.DeleteByUIDs, humanID, uids)
	case "gym":
		return deleteUIDsAndReport(deps.Tracking.Gyms.DeleteByUIDs, humanID, uids)
	case "fort_update":
		return deleteUIDsAndReport(deps.Tracking.Forts.DeleteByUIDs, humanID, uids)
	case "max_battle", "maxbattle":
		return deleteUIDsAndReport(deps.Tracking.Maxbattles.DeleteByUIDs, humanID, uids)
	default:
		return 0, fmt.Errorf("unsubscribe: unknown alert type %q", alertType)
	}
}

// deleteUIDsAndReport calls the per-type DeleteByUIDs and reports the
// number deleted. The Tracking stores don't return a row count — they
// just attempt deletes — so for v1 we report the input list length as a
// proxy. Refinement is a follow-up if operators want exact counts.
func deleteUIDsAndReport(fn func(string, []int64) error, humanID string, uids []int64) (int, error) {
	if err := fn(humanID, uids); err != nil {
		return 0, err
	}
	return len(uids), nil
}
