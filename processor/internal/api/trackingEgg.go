package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guregu/null/v6"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// HandleGetEgg returns the GET /api/tracking/egg/{id} handler.
func HandleGetEgg(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		human, profileNo, err := lookupHuman(deps, c)
		if err != nil {
			trackingJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		eggs, err := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get eggs: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type eggWithDesc struct {
			db.EggTrackingAPI
			Description string `json:"description"`
		}

		result := make([]eggWithDesc, len(eggs))
		for i := range eggs {
			et := toEggTracking(&eggs[i])
			result[i] = eggWithDesc{
				EggTrackingAPI: eggs[i],
				Description:    deps.RowText.EggRowText(tr, et),
			}
		}

		trackingJSONOK(c, map[string]any{"egg": result})
	}
}

// HandleDeleteEgg returns the DELETE /api/tracking/egg/{id}/byUid/{uid} handler.
func HandleDeleteEgg(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		uidStr := c.Param("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid uid")
			return
		}

		// Look up human + existing rows for row text
		human, profileNo, err := lookupHuman(deps, c)
		if err != nil || human == nil {
			// Fall back to simple delete without row text
			if err := db.DeleteByUID(deps.DB, "egg", id, uid); err != nil {
				log.Errorf("Tracking API: delete egg: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		// Select existing to find the row being deleted (for description)
		existing, _ := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "egg", id, uid); err != nil {
			log.Errorf("Tracking API: delete egg: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		// Generate removed row text
		tr := translatorFor(deps, human)
		language := resolveLanguage(deps, human)
		silent := isSilent(c)
		var message string
		for _, e := range existing {
			if e.UID == uid {
				message = tr.T("tracking.removed_prefix") + deps.RowText.EggRowText(tr, toEggTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// eggInsertRequest represents a single egg tracking row from the POST body.
type eggInsertRequest struct {
	Level       json.RawMessage `json:"level"`
	Distance    flexInt         `json:"distance"`
	Template    any             `json:"template"`
	Clean       flexBool        `json:"clean"`
	Team        flexInt         `json:"team"`
	Exclusive   flexBool        `json:"exclusive"`
	GymID       *string         `json:"gym_id"`
	RSVPChanges flexInt         `json:"rsvp_changes"`
}

// HandleCreateEgg returns the POST /api/tracking/egg/{id} handler.
func HandleCreateEgg(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		human, profileNo, err := lookupHuman(deps, c)
		if err != nil {
			trackingJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		language := resolveLanguage(deps, human)
		tr := translatorFor(deps, human)
		silent := isSilent(c)

		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var insertReqs []eggInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single eggInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []eggInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.EggTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			template := defaultTemplate
			if req.Template != nil {
				switch v := req.Template.(type) {
				case string:
					if v != "" {
						template = v
					}
				case float64:
					template = strconv.Itoa(int(v))
				case json.Number:
					template = string(v)
				}
			}

			distance := req.Distance.intValue(0)

			team := req.Team.intValue(4)
			if team < 0 || team > 4 {
				team = 4
			}

			clean := req.Clean.intValue(0)
			exclusive := db.IntBool(req.Exclusive.intValue(0) != 0)

			var gymID null.String
			if req.GymID != nil && *req.GymID != "" {
				gymID = null.StringFrom(*req.GymID)
			}

			rsvpChanges := req.RSVPChanges.intValue(0)
			if rsvpChanges < 0 || rsvpChanges > 2 {
				rsvpChanges = 0
			}

			// Level expansion
			levels := parseLevelArray(req.Level)
			for _, lvl := range levels {
				if lvl < 1 {
					trackingJSONError(c, http.StatusBadRequest, "Invalid level")
					return
				}

				insert = append(insert, db.EggTrackingAPI{
					ID:          human.ID,
					ProfileNo:   profileNo,
					Ping:        "",
					Template:    template,
					Distance:    distance,
					Team:        team,
					Clean:       clean,
					Exclusive:   exclusive,
					GymID:       gymID,
					RSVPChanges: rsvpChanges,
					Level:       lvl,
				})
			}
		}

		tracked, err := deps.Tracking.Eggs.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing eggs: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.EggGetUID, store.EggSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.EggRowText(tr, toEggTracking(&diff.AlreadyPresent[i])))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.EggRowText(tr, toEggTracking(&diff.Updates[i])))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.EggRowText(tr, toEggTracking(&diff.Inserts[i])))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		// Apply: delete updated UIDs, insert new + updated
		if len(diff.Updates) > 0 {
			uids := make([]int64, len(diff.Updates))
			for i := range diff.Updates {
				uids[i] = diff.Updates[i].UID
			}
			if err := deps.Tracking.Eggs.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated eggs: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Eggs.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert egg: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Eggs.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert egg: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}

		reloadState(deps)

		if !silent {
			sendConfirmation(deps, human, message, language)
		}

		responseMsg := message
		if silent {
			responseMsg = ""
		}

		trackingJSONOK(c, map[string]any{
			"message":        responseMsg,
			"newUids":        newUIDs,
			"alreadyPresent": len(diff.AlreadyPresent),
			"updates":        len(diff.Updates),
			"insert":         len(diff.Inserts),
		})
	}
}

// HandleBulkDeleteEgg returns the POST /api/tracking/egg/{id}/delete handler.
func HandleBulkDeleteEgg(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var uids []int64
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &uids); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single int64
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			uids = []int64{single}
		}

		// Look up human + existing rows for row text
		human, profileNo, err := lookupHuman(deps, c)
		var existing []db.EggTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "egg", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete eggs: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		// Generate removed row text
		var message string
		if human != nil && len(existing) > 0 {
			tr := translatorFor(deps, human)
			language := resolveLanguage(deps, human)
			silent := isSilent(c)
			uidSet := make(map[int64]bool, len(uids))
			for _, u := range uids {
				uidSet[u] = true
			}
			var sb strings.Builder
			for _, e := range existing {
				if uidSet[e.UID] {
					sb.WriteString(tr.T("tracking.removed_prefix"))
					sb.WriteString(deps.RowText.EggRowText(tr, toEggTracking(&e)))
					sb.WriteByte('\n')
				}
			}
			message = sb.String()
			if !silent && message != "" {
				sendConfirmation(deps, human, message, language)
			}
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// toEggTracking converts an EggTrackingAPI to an EggTracking for rowtext generation.
func toEggTracking(api *db.EggTrackingAPI) *db.EggTracking {
	return &db.EggTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       api.Clean,
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		GymID:       api.GymID.NullString,
		RSVPChanges: api.RSVPChanges,
	}
}
