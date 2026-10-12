package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_snapshot.go is the full tracking-snapshot endpoint:
//
//	GET /api/v2/humans/{id}/tracking
//
// It returns the human + all-type tracking rules + profiles + locations +
// summaries in one document, replacing v1's all/{id} and allProfiles/{id}.
//
// The "tracking" map is assembled from the per-type snapshot providers
// registered by registerV2Tracking, so each type's rules are byte-identical to
// that type's GET .../tracking/<type> list (same scoping, same envelope, same
// optional ?include_descriptions). invasion and incident split via their Filter
// discriminator (an incident row lands under "incident", an invasion under
// "invasion"). RegisterV2TrackingSnapshot MUST be called AFTER all per-type
// RegisterV2TrackingX calls so the provider registry is fully populated.

// v2SnapshotInput binds the path id and the three strict query params. Unknown
// query params are rejected (RejectUnknownQueryParameters on the op).
type v2SnapshotInput struct {
	ID                  string `path:"id" doc:"Human id (the owning user); ownership scope. 404 if it does not exist."`
	Profile             int    `query:"profile" default:"-1" doc:"Profile number; defaults to the human's active profile. Ignored when all_profiles=true."`
	AllProfiles         bool   `query:"all_profiles" doc:"Span every profile the human owns; each rule then carries its profile_no."`
	IncludeDescriptions bool   `query:"include_descriptions" doc:"Add a human-readable description to every rule."`
}

// v2SnapshotBody is the response document. tracking is keyed by tracking-type
// name; each value is the same strict rule envelopes the per-type list returns
// (raw JSON so the flattened-envelope marshalling is preserved verbatim). The
// human, profiles, locations and summaries use the same v2 shapes as their own
// v2 endpoints.
type v2SnapshotBody struct {
	Human     *v2Human                     `json:"human"`
	Tracking  map[string][]json.RawMessage `json:"tracking"`
	Profiles  []v2ProfileResponse          `json:"profiles"`
	Locations v2Locations                  `json:"locations"`
	Summaries []v2SummarySchedule          `json:"summaries"`
	Mutes     []v2MuteItem                 `json:"mutes" doc:"Active (non-expired) alert mutes — same items as GET …/mutes. Mutes are held in memory only and are cleared by a processor restart."`
	Accounts  []v2AccountItem              `json:"accounts" doc:"Linked game accounts — same items as GET …/accounts."`
}

type v2SnapshotOutput struct {
	Body v2SnapshotBody
}

// RegisterV2TrackingSnapshot registers GET /v2/humans/{id}/tracking. It must run
// after every per-type RegisterV2TrackingX so deps.v2SnapshotProviders is full.
func RegisterV2TrackingSnapshot(api huma.API, deps *TrackingDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-tracking-snapshot",
		Method:      "GET",
		Path:        "/v2/humans/{id}/tracking",
		Summary:     "Full tracking snapshot for a human",
		Description: "Returns the human, all-type tracking rules, profiles, locations and summaries in one document. " +
			"?profile selects one profile (default: active); ?all_profiles=true spans every profile (rules carry profile_no); " +
			"?include_descriptions=true adds a description to every rule. Replaces v1 all/{id} and allProfiles/{id}.",
		Tags:                         []string{"v2-tracking"},
		Security:                     []map[string][]string{{"poracleSecret": {}}},
		RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2SnapshotInput) (*v2SnapshotOutput, error) {
		human, profileNo, err := resolveHuman(deps, in.ID, in.Profile)
		if err != nil {
			return nil, err
		}

		// Full human record (lat/lon/area) for the "human" sub-object, mirroring
		// v1 all/{id}. A nil record here means the lite lookup raced a delete;
		// resolveHuman already 404s the genuinely-missing case.
		humanFull, ferr := deps.Humans.Get(human.ID)
		if ferr != nil {
			log.Errorf("v2 snapshot: get full human %s: %s", human.ID, ferr)
			return nil, huma.Error500InternalServerError("database error")
		}

		// Profiles — needed both for the response and to enumerate all_profiles.
		profiles, perr := deps.Humans.GetProfiles(human.ID)
		if perr != nil {
			log.Errorf("v2 snapshot: get profiles %s: %s", human.ID, perr)
			return nil, huma.Error500InternalServerError("database error")
		}

		tracking := make(map[string][]json.RawMessage, len(deps.v2SnapshotProviders))
		for _, p := range deps.v2SnapshotProviders {
			rules, rerr := p.Rules(human, profiles, profileNo, in.AllProfiles, in.IncludeDescriptions)
			if rerr != nil {
				log.Errorf("v2 snapshot: %s rules for %s: %s", p.TypeName, human.ID, rerr)
				return nil, huma.Error500InternalServerError("database error")
			}
			tracking[p.TypeName] = rules
		}

		out := &v2SnapshotOutput{}
		out.Body.Human = humanToV2(humanFull)
		out.Body.Tracking = tracking
		out.Body.Profiles = make([]v2ProfileResponse, len(profiles))
		for i, p := range profiles {
			out.Body.Profiles[i] = profileToV2Response(p)
		}
		out.Body.Locations = snapshotLocations(deps, human.ID, humanFull)
		out.Body.Summaries = snapshotSummaries(deps, human.ID)
		out.Body.Mutes = snapshotMutes(deps, human.ID)
		out.Body.Accounts = snapshotAccounts(deps, human.ID)
		return out, nil
	})
}

// snapshotLocations builds the {default, named} locations payload, mirroring
// HandleListLocations. humanFull supplies the default lat/lon.
func snapshotLocations(deps *TrackingDeps, id string, humanFull *store.Human) v2Locations {
	out := v2Locations{Named: []v2LocationItem{}}
	locs, err := deps.Humans.ListLocations(id)
	if err != nil {
		log.Warnf("v2 snapshot: list locations %s: %s", id, err)
	} else {
		for _, l := range locs {
			out.Named = append(out.Named, v2LocationItem{
				Label:     l.Label,
				Latitude:  l.Latitude,
				Longitude: l.Longitude,
			})
		}
	}
	if humanFull != nil && (humanFull.Latitude != 0 || humanFull.Longitude != 0) {
		out.Default = &v2DefaultLocation{
			Latitude:  humanFull.Latitude,
			Longitude: humanFull.Longitude,
		}
	}
	return out
}

// snapshotAccounts lists the human's linked accounts; nil store => empty.
func snapshotAccounts(deps *TrackingDeps, id string) []v2AccountItem {
	out := make([]v2AccountItem, 0)
	if deps.Accounts == nil {
		return out
	}
	list, err := deps.Accounts.List(id)
	if err != nil {
		log.Errorf("v2 snapshot: accounts %s: %s", id, err)
		return out
	}
	for _, a := range list {
		out = append(out, accountToItem(a))
	}
	return out
}

// snapshotMutes returns the human's active mutes via the same conversion the
// mutes list endpoint uses. nil store ⇒ empty (tests and minimal harnesses).
func snapshotMutes(deps *TrackingDeps, id string) []v2MuteItem {
	out := []v2MuteItem{}
	if deps.Mutes == nil {
		return out
	}
	now := time.Now().Unix()
	for _, e := range deps.Mutes.ListActive(id, now) {
		out = append(out, muteEntryToItem(e, now))
	}
	return out
}

// v2SummarySchedule is one summary schedule in the snapshot. Its active_hours
// uses the same typed entries (ISO weekday 1=Monday … 7=Sunday) as profile
// schedules.
type v2SummarySchedule struct {
	AlertType   string              `json:"alert_type" doc:"Alert type the schedule applies to (currently quest)."`
	ActiveHours []v2ActiveHourEntry `json:"active_hours" doc:"When the buffered summary is sent. [] when unscheduled."`
}

// snapshotSummaries returns the human's summary schedules across the known
// summary alert types, mirroring HandleSummaryListForUser. nil store ⇒ empty.
func snapshotSummaries(deps *TrackingDeps, id string) []v2SummarySchedule {
	out := []v2SummarySchedule{}
	if deps.Summaries == nil {
		return out
	}
	for _, alertType := range knownSummaryAlertTypes {
		s, err := deps.Summaries.Get(id, alertType)
		if err != nil {
			log.Warnf("v2 snapshot: get summary %s/%s: %s", id, alertType, err)
			continue
		}
		if s == nil {
			continue
		}
		out = append(out, v2SummarySchedule{
			AlertType:   s.AlertType,
			ActiveHours: parseV2ActiveHours(s.ActiveHours),
		})
	}
	return out
}
