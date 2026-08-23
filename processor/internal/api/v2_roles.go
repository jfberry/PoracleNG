package api

import (
	"context"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/discordroles"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// v2_roles.go is the strict /api/v2 roles surface. Each op mirrors the exact
// discordgo logic, validation, and error handling of the frozen v1 handlers in
// roles.go — reusing RoleDeps + the discordroles helpers. The differences are
// strict typed huma inputs, problem+json errors, and REST-clean paths:
//   - POST   /v2/humans/{id}/roles/{roleId}   (v1: roles/add/{roleId})
//   - DELETE /v2/humans/{id}/roles/{roleId}   (v1: roles/remove/{roleId})
//
// Success-shape convention: GETs return the resource ({guilds:...} /
// {admin:...}); role add/remove return {result:[...]} (the discordroles change
// list, matching v1).

// resolveHumanForRoles looks up a human via the RoleDeps store, returning an
// huma 404 when missing and 500 on a store error.
func resolveHumanForRoles(deps *RoleDeps, id string) (*store.Human, error) {
	human, err := deps.Humans.Get(id)
	if err != nil {
		log.Errorf("v2 roles: get human %s: %s", id, err)
		return nil, huma.Error500InternalServerError("database error")
	}
	if human == nil {
		return nil, huma.Error404NotFound("user not found")
	}
	return human, nil
}

// --- list roles -------------------------------------------------------------

// v2RolesOutput is the GET /v2/humans/{id}/roles response. For non-discord:user
// humans it carries an empty guild list (matching v1's [] response semantics).
type v2RolesOutput struct {
	Body struct {
		Guilds []discordroles.GuildRoleInfo `json:"guilds"`
	}
}

func registerV2RolesList(api huma.API, deps *RoleDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-list-human-roles", Method: "GET", Path: "/v2/humans/{id}/roles",
		Summary:     "List a human's subscribable Discord roles",
		Description: "Lists the configured subscription roles across all guilds for a discord:user, marking which are set. Empty guild list for non-discord:user humans. 404 if the human does not exist; 503 if the Discord bot is not running.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2RolesOutput, error) {
		h, err := resolveHumanForRoles(deps, in.ID)
		if err != nil {
			return nil, err
		}
		out := &v2RolesOutput{}
		out.Body.Guilds = []discordroles.GuildRoleInfo{}
		if h.Type != "discord:user" {
			return out, nil
		}
		if deps.session() == nil {
			return nil, huma.Error503ServiceUnavailable("Discord bot not available")
		}
		roleSubMap := deps.Config.Discord.RoleSubscriptionMap()
		guilds, err := discordroles.ListUserRolesAcrossGuilds(deps.session(), roleSubMap, h.ID)
		if err != nil {
			log.Errorf("v2 roles: list roles %s: %s", in.ID, err)
			return nil, huma.Error500InternalServerError("failed to list roles")
		}
		out.Body.Guilds = guilds
		return out, nil
	})
}

// --- add / remove role ------------------------------------------------------

type v2RoleChangeInput struct {
	ID     string `path:"id" doc:"Human id (the owning user)"`
	RoleID string `path:"roleId" doc:"Discord role id to add/remove"`
}

// v2RoleChangeOutput carries the discordroles change list (matching v1's
// {result:[...]}). Empty for non-discord:user humans.
type v2RoleChangeOutput struct {
	Body struct {
		Result []discordroles.RoleInfo `json:"result"`
	}
}

func roleChangeHandler(deps *RoleDeps, add bool) func(context.Context, *v2RoleChangeInput) (*v2RoleChangeOutput, error) {
	return func(_ context.Context, in *v2RoleChangeInput) (*v2RoleChangeOutput, error) {
		h, err := resolveHumanForRoles(deps, in.ID)
		if err != nil {
			return nil, err
		}
		out := &v2RoleChangeOutput{}
		out.Body.Result = []discordroles.RoleInfo{}
		if h.Type != "discord:user" {
			return out, nil
		}
		if deps.session() == nil {
			return nil, huma.Error503ServiceUnavailable("Discord bot not available")
		}
		roleSubMap := deps.Config.Discord.RoleSubscriptionMap()
		changes, err := discordroles.SetUserRole(deps.session(), roleSubMap, h.ID, in.RoleID, add)
		if err != nil {
			log.Errorf("v2 roles: set role %s/%s: %s", in.ID, in.RoleID, err)
			return nil, huma.Error500InternalServerError("failed to set role")
		}
		out.Body.Result = changes
		return out, nil
	}
}

func registerV2RolesAdd(api huma.API, deps *RoleDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-add-human-role", Method: "POST", Path: "/v2/humans/{id}/roles/{roleId}",
		Summary:     "Add a subscription role to a human",
		Description: "Adds the given Discord subscription role (validated against the configured role subscription map). Returns the resulting role set. 404 if the human does not exist; 503 if the Discord bot is not running.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, roleChangeHandler(deps, true))
}

func registerV2RolesRemove(api huma.API, deps *RoleDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-remove-human-role", Method: "DELETE", Path: "/v2/humans/{id}/roles/{roleId}",
		Summary:     "Remove a subscription role from a human",
		Description: "Removes the given Discord subscription role. Returns the resulting role set. 404 if the human does not exist; 503 if the Discord bot is not running.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, roleChangeHandler(deps, false))
}

// --- administration roles ---------------------------------------------------

// v2AdminRolesOutput is the GET /v2/humans/{id}/admin-roles response, mirroring
// v1's {admin: adminRolesResult}.
type v2AdminRolesOutput struct {
	Body struct {
		Admin adminRolesResult `json:"admin"`
	}
}

func registerV2AdminRoles(api huma.API, deps *RoleDeps, tag []string, sec []map[string][]string) {
	huma.Register(api, huma.Operation{
		OperationID: "v2-get-human-admin-roles", Method: "GET", Path: "/v2/humans/{id}/admin-roles",
		Summary:     "Get a human's delegated-administration permissions",
		Description: "Reports which channels/webhooks/users the human may administer on Discord and Telegram, derived from delegated_administration config and the human's Discord roles. 404 if the human does not exist.",
		Tags:        tag, Security: sec, RejectUnknownQueryParameters: true,
	}, func(_ context.Context, in *v2HumanIDInput) (*v2AdminRolesOutput, error) {
		if _, err := resolveHumanForRoles(deps, in.ID); err != nil {
			return nil, err
		}
		out := &v2AdminRolesOutput{}
		out.Body.Admin = computeAdministrationRoles(deps, in.ID)
		return out, nil
	})
}

// computeAdministrationRoles builds the same adminRolesResult v1's
// HandleGetAdministrationRoles produces, factored out so v2 can reuse the exact
// delegated-administration logic without duplicating it inline. v1 is left
// untouched (it keeps its own copy of this logic).
func computeAdministrationRoles(deps *RoleDeps, id string) adminRolesResult {
	result := adminRolesResult{}

	// Discord administration roles
	if hasDiscordToken(deps.Config) {
		discord := &discordAdminRoles{
			Channels: []string{},
			Webhooks: []string{},
			Users:    false,
		}

		da := deps.Config.Discord.DelegatedAdministration
		var userRoles []string
		rolesFetched := false

		fetchRoles := func() []string {
			if rolesFetched {
				return userRoles
			}
			rolesFetched = true
			if deps.session() != nil {
				roles, err := discordroles.GetUserRoleIDs(deps.session(), deps.Config.Discord.Guilds, id)
				if err != nil {
					log.Warnf("v2 roles: get user roles for %s: %v", id, err)
				} else {
					userRoles = roles
				}
			}
			return userRoles
		}

		// Channel tracking permissions
		if len(da.ChannelTracking) > 0 {
			roles := fetchRoles()
			rolesAndID := append(roles, id)
			rolesAndIDSet := make(map[string]bool, len(rolesAndID))
			for _, r := range rolesAndID {
				rolesAndIDSet[r] = true
			}

			var allChannels map[string][]discordroles.ChannelInfo
			channelsFetched := false

			for targetID, allowedIDs := range da.ChannelTracking {
				if !containsAny(allowedIDs, rolesAndIDSet) {
					continue
				}

				if !channelsFetched && deps.session() != nil {
					channelsFetched = true
					var err error
					allChannels, err = discordroles.GetAllChannelIDs(deps.session(), deps.Config.Discord.Guilds)
					if err != nil {
						log.Warnf("v2 roles: get all channels: %v", err)
					}
				}

				for _, guildID := range deps.Config.Discord.Guilds {
					if targetID == guildID {
						if chs, ok := allChannels[guildID]; ok {
							for _, ch := range chs {
								discord.Channels = append(discord.Channels, ch.ID)
							}
						}
					}
				}

				for _, guildID := range deps.Config.Discord.Guilds {
					chs, ok := allChannels[guildID]
					if !ok {
						continue
					}
					for _, ch := range chs {
						if ch.CategoryID == targetID {
							discord.Channels = append(discord.Channels, ch.ID)
						}
					}
					for _, ch := range chs {
						if ch.ID == targetID {
							discord.Channels = append(discord.Channels, targetID)
							break
						}
					}
				}
			}
		}

		// Webhook tracking permissions
		if len(da.WebhookTracking) > 0 {
			roles := fetchRoles()
			rolesSet := make(map[string]bool, len(roles))
			for _, r := range roles {
				rolesSet[r] = true
			}

			for webhookName, allowedIDs := range da.WebhookTracking {
				if slices.Contains(allowedIDs, id) {
					discord.Webhooks = append(discord.Webhooks, webhookName)
				}
				if !contains(discord.Webhooks, webhookName) {
					for _, aid := range allowedIDs {
						if rolesSet[aid] {
							discord.Webhooks = append(discord.Webhooks, webhookName)
							break
						}
					}
				}
			}
		}

		// User tracking permissions
		if len(da.UserTracking) > 0 {
			roles := fetchRoles()
			rolesAndID := append(roles, id)
			rolesAndIDSet := make(map[string]bool, len(rolesAndID))
			for _, r := range rolesAndID {
				rolesAndIDSet[r] = true
			}
			for _, allowedID := range da.UserTracking {
				if rolesAndIDSet[allowedID] {
					discord.Users = true
					break
				}
			}
		}

		result.Discord = discord
	}

	// Telegram administration roles
	if hasTelegramToken(deps.Config) {
		telegram := &telegramAdminRoles{
			Channels: []string{},
			Users:    false,
		}

		tda := deps.Config.Telegram.DelegatedAdministration

		if len(tda.ChannelTracking) > 0 {
			for channelID, allowedIDs := range tda.ChannelTracking {
				if slices.Contains(allowedIDs, id) {
					telegram.Channels = append(telegram.Channels, channelID)
				}
			}
		}

		if len(tda.UserTracking) > 0 {
			if slices.Contains(tda.UserTracking, id) {
				telegram.Users = true
			}
		}

		result.Telegram = telegram
	}

	return result
}

// RegisterV2Roles registers the strict v2 roles endpoints (list, add, remove,
// admin-roles) under /api/v2.
func RegisterV2Roles(humaAPI huma.API, deps *RoleDeps) {
	tag := []string{"v2-roles"}
	sec := []map[string][]string{{"poracleSecret": {}}}

	registerV2RolesList(humaAPI, deps, tag, sec)
	registerV2RolesAdd(humaAPI, deps, tag, sec)
	registerV2RolesRemove(humaAPI, deps, tag, sec)
	registerV2AdminRoles(humaAPI, deps, tag, sec)
}
