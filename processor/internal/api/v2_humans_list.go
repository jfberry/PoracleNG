package api

import (
	"context"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2HumanSummary is the per-row shape of the humans listing: enough to render
// an admin list or resolve a display name, without the geofence areas,
// coordinates and tracking counts of the full resource. Fetch
// GET /v2/humans/{id} for those.
type v2HumanSummary struct {
	ID               string `json:"id" doc:"Human id (Discord/Telegram id, or webhook name)"`
	Type             string `json:"type" doc:"Registration type, e.g. discord:user, discord:channel, webhook, telegram:user"`
	Name             string `json:"name" doc:"Display name"`
	Enabled          bool   `json:"enabled" doc:"Whether the user has alerts switched on (the !start/!stop flag)"`
	AdminDisable     bool   `json:"admin_disable" doc:"Whether an admin has disabled this account; such a user must re-register"`
	Language         string `json:"language" doc:"Alert language code, empty when the user has not chosen one"`
	CurrentProfileNo int    `json:"current_profile_no" doc:"The profile currently active for this human"`
}

type v2HumansListInput struct {
	Type string `query:"type" doc:"Filter to one registration type, e.g. webhook or discord:user. Omit for every type."`
	IDs  string `query:"id" doc:"Comma-separated ids to fetch, e.g. id=123,456 — the same form as the bulk tracking delete's uid=. Unknown ids are omitted rather than erroring. Omit to list everything."`
}

type v2HumansListOutput struct {
	Body struct {
		Humans []v2HumanSummary `json:"humans" doc:"Matching humans, ascending by id"`
	}
}

// registerV2HumansList registers GET /v2/humans.
//
// Neither API version could answer "who is registered here", so a client
// wanting an admin user list had to keep a repository over the Poracle
// database. ?id=a,b,c exists because the alternative for a page resolving many
// display names is one request per person (#214).
func registerV2HumansList(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-humans", Method: "GET", Path: "/v2/humans",
		Summary: "List registered humans",
		Description: "Lists humans on this instance, ascending by id. `?type=` narrows to one registration type; " +
			"`?id=a,b,c` fetches a specific set in one request (unknown ids are omitted, not an error). " +
			"Combining them intersects. Returns a summary per human — use GET /v2/humans/{id} for the full resource.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumansListInput) (*v2HumansListOutput, error) {
		var humans []*store.Human
		var err error
		if in.Type != "" {
			humans, err = deps.Humans.ListByType(in.Type)
		} else {
			humans, err = deps.Humans.ListAll()
		}
		if err != nil {
			log.Errorf("v2 humans: list: %s", err)
			return nil, huma.Error500InternalServerError("database error")
		}

		var wanted map[string]bool
		if in.IDs != "" {
			wanted = make(map[string]bool)
			for _, id := range strings.Split(in.IDs, ",") {
				if id = strings.TrimSpace(id); id != "" {
					wanted[id] = true
				}
			}
		}

		out := &v2HumansListOutput{}
		out.Body.Humans = []v2HumanSummary{}
		for _, h := range humans {
			if h == nil || (wanted != nil && !wanted[h.ID]) {
				continue
			}
			out.Body.Humans = append(out.Body.Humans, v2HumanSummary{
				ID:               h.ID,
				Type:             h.Type,
				Name:             h.Name,
				Enabled:          h.Enabled,
				AdminDisable:     h.AdminDisable,
				Language:         h.Language,
				CurrentProfileNo: h.CurrentProfileNo,
			})
		}
		// Store order is unspecified (the mock and SQL differ); sort so
		// responses are cacheable and diffable.
		sort.Slice(out.Body.Humans, func(i, j int) bool {
			return out.Body.Humans[i].ID < out.Body.Humans[j].ID
		})
		return out, nil
	})
}

// registerV2HumanDelete registers DELETE /v2/humans/{id}.
//
// store.Delete cascades every tracking table, profiles and summary_schedules.
// Doing this client-side meant reimplementing that cascade against PoracleNG's
// schema, which goes stale silently whenever a table is added (#214).
func registerV2HumanDelete(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-delete-human", Method: "DELETE", Path: "/v2/humans/{id}",
		Summary: "Delete a human and all their data",
		Description: "Removes the human along with every tracking rule, profile and summary schedule they own. " +
			"Irreversible. 404 if the human does not exist. Triggers a state reload.",
		Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*statusOKOutput, error) {
		if _, err := resolveFullHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Humans.Delete(in.ID); err != nil {
			log.Errorf("v2 humans: delete %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		reloadState(deps)
		return okStatus(), nil
	})
}
