package api

import (
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// CommandDeps holds all dependencies needed for command execution.
type CommandDeps struct {
	DB           *sqlx.DB
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
	ReloadFunc   func()
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
func HandleCommand(deps *CommandDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req commandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeCommandJSON(w, http.StatusBadRequest, commandResponse{Status: "error"})
			return
		}

		if req.Text == "" || req.UserID == "" {
			writeCommandJSON(w, http.StatusBadRequest, commandResponse{Status: "error"})
			return
		}

		// Parse commands from text
		parsed := deps.Parser.Parse(req.Text)
		if len(parsed) == 0 {
			writeCommandJSON(w, http.StatusOK, commandResponse{Status: "ok", Replies: nil})
			return
		}

		// Look up user in DB for language, profile, location, area
		userLang, profileNo, hasLocation, hasArea := lookupUserState(deps.DB, req.UserID, deps.Config.General.Locale)

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
				DB:           deps.DB,
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
				ReloadFunc:   deps.ReloadFunc,
			}

			// Handle target override (user<id>, name<webhook>)
			target, remainingArgs, err := bot.BuildTarget(deps.DB, ctx, cmd.Args)
			if err != nil {
				log.Debugf("command: target resolution failed: %v", err)
				allReplies = append(allReplies, bot.Reply{React: "🙅", Text: err.Error()})
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

		writeCommandJSON(w, http.StatusOK, commandResponse{Status: "ok", Replies: allReplies})
	}
}

// lookupUserState loads basic user info for command context building.
func lookupUserState(database *sqlx.DB, userID, defaultLocale string) (lang string, profileNo int, hasLocation, hasArea bool) {
	lang = defaultLocale
	profileNo = 1

	var h struct {
		Language  *string `db:"language"`
		ProfileNo int     `db:"current_profile_no"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
		Area      *string `db:"area"`
	}
	err := database.Get(&h, "SELECT language, current_profile_no, latitude, longitude, area FROM humans WHERE id = ? LIMIT 1", userID)
	if err != nil {
		return // defaults
	}

	if h.Language != nil && *h.Language != "" {
		lang = *h.Language
	}
	profileNo = h.ProfileNo
	hasLocation = h.Latitude != 0 || h.Longitude != 0
	hasArea = h.Area != nil && *h.Area != "" && *h.Area != "[]"
	return
}

func writeCommandJSON(w http.ResponseWriter, status int, resp commandResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
