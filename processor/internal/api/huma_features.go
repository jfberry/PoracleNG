package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// summaryIDInput carries the {id} path parameter for the list-for-user read.
type summaryIDInput struct {
	ID string `path:"id"`
}

// summaryAlertInput carries the {id} and {alertType} path parameters shared by
// the get / delete / trigger summary ops.
type summaryAlertInput struct {
	ID        string `path:"id"`
	AlertType string `path:"alertType"`
}

// summarySetBody is the schedule-upsert request body. It captures the raw bytes
// verbatim (like openJSON) so the handler keeps the legacy lenient parsing —
// accepting active_hours as a JSON array OR a JSON-string-encoded array, with
// db.ParseActiveHours tolerating zero-padded ("00") ints. Unlike openJSON, its
// Schema() advertises a DOCUMENTED but PERMISSIVE schema: it describes the
// {active_hours: [ActiveHourEntry]} array shape for the docs while staying
// permissive (anyOf array|string, no strict per-field types) so huma's
// pre-bind validation does not reject the legacy lenient forms. (huma validates
// the body against the schema BEFORE binding, so a strict schema here would
// break leniency — see reference_huma_gotchas.)
type summarySetBody json.RawMessage

func (b *summarySetBody) UnmarshalJSON(data []byte) error {
	*b = append((*b)[:0], data...)
	return nil
}

func (b summarySetBody) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	return b, nil
}

// Schema documents the active_hours array shape while remaining permissive.
func (summarySetBody) Schema(huma.Registry) *huma.Schema {
	// Permissive entry: object with the documented fields but NO strict per-
	// field types or `required`, so zero-padded string ints ("00") still pass.
	entry := &huma.Schema{
		Type:                 "object",
		Description:          "Active-hours entry: {day 0-6, hours 0-23, mins 0-59, optional step, end_hours, end_mins}. Integer fields also accept zero-padded strings (e.g. \"00\").",
		AdditionalProperties: true,
	}
	return &huma.Schema{
		Type: "object",
		Properties: map[string]*huma.Schema{
			"active_hours": {
				Description: "Active-hours schedule. Preferred form is a JSON array of entries " +
					"{day, hours, mins, optional step/end_hours/end_mins}. For backward compatibility " +
					"this in-place endpoint also accepts a JSON-string-encoded array. An empty array " +
					"(or \"\"/\"[]\"/\"{}\") clears the schedule.",
				AnyOf: []*huma.Schema{
					{Type: "array", Items: entry},
					{Type: "string"},
				},
			},
		},
		// Open: no Required, additionalProperties allowed — keeps the lenient
		// legacy contract intact under huma's pre-bind validation.
		AdditionalProperties: true,
	}
}

// summarySetInput carries the {id} / {alertType} path parameters plus the
// schedule-upsert body. The body is summarySetBody (raw-captured + documented
// permissive schema) so the handler can unmarshal it into summarySetRequest
// with the exact lenient logic the legacy gin handler used.
type summarySetInput struct {
	ID        string         `path:"id"`
	AlertType string         `path:"alertType"`
	Body      summarySetBody `contentType:"application/json"`
}

// RegisterSummarySet registers POST /api/summaries/{id}/{alertType} (the
// schedule upsert) on the shared huma instance, replacing the legacy gin
// HandleSummarySet. It preserves the lenient active_hours acceptance in place
// (string-or-array body, flex "00" ints) by routing the raw body through the
// existing summarySetRequest type and db.ParseActiveHours; the strict typed
// schema is a separate v2 concern. Success body is the same {"status":"ok"};
// error paths surface as problem+json.
func RegisterSummarySet(api huma.API, deps *SummaryDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "set-summary", Method: "POST", Path: "/summaries/{id}/{alertType}",
		Summary: "Upsert a summary schedule", Tags: []string{"summaries"},
		Description: "Upsert the active-hours schedule for (id, alertType). The " +
			"`active_hours` field is an array of entries " +
			"`{day 0-6, hours 0-23, mins 0-59, optional step, end_hours, end_mins}`. " +
			"For backward compatibility with existing clients this in-place endpoint " +
			"also accepts the legacy lenient forms: `active_hours` may be a " +
			"JSON-string-encoded array, and integer fields may be zero-padded strings " +
			"(e.g. \"00\"). An empty array (or \"\"/\"[]\"/\"{}\") clears the schedule.",
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *summarySetInput) (*statusOKOutput, error) {
		if deps.Schedules == nil {
			return nil, huma.Error503ServiceUnavailable(summaryDisabledMsg)
		}
		if in.ID == "" || in.AlertType == "" {
			return nil, huma.Error400BadRequest("missing path parameter")
		}
		if !isKnownSummaryAlertType(in.AlertType) {
			return nil, huma.Error400BadRequest(unknownAlertTypeMsg)
		}

		var req summarySetRequest
		if err := json.Unmarshal([]byte(in.Body), &req); err != nil {
			return nil, huma.Error400BadRequest("invalid request body")
		}
		if req.ActiveHours == nil {
			return nil, huma.Error400BadRequest("active_hours must be specified")
		}

		var hoursJSON string
		switch v := req.ActiveHours.(type) {
		case string:
			hoursJSON = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, huma.Error400BadRequest("active_hours could not be serialised")
			}
			hoursJSON = string(b)
		}

		// Validate through the same parser the scheduler uses; empty
		// ""/"[]"/"{}" are allowed (clears the schedule).
		if _, perr := db.ParseActiveHours(hoursJSON); perr != nil {
			return nil, huma.Error400BadRequest("active_hours is not valid JSON for an ActiveHourEntry array")
		}

		if err := deps.Schedules.Set(in.ID, in.AlertType, hoursJSON); err != nil {
			log.Errorf("Summary API: set %s/%s: %v", in.ID, in.AlertType, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if deps.ReloadFunc != nil {
			deps.ReloadFunc()
		}
		out := &statusOKOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}

// RegisterSummaries registers the read/delete/trigger summary ops on the shared
// huma instance. It mirrors the legacy gin handlers (HandleSummaryListForUser,
// HandleSummaryGet, HandleSummaryDelete, HandleSummaryTrigger) byte-for-byte on
// success; error paths now surface as problem+json. The POST upsert is
// registered separately by RegisterSummarySet.
func RegisterSummaries(api huma.API, deps *SummaryDeps) {
	// GET /api/summaries/{id} — list every known alert type's schedule.
	huma.Register(api, huma.Operation{
		OperationID: "list-summaries-for-user", Method: "GET", Path: "/summaries/{id}",
		Summary: "List summary schedules for a user", Tags: []string{"summaries"},
		Description: "Returns {status, schedules}: the user's summary schedules across alert types (currently only \"quest\" supports summaries). 503 when the summary feature is disabled.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *summaryIDInput) (*summaryListOutput, error) {
		if deps.Schedules == nil {
			return nil, huma.Error503ServiceUnavailable(summaryDisabledMsg)
		}
		if in.ID == "" {
			return nil, huma.Error400BadRequest("missing id parameter")
		}
		schedules := make([]summaryScheduleResponse, 0)
		for _, alertType := range knownSummaryAlertTypes {
			s, err := deps.Schedules.Get(in.ID, alertType)
			if err != nil {
				log.Errorf("Summary API: get %s/%s: %v", in.ID, alertType, err)
				return nil, huma.Error500InternalServerError("database error")
			}
			if s == nil {
				continue
			}
			schedules = append(schedules, toSummaryResponse(s))
		}
		out := &summaryListOutput{}
		out.Body.Status = "ok"
		out.Body.Schedules = schedules
		return out, nil
	})

	// GET /api/summaries/{id}/{alertType} — fetch one schedule.
	huma.Register(api, huma.Operation{
		OperationID: "get-summary", Method: "GET", Path: "/summaries/{id}/{alertType}",
		Summary: "Get a summary schedule", Tags: []string{"summaries"},
		Description: "Returns {status, schedule}: the user's summary schedule for one alert type. The schedule's active_hours decide when buffered alerts are dispatched as a digest. 503 when the summary feature is disabled.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *summaryAlertInput) (*summaryGetOutput, error) {
		if deps.Schedules == nil {
			return nil, huma.Error503ServiceUnavailable(summaryDisabledMsg)
		}
		if in.ID == "" || in.AlertType == "" {
			return nil, huma.Error400BadRequest("missing path parameter")
		}
		if !isKnownSummaryAlertType(in.AlertType) {
			return nil, huma.Error400BadRequest(unknownAlertTypeMsg)
		}
		s, err := deps.Schedules.Get(in.ID, in.AlertType)
		if err != nil {
			log.Errorf("Summary API: get %s/%s: %v", in.ID, in.AlertType, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if s == nil {
			return nil, huma.Error404NotFound("schedule not found")
		}
		out := &summaryGetOutput{}
		out.Body.Status = "ok"
		out.Body.Schedule = toSummaryResponse(s)
		return out, nil
	})

	// DELETE /api/summaries/{id}/{alertType} — remove a schedule (idempotent).
	huma.Register(api, huma.Operation{
		OperationID: "delete-summary", Method: "DELETE", Path: "/summaries/{id}/{alertType}",
		Summary: "Delete a summary schedule", Tags: []string{"summaries"},
		Description: "Removes the user's summary schedule for the alert type — the API counterpart of `!summary <type> cleartime`. Matched alerts still buffer per the tracking rules but no longer fire on a timer.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *summaryAlertInput) (*statusOKOutput, error) {
		if deps.Schedules == nil {
			return nil, huma.Error503ServiceUnavailable(summaryDisabledMsg)
		}
		if in.ID == "" || in.AlertType == "" {
			return nil, huma.Error400BadRequest("missing path parameter")
		}
		if !isKnownSummaryAlertType(in.AlertType) {
			return nil, huma.Error400BadRequest(unknownAlertTypeMsg)
		}
		if err := deps.Schedules.Delete(in.ID, in.AlertType); err != nil {
			log.Errorf("Summary API: delete %s/%s: %v", in.ID, in.AlertType, err)
			return nil, huma.Error500InternalServerError("database error")
		}
		if deps.ReloadFunc != nil {
			deps.ReloadFunc()
		}
		out := &statusOKOutput{}
		out.Body.Status = "ok"
		return out, nil
	})

	// POST /api/summaries/{id}/{alertType}/trigger — flush the buffer now.
	huma.Register(api, huma.Operation{
		OperationID: "trigger-summary", Method: "POST", Path: "/summaries/{id}/{alertType}/trigger",
		Summary: "Trigger a summary dispatch", Tags: []string{"summaries"},
		Description: "Dispatches the user's buffered summary for the alert type immediately — the API counterpart of `!summary quest now`. Returns {status:\"ok\"} once the dispatch is queued.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *summaryAlertInput) (*statusOKOutput, error) {
		if deps.Dispatch == nil {
			return nil, huma.Error503ServiceUnavailable(summaryDisabledMsg)
		}
		if in.ID == "" || in.AlertType == "" {
			return nil, huma.Error400BadRequest("missing path parameter")
		}
		if !isKnownSummaryAlertType(in.AlertType) {
			return nil, huma.Error400BadRequest(unknownAlertTypeMsg)
		}
		deps.Dispatch(in.ID, in.AlertType)
		out := &statusOKOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}

// summaryListOutput is the typed body for GET /api/summaries/{id}: {status,
// schedules}.
type summaryListOutput struct {
	Body struct {
		Status    string                    `json:"status"`
		Schedules []summaryScheduleResponse `json:"schedules"`
	}
}

// summaryGetOutput is the typed body for GET /api/summaries/{id}/{alertType}:
// {status, schedule}.
type summaryGetOutput struct {
	Body struct {
		Status   string                  `json:"status"`
		Schedule summaryScheduleResponse `json:"schedule"`
	}
}

// summaryDisabledMsg / unknownAlertTypeMsg keep the legacy wording stable.
const (
	summaryDisabledMsg  = "summary feature is disabled (set tracking.quest_summary_enabled = true)"
	unknownAlertTypeMsg = "unknown alert type (currently only \"quest\" is supported for summary scheduling)"
)

// commandBody mirrors commandRequest but marks every field optional so huma's
// validation matches the legacy gin ShouldBindJSON behaviour (which treated all
// fields as optional and enforced text/user_id presence in handler logic). A
// dedicated struct keeps the shared commandRequest free of huma tags.
type commandBody struct {
	Text      string `json:"text,omitempty" required:"false"`
	UserID    string `json:"user_id,omitempty" required:"false"`
	UserName  string `json:"user_name,omitempty" required:"false"`
	Platform  string `json:"platform,omitempty" required:"false"`
	ChannelID string `json:"channel_id,omitempty" required:"false"`
	GuildID   string `json:"guild_id,omitempty" required:"false"`
	IsDM      bool   `json:"is_dm,omitempty" required:"false"`
}

// commandInput wraps the JSON command body for huma binding.
type commandInput struct {
	Body commandBody
}

// commandOutput is the typed body for POST /api/command: the same
// commandResponse shape the legacy handler returned ({status, replies}).
type commandOutput struct {
	Body commandResponse
}

// RegisterCommand registers POST /api/command on the shared huma instance,
// replacing the legacy gin HandleCommand. The success body is the same
// commandResponse shape; error paths surface as problem+json.
//
// deps *bot.BotDeps must have Parser populated (added by the caller on top of
// sharedBotDeps) so the endpoint can tokenise the raw text.
func RegisterCommand(api huma.API, deps *bot.BotDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "post-command", Method: "POST", Path: "/command",
		Summary: "Execute a bot command", Tags: []string{"command"},
		Description: "Runs a bot command line (e.g. \"track pikachu iv90\") through the same parser the Discord/Telegram bots use, on behalf of the user/channel described by the body. Returns {status, replies} with the messages the bot would have sent instead of delivering them.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *commandInput) (*commandOutput, error) {
		req := commandRequest{
			Text:      in.Body.Text,
			UserID:    in.Body.UserID,
			UserName:  in.Body.UserName,
			Platform:  in.Body.Platform,
			ChannelID: in.Body.ChannelID,
			GuildID:   in.Body.GuildID,
			IsDM:      in.Body.IsDM,
		}

		if req.Text == "" || req.UserID == "" {
			return nil, huma.Error400BadRequest("text and user_id are required")
		}

		if deps == nil || deps.Parser == nil {
			return nil, huma.Error500InternalServerError("command parser not configured")
		}

		// Parse commands from text
		parsed := deps.Parser.Parse(req.Text)
		if len(parsed) == 0 {
			return &commandOutput{Body: commandResponse{Status: "ok", Replies: nil}}, nil
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

		return &commandOutput{Body: commandResponse{Status: "ok", Replies: allReplies}}, nil
	})
}
