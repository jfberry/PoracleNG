package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/nlp"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// CommandDeps holds all dependencies needed for command execution.
type CommandDeps struct {
	DB           *sqlx.DB
	Humans       store.HumanStore
	Tracking     *store.TrackingStores
	Config       *config.Config
	StateMgr     *state.Manager
	GameData     *gamedata.GameData
	Translations *i18n.Bundle
	Dispatcher   *delivery.Dispatcher
	RowText      *rowtext.Generator
	Resolver     *bot.PokemonResolver
	ArgMatcher   *bot.ArgMatcher
	Parser       *bot.Parser
	Registry     *bot.Registry
	Weather      *tracker.WeatherTracker
	Stats        *tracker.StatsTracker
	DTS          *dts.TemplateStore
	Emoji        *dts.EmojiLookup
	NLPParser     *nlp.Parser
	TestProcessor bot.TestProcessor
	ReloadFunc    func()
}

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
func HandleCommand(deps *CommandDeps) gin.HandlerFunc {
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

		// Parse commands from text
		parsed := deps.Parser.Parse(req.Text)
		if len(parsed) == 0 {
			c.JSON(http.StatusOK, commandResponse{Status: "ok", Replies: nil})
			return
		}

		// Look up user in DB for language, profile, location, area
		userLang, profileNo, hasLocation, hasArea, _ := bot.LookupUserStateFromStore(deps.Humans, req.UserID, deps.Config.General.Locale)

		// Check admin status
		isAdmin := bot.IsAdmin(deps.Config, req.Platform, req.UserID)

		// Get geofence data from state
		var spatialIndex *geofence.SpatialIndex
		var fences []geofence.Fence
		st := deps.StateMgr.Get()
		if st != nil {
			spatialIndex = st.Geofence
			fences = st.Fences
		}

		var allReplies []bot.Reply

		// Merge consecutive cmd.apply pipe groups back into single invocations.
		parsed = bot.MergeApplyGroups(parsed)

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
			if !bot.CommandAllowed(deps.Config, req.Platform, cmd.CommandKey, req.UserID, nil) {
				allReplies = append(allReplies, bot.Reply{React: "🙅"})
				continue
			}

			// Build context
			ctx := &bot.CommandContext{
				UserID:       req.UserID,
				UserName:     req.UserName,
				Platform:     req.Platform,
				ChannelID:    req.ChannelID,
				GuildID:      req.GuildID,
				IsDM:         req.IsDM,
				IsAdmin:      isAdmin,
				Language:     userLang,
				ProfileNo:    profileNo,
				HasLocation:  hasLocation,
				HasArea:      hasArea,
				TargetID:     req.UserID,
				TargetName:   req.UserName,
				TargetType:   req.Platform + ":user",
				AreaLogic:    bot.NewAreaLogic(fences, deps.Config),
				DB:           deps.DB,
				Humans:       deps.Humans,
				Tracking:     deps.Tracking,
				Config:       deps.Config,
				StateMgr:     deps.StateMgr,
				GameData:     deps.GameData,
				Translations: deps.Translations,
				Geofence:     spatialIndex,
				Fences:       fences,
				Dispatcher:   deps.Dispatcher,
				RowText:      deps.RowText,
				Resolver:     deps.Resolver,
				ArgMatcher:   deps.ArgMatcher,
				Weather:      deps.Weather,
				Stats:        deps.Stats,
				DTS:          deps.DTS,
				Emoji:        deps.Emoji,
				NLP:           deps.NLPParser,
				TestProcessor: deps.TestProcessor,
				Registry:      deps.Registry,
				ReloadFunc:    deps.ReloadFunc,
			}

			// Handle target override (user<id>, name<webhook>)
			target, remainingArgs, err := bot.BuildTarget(ctx, cmd.Args)
			if err != nil {
				log.Debugf("command: target resolution failed: %v", err)
				allReplies = append(allReplies, bot.Reply{React: "🙅", Text: bot.LocalizeTargetError(deps.Translations.For(userLang), err)})
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
			allReplies = append(allReplies, replies...)
		}

		c.JSON(http.StatusOK, commandResponse{Status: "ok", Replies: allReplies})
	}
}
