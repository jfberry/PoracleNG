package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_mutes.go is the strict /api/v2 surface over the in-memory mute store
// (internal/mute, #109). Design: docs/superpowers/specs/2026-06-11-mute-api-design.md.
//
// Mutes are deliberately volatile — lost on processor restart — and this API
// keeps those semantics; the wire descriptions say so. A mute has no uid: its
// identity is (scope, value) per human, so item deletion addresses entries via
// ?scope=&value= query params (area names and fort ids are hostile to path
// segments). DELETE returns the removed entries ({deleted:[…]}) mirroring v2
// tracking's delete shape; POST returns the canonical entry plus a replaced
// flag (re-muting the same scope+value extends expiry, like Store.Add and the
// bot's muted/re-muted distinction).

const (
	// muteDefaultDuration (minutes) matches the bot and button default. The
	// scope enum and the 10080-minute (one week) duration ceiling live in the
	// struct tags below — Go struct tags must be literals.
	muteDefaultDuration = 60
	muteVolatilityNote  = "Mutes are held in memory only and are cleared by a processor restart."
)

// v2MuteItem is one active mute on the wire.
type v2MuteItem struct {
	Scope         string  `json:"scope" enum:"gym,pokemon,area,pokestop,station,tracking,everything" doc:"What the mute suppresses: a specific gym/pokestop/station (by id), a pokemon species (by dex id), a geofence area (by name), one tracking rule (by uid), or everything."`
	Value         *string `json:"value" doc:"Scope-specific identifier (gym/pokestop/station id, dex id or rule uid as a numeric string, canonical area name). Null for scope=everything."`
	ExpiresAt     int64   `json:"expires_at" doc:"Unix seconds when the mute lapses."`
	RemainingSecs int64   `json:"remaining_secs" doc:"Seconds until expiry at response time (UI convenience, derived from expires_at)."`
}

// v2MutesOutput is the GET list response: {mutes:[…]}.
type v2MutesOutput struct {
	Body struct {
		Mutes []v2MuteItem `json:"mutes" doc:"Active (non-expired) mutes. Mutes are held in memory only and are cleared by a processor restart."`
	}
}

// v2CreateMuteInput is the POST body: scope + value + optional duration.
type v2CreateMuteInput struct {
	ID   string `path:"id" doc:"Human id (the owning user)"`
	Body struct {
		Scope       string `json:"scope" enum:"gym,pokemon,area,pokestop,station,tracking,everything" doc:"Mute scope. value is required for every scope except 'everything', where it must be absent."`
		Value       string `json:"value,omitempty" required:"false" doc:"Scope identifier: gym/pokestop/station id, pokemon dex id or tracking-rule uid (numeric string), or area name (matched case-insensitively against loaded geofences)."`
		DurationMin int    `json:"duration_min,omitempty" required:"false" minimum:"1" maximum:"10080" doc:"Mute length in minutes. Defaults to 60; maximum one week (10080)."`
	}
}

// v2CreateMuteOutput is the POST response: the canonical entry + replaced flag.
type v2CreateMuteOutput struct {
	Body struct {
		Mute     v2MuteItem `json:"mute"`
		Replaced bool       `json:"replaced" doc:"True when an existing mute with the same scope and value was extended rather than a new one created."`
	}
}

// v2DeleteMutesInput addresses one entry (scope+value), one everything-mute
// (scope only), or — with no params — every mute the human has.
type v2DeleteMutesInput struct {
	ID    string `path:"id" doc:"Human id (the owning user)"`
	Scope string `query:"scope" enum:"gym,pokemon,area,pokestop,station,tracking,everything" required:"false" doc:"Scope of the mute to remove. Omit (together with value) to remove all mutes."`
	Value string `query:"value" required:"false" doc:"Value of the mute to remove, as returned by the list endpoint. Required with scope, except scope=everything which takes none."`
}

// v2DeletedMutesOutput mirrors v2 tracking's delete shape: {deleted:[…]}.
type v2DeletedMutesOutput struct {
	Body struct {
		Deleted []v2MuteItem `json:"deleted" doc:"The removed mutes (empty when a delete-all found none)."`
	}
}

// muteEntryToItem converts a store entry to the wire item. Value is
// present-but-null for scope=everything, matching the v2 null conventions.
func muteEntryToItem(e mute.Entry, now int64) v2MuteItem {
	item := v2MuteItem{Scope: e.ScopeType, ExpiresAt: e.ExpiresAt}
	if e.ScopeType != mute.ScopeEverything {
		v := e.ScopeValue
		item.Value = &v
	}
	if rem := e.ExpiresAt - now; rem > 0 {
		item.RemainingSecs = rem
	}
	return item
}

// muteResolveHuman 404s when the human does not exist; returns the full record
// (community membership feeds area validation).
func muteResolveHuman(deps *TrackingDeps, id string) (*store.Human, error) {
	human, err := deps.Humans.Get(id)
	if err != nil {
		log.Errorf("v2 mutes: human lookup %s: %s", id, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	if human == nil {
		return nil, huma.Error404NotFound("human not found")
	}
	return human, nil
}

// validateMuteValue applies the per-scope value rules shared with the bot
// parser: required except for everything (where it is forbidden), numeric for
// pokemon/tracking, and a known geofence area for area (canonicalised).
// Returns the value to store.
func validateMuteValue(deps *TrackingDeps, human *store.Human, scope, value string) (string, error) {
	if scope == mute.ScopeEverything {
		if value != "" {
			return "", huma.Error422UnprocessableEntity("scope 'everything' takes no value")
		}
		return "", nil
	}
	if value == "" {
		return "", huma.Error422UnprocessableEntity("value is required for scope '" + scope + "'")
	}
	switch scope {
	case mute.ScopePokemon, mute.ScopeTracking:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return "", huma.Error422UnprocessableEntity("value for scope '" + scope + "' must be a positive integer")
		}
		// Canonicalise ("025" → "25"): the matcher and the DELETE lookup
		// compare ScopeValue against strconv-formatted IDs, so a verbatim
		// non-canonical value would create a mute that never fires.
		return strconv.Itoa(n), nil
	case mute.ScopeArea:
		// Prefer AreaLogic (community-aware, like the bot); fall back to the
		// live state's fences — production trackingDeps carries no AreaLogic,
		// and StateMgr stays correct across geofence reloads. Both paths
		// canonicalise the name so the stored value matches `!tracked` output.
		if deps.AreaLogic != nil {
			resolved, ok := deps.AreaLogic.ResolveAvailableArea(value, human.CommunityMembership, false)
			if !ok {
				return "", huma.Error422UnprocessableEntity("unknown area: " + value)
			}
			return resolved, nil
		}
		if deps.StateMgr != nil {
			if st := deps.StateMgr.Get(); st != nil {
				for _, f := range st.Fences {
					if strings.EqualFold(f.Name, value) {
						return f.Name, nil
					}
				}
				return "", huma.Error422UnprocessableEntity("unknown area: " + value)
			}
		}
	}
	return value, nil
}

// RegisterV2Mutes registers the strict v2 mutes endpoints (list, create,
// delete one / delete all) under /api/v2/humans/{id}/mutes.
func RegisterV2Mutes(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-mutes"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	registerV2MutesList(humaAPI, deps, tag, sec)
	registerV2MutesCreate(humaAPI, deps, tag, sec)
	registerV2MutesDelete(humaAPI, deps, tag, sec)
}

func registerV2MutesList(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-human-mutes", Method: "GET", Path: "/v2/humans/{id}/mutes",
		Summary:     "List a human's active mutes",
		Description: "Returns the human's active (non-expired) alert mutes. " + muteVolatilityNote,
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2MutesOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		now := time.Now().Unix()
		out := &v2MutesOutput{}
		out.Body.Mutes = make([]v2MuteItem, 0)
		for _, e := range deps.Mutes.ListActive(in.ID, now) {
			out.Body.Mutes = append(out.Body.Mutes, muteEntryToItem(e, now))
		}
		return out, nil
	})
}

func registerV2MutesCreate(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-create-human-mute", Method: "POST", Path: "/v2/humans/{id}/mutes",
		Summary:     "Mute alerts for a scope",
		Description: "Suppresses the human's alerts matching the scope for duration_min minutes (default 60, max one week). Re-muting the same scope+value extends the expiry and reports replaced=true. " + muteVolatilityNote,
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2CreateMuteInput) (*v2CreateMuteOutput, error) {
		human, err := muteResolveHuman(deps, in.ID)
		if err != nil {
			return nil, err
		}
		value, err := validateMuteValue(deps, human, in.Body.Scope, in.Body.Value)
		if err != nil {
			return nil, err
		}
		duration := in.Body.DurationMin
		if duration == 0 {
			duration = muteDefaultDuration
		}
		now := time.Now().Unix()
		entry := mute.Entry{
			HumanID:    in.ID,
			ScopeType:  in.Body.Scope,
			ScopeValue: value,
			ExpiresAt:  now + int64(duration)*60,
		}
		replaced := deps.Mutes.Add(entry)
		out := &v2CreateMuteOutput{}
		out.Body.Mute = muteEntryToItem(entry, now)
		out.Body.Replaced = replaced
		return out, nil
	})
}

func registerV2MutesDelete(api huma.API, deps *TrackingDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-delete-human-mutes", Method: "DELETE", Path: "/v2/humans/{id}/mutes",
		Summary:     "Remove one mute or all mutes",
		Description: "With ?scope= (and ?value= for every scope except 'everything'): removes that single mute, 404 when it does not exist. With no params: removes every mute the human has. Returns the removed entries.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2DeleteMutesInput) (*v2DeletedMutesOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		now := time.Now().Unix()
		out := &v2DeletedMutesOutput{}
		out.Body.Deleted = make([]v2MuteItem, 0)

		// Delete-all: no scope, no value.
		if in.Scope == "" && in.Value == "" {
			for _, e := range deps.Mutes.ListActive(in.ID, now) {
				out.Body.Deleted = append(out.Body.Deleted, muteEntryToItem(e, now))
			}
			deps.Mutes.RemoveAll(in.ID)
			return out, nil
		}
		if in.Scope == "" {
			return nil, huma.Error422UnprocessableEntity("value requires scope")
		}
		if in.Scope == mute.ScopeEverything && in.Value != "" {
			return nil, huma.Error422UnprocessableEntity("scope 'everything' takes no value")
		}
		if in.Scope != mute.ScopeEverything && in.Value == "" {
			return nil, huma.Error422UnprocessableEntity("value is required for scope '" + in.Scope + "'")
		}

		// Snapshot the entry first so the response can carry it.
		var found *mute.Entry
		for _, e := range deps.Mutes.List(in.ID) {
			if e.ScopeType == in.Scope && e.ScopeValue == in.Value {
				cp := e
				found = &cp
				break
			}
		}
		if found == nil || !deps.Mutes.Remove(in.ID, in.Scope, in.Value) {
			return nil, huma.Error404NotFound("mute not found")
		}
		out.Body.Deleted = append(out.Body.Deleted, muteEntryToItem(*found, now))
		return out, nil
	})
}
