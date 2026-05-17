package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

type commandRequest struct {
	Text      string `json:"text"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Platform  string `json:"platform"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	IsDM      bool   `json:"is_dm"`
}

type commandResponse struct {
	Status  string      `json:"status"`
	Replies []bot.Reply `json:"replies"`
}

// HandleCommand returns the POST /api/command handler.
// This endpoint allows testing commands without a bot gateway.
//
// The deps *bot.BotDeps must have Parser populated (added by the caller on
// top of sharedBotDeps) so the endpoint can tokenise the raw text.
func HandleCommand(deps *bot.BotDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req commandRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, commandResponse{Status: "error"})
			return
		}

		if req.Text == "" || req.UserID == "" {
			c.JSON(http.StatusBadRequest, commandResponse{Status: "error"})
			return
		}

		if deps == nil || deps.Parser == nil {
			c.JSON(http.StatusInternalServerError, commandResponse{Status: "error"})
			return
		}

		// Parse commands from text
		parsed := deps.Parser.Parse(req.Text)
		if len(parsed) == 0 {
			c.JSON(http.StatusOK, commandResponse{Status: "ok", Replies: nil})
			return
		}

		// Look up user in DB for language, profile, location, area
		userLang, profileNo, hasLocation, hasArea, _ := bot.LookupUserStateFromStore(deps.Humans, req.UserID, deps.Cfg.General.Locale)

		// Check admin status
		isAdmin := bot.IsAdmin(deps.Cfg, req.Platform, req.UserID)

		// Get geofence data from state
		var spatialIndex *geofence.SpatialIndex
		var fences []geofence.Fence
		if deps.StateMgr != nil {
			if st := deps.StateMgr.Get(); st != nil {
				spatialIndex = st.Geofence
				fences = st.Fences
			}
		}

		var allReplies []bot.Reply

		// Merge consecutive cmd.apply pipe groups back into single invocations.
		parsed = bot.MergeApplyGroups(parsed)

		// Translator for maintenance suffix and error messages.
		tr := deps.Translations.For(userLang)

		for _, cmd := range parsed {
			if cmd.CommandKey == "" {
				// Unknown command
				if req.IsDM {
					allReplies = append(allReplies, bot.Reply{
						Text: "Unknown command",
					})
				}
				continue
			}

			// Look up command handler
			handler := deps.Registry.Lookup(cmd.CommandKey)
			if handler == nil {
				continue
			}

			// Check command security
			if !bot.CommandAllowed(deps.Cfg, req.Platform, cmd.CommandKey, req.UserID, nil) {
				allReplies = append(allReplies, bot.Reply{React: "🙅"})
				continue
			}

			// Build context via NewCommandContext so every BotDeps closure
			// (WebhookRate, AlertLimiter, GeocoderStats/Clear, Reconciler,
			// SlashSync, LogBuffer, etc.) is populated — identical to the
			// gateway and slash surfaces.
			ctx := bot.NewCommandContext(deps)
			// Overlay per-request fields on top of the shared BotDeps.
			ctx.UserID = req.UserID
			ctx.UserName = req.UserName
			ctx.Platform = req.Platform
			ctx.ChannelID = req.ChannelID
			ctx.GuildID = req.GuildID
			ctx.IsDM = req.IsDM
			ctx.IsAdmin = isAdmin
			ctx.Language = userLang
			ctx.ProfileNo = profileNo
			ctx.HasLocation = hasLocation
			ctx.HasArea = hasArea
			ctx.TargetID = req.UserID
			ctx.TargetName = req.UserName
			ctx.TargetType = req.Platform + ":user"
			ctx.AreaLogic = bot.NewAreaLogic(fences, deps.Cfg)
			ctx.Geofence = spatialIndex
			ctx.Fences = fences

			// Handle target override (user<id>, name<webhook>)
			target, remainingArgs, err := bot.BuildTarget(ctx, cmd.Args)
			if err != nil {
				log.Debugf("command: target resolution failed: %v", err)
				allReplies = append(allReplies, bot.Reply{React: "🙅", Text: bot.LocalizeTargetError(tr, err)})
				continue
			}
			if target != nil {
				ctx.TargetID = target.ID
				ctx.TargetName = target.Name
				ctx.TargetType = target.Type
				if target.Language != "" {
					ctx.Language = target.Language
				}
				ctx.ProfileNo = target.ProfileNo
				ctx.HasLocation = target.HasLocation
				ctx.HasArea = target.HasArea
			}

			replies := handler.Run(ctx, remainingArgs)
			// Apply maintenance suffix so callers see the paused-delivery
			// warning, matching discordbot/bot.go and telegrambot/bot.go.
			replies = bot.ApplyMaintenanceSuffix(replies, deps.Dispatcher, tr.T("cmd.maintenance.active_suffix"))
			allReplies = append(allReplies, replies...)
		}

		c.JSON(http.StatusOK, commandResponse{Status: "ok", Replies: allReplies})
	}
}
