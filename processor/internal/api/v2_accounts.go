package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_accounts.go is the strict /api/v2 surface for a human's linked game
// accounts (player IDs) used by personal shiny/background tracking. The API
// is trusted (it requires the API secret), so [player_accounts]
// self_service does not apply here.

type v2AccountItem struct {
	PlayerID  string    `json:"player_id" doc:"Game player ID, opaque and case-sensitive (1-256 bytes)."`
	Label     *string   `json:"label" nullable:"true" doc:"Optional label, unique per human (case-insensitive). null when unset."`
	CreatedAt time.Time `json:"created_at" doc:"When the account was linked."`
}

type v2AccountsOutput struct {
	Body struct {
		Accounts []v2AccountItem `json:"accounts"`
	}
}

type v2AddAccountBody struct {
	PlayerID string `json:"player_id" required:"true" minLength:"1" maxLength:"256" doc:"Game player ID (case-sensitive)."`
	Label    string `json:"label,omitempty" maxLength:"64" doc:"Optional label, unique per human."`
}

type v2AddAccountInput struct {
	ID   string `path:"id" doc:"Human id"`
	Body v2AddAccountBody
}

type v2AccountPathInput struct {
	ID       string `path:"id" doc:"Human id"`
	PlayerID string `path:"player_id" doc:"Game player ID. Must be URL-escaped; a player ID containing / is sent as %2F."`
}

type v2LabelAccountInput struct {
	ID       string `path:"id" doc:"Human id"`
	PlayerID string `path:"player_id" doc:"Game player ID. Must be URL-escaped; a player ID containing / is sent as %2F."`
	Body     struct {
		Label string `json:"label" maxLength:"64" doc:"New label; empty clears it."`
	}
}

type v2AccountCreatedOutput struct {
	Status int
	Body   struct {
		Account v2AccountItem `json:"account"`
	}
}

func accountToItem(a db.PlayerAccount) v2AccountItem {
	it := v2AccountItem{PlayerID: a.PlayerID, CreatedAt: a.CreatedAt}
	if a.Label != "" {
		l := a.Label
		it.Label = &l
	}
	return it
}

func accountStoreError(op, id string, err error) error {
	switch {
	case errors.Is(err, store.ErrDuplicateAccount):
		return huma.Error409Conflict("player id already linked")
	case errors.Is(err, store.ErrDuplicateAccountLabel):
		return huma.Error409Conflict("label already in use")
	case errors.Is(err, store.ErrTooManyAccounts), errors.Is(err, store.ErrInvalidPlayerID), errors.Is(err, store.ErrInvalidAccountLabel):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, store.ErrAccountNotFound):
		return huma.Error404NotFound("account not found")
	}
	log.Errorf("v2 accounts: %s %s: %s", op, id, err)
	return huma.Error500InternalServerError("database error")
}

// RegisterV2Accounts registers /api/v2/humans/{id}/accounts.
func RegisterV2Accounts(humaAPI huma.API, deps *TrackingDeps) {
	tag := []string{"v2-accounts"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-list-human-accounts", Method: "GET", Path: "/v2/humans/{id}/accounts",
		Summary: "List a human's linked game accounts", Tags: tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2AccountsOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		list, err := deps.Accounts.List(in.ID)
		if err != nil {
			return nil, accountStoreError("list", in.ID, err)
		}
		out := &v2AccountsOutput{}
		out.Body.Accounts = make([]v2AccountItem, 0, len(list))
		for _, a := range list {
			out.Body.Accounts = append(out.Body.Accounts, accountToItem(a))
		}
		return out, nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-add-human-account", Method: "POST", Path: "/v2/humans/{id}/accounts", DefaultStatus: 201,
		Summary:     "Link a game account",
		Description: "Links a player ID to the human. 409 if the player ID or label is already linked to this human; 422 when invalid or over the 50-account limit. Triggers a state reload.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AddAccountInput) (*v2AccountCreatedOutput, error) {
		if _, err := muteResolveHuman(deps, in.ID); err != nil {
			return nil, err
		}
		if err := deps.Accounts.Add(in.ID, in.Body.PlayerID, in.Body.Label); err != nil {
			return nil, accountStoreError("add", in.ID, err)
		}
		reloadState(deps)
		a, err := deps.Accounts.Resolve(in.ID, in.Body.PlayerID)
		if err != nil || a == nil {
			return nil, accountStoreError("add", in.ID, errors.Join(err, store.ErrAccountNotFound))
		}
		out := &v2AccountCreatedOutput{Status: 201}
		out.Body.Account = accountToItem(*a)
		return out, nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-label-human-account", Method: "PUT", Path: "/v2/humans/{id}/accounts/{player_id}",
		Summary:     "Set or clear a linked account's label",
		Description: "Player IDs containing / must be URL-escaped (%2F) in the path.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2LabelAccountInput) (*statusOKOutput, error) {
		if err := deps.Accounts.SetLabel(in.ID, in.PlayerID, in.Body.Label); err != nil {
			return nil, accountStoreError("label", in.ID, err)
		}
		reloadState(deps)
		return okStatus(), nil
	})

	huma.Register(humaAPI, huma.Operation{
		OperationID: "v2-delete-human-account", Method: "DELETE", Path: "/v2/humans/{id}/accounts/{player_id}",
		Summary:     "Unlink a game account",
		Description: "Player IDs containing / must be URL-escaped (%2F) in the path.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2AccountPathInput) (*statusOKOutput, error) {
		if err := deps.Accounts.Remove(in.ID, in.PlayerID); err != nil {
			return nil, accountStoreError("remove", in.ID, err)
		}
		reloadState(deps)
		return okStatus(), nil
	})
}
