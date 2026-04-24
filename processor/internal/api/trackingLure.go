package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// validLureIDs defines the set of valid lure_id values.
var validLureIDs = map[int]bool{
	0: true, 501: true, 502: true, 503: true, 504: true, 505: true, 506: true,
}

// HandleGetLure returns the GET /api/tracking/lure/{id} handler.
func HandleGetLure(deps *TrackingDeps) gin.HandlerFunc {
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

		lures, err := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get lures: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		// Add description field using rowtext
		tr := translatorFor(deps, human)
		type lureWithDesc struct {
			db.LureTrackingAPI
			Description string `json:"description"`
		}

		result := make([]lureWithDesc, len(lures))
		for i := range lures {
			lt := &db.LureTracking{
				ID:        lures[i].ID,
				ProfileNo: lures[i].ProfileNo,
				Ping:      lures[i].Ping,
				Clean:     lures[i].Clean,
				Distance:  lures[i].Distance,
				Template:  lures[i].Template,
				LureID:    lures[i].LureID,
			}
			result[i] = lureWithDesc{
				LureTrackingAPI: lures[i],
				Description:     deps.RowText.LureRowText(tr, lt),
			}
		}

		trackingJSONOK(c, map[string]any{"lure": result})
	}
}

// HandleDeleteLure returns the DELETE /api/tracking/lure/{id}/byUid/{uid} handler.
func HandleDeleteLure(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		uidStr := c.Param("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid uid")
			return
		}

		human, profileNo, err := lookupHuman(deps, c)
		if err != nil || human == nil {
			if err := db.DeleteByUID(deps.DB, "lures", id, uid); err != nil {
				log.Errorf("Tracking API: delete lure: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "lures", id, uid); err != nil {
			log.Errorf("Tracking API: delete lure: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		tr := translatorFor(deps, human)
		language := resolveLanguage(deps, human)
		silent := isSilent(c)
		var message string
		for _, e := range existing {
			if e.UID == uid {
				message = tr.T("tracking.removed_prefix") + deps.RowText.LureRowText(tr, toLureTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// lureInsertRequest represents a single lure tracking row from the POST body.
type lureInsertRequest struct {
	LureID   flexInt  `json:"lure_id"`
	Distance flexInt  `json:"distance"`
	Template any      `json:"template"`
	Clean    flexBool `json:"clean"`
}

// HandleCreateLure returns the POST /api/tracking/lure/{id} handler.
func HandleCreateLure(deps *TrackingDeps) gin.HandlerFunc {
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

		// Parse body: single object or array
		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var insertReqs []lureInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single lureInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []lureInsertRequest{single}
		}

		// Default template name
		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		// Build normalized insert rows
		insert := make([]db.LureTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			lureID := req.LureID.intValue(0)
			if !validLureIDs[lureID] {
				trackingJSONError(c, http.StatusBadRequest, "Unrecognised lure_id value")
				return
			}

			distance := req.Distance.intValue(0)

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

			clean := req.Clean.intValue(0)

			insert = append(insert, db.LureTrackingAPI{
				ID:        human.ID,
				ProfileNo: profileNo,
				Ping:      "",
				Template:  template,
				Distance:  distance,
				Clean:     clean,
				LureID:    lureID,
			})
		}

		tracked, err := deps.Tracking.Lures.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing lures: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.LureGetUID, store.LureSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				bot.CommandPrefixForType(deps.Config, human.Type), tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				lt := toLureTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				lt := toLureTracking(&diff.Updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				lt := toLureTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
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
			if err := deps.Tracking.Lures.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated lures: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Lures.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert lure: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Lures.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert lure: %s", err)
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

// HandleBulkDeleteLure returns the POST /api/tracking/lure/{id}/delete handler.
func HandleBulkDeleteLure(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// Parse body: single uid or array of uids
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

		human, profileNo, err := lookupHuman(deps, c)
		var existing []db.LureTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "lures", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete lures: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

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
					sb.WriteString(deps.RowText.LureRowText(tr, toLureTracking(&e)))
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

// toLureTracking converts a LureTrackingAPI to a LureTracking for rowtext generation.
func toLureTracking(api *db.LureTrackingAPI) *db.LureTracking {
	return &db.LureTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     api.Clean,
		Distance:  api.Distance,
		Template:  api.Template,
		LureID:    api.LureID,
	}
}
