package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// validFortTypes defines the set of valid fort_type values.
var validFortTypes = map[string]bool{
	"pokestop": true, "gym": true, "everything": true,
}

// HandleGetFort returns the GET /api/tracking/fort/{id} handler.
func HandleGetFort(deps *TrackingDeps) gin.HandlerFunc {
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

		forts, err := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get forts: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type fortWithDesc struct {
			db.FortTrackingAPI
			Description string `json:"description"`
		}

		result := make([]fortWithDesc, len(forts))
		for i := range forts {
			ft := toFortTracking(&forts[i])
			result[i] = fortWithDesc{
				FortTrackingAPI: forts[i],
				Description:     deps.RowText.FortUpdateRowText(tr, ft),
			}
		}

		trackingJSONOK(c, map[string]any{"fort": result})
	}
}

// HandleDeleteFort returns the DELETE /api/tracking/fort/{id}/byUid/{uid} handler.
func HandleDeleteFort(deps *TrackingDeps) gin.HandlerFunc {
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
			if err := db.DeleteByUID(deps.DB, "forts", id, uid); err != nil {
				log.Errorf("Tracking API: delete fort: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "forts", id, uid); err != nil {
			log.Errorf("Tracking API: delete fort: %s", err)
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
				message = tr.T("tracking.removed_prefix") + deps.RowText.FortUpdateRowText(tr, toFortTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// fortInsertRequest represents a single fort tracking row from the POST body.
type fortInsertRequest struct {
	FortType     *string  `json:"fort_type"`
	Distance     flexInt  `json:"distance"`
	Template     any      `json:"template"`
	IncludeEmpty flexBool `json:"include_empty"`
	ChangeTypes  any      `json:"change_types"`
}

// HandleCreateFort returns the POST /api/tracking/fort/{id} handler.
func HandleCreateFort(deps *TrackingDeps) gin.HandlerFunc {
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

		var insertReqs []fortInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single fortInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []fortInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.FortTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			fortType := "everything"
			if req.FortType != nil && *req.FortType != "" {
				fortType = *req.FortType
			}
			if !validFortTypes[fortType] {
				trackingJSONError(c, http.StatusBadRequest, "Invalid fort_type: "+fortType+" (must be pokestop, gym, or everything)")
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

			includeEmpty := db.IntBool(req.IncludeEmpty.intValue(0) != 0)

			changeTypes := "[]"
			if req.ChangeTypes != nil {
				switch v := req.ChangeTypes.(type) {
				case string:
					changeTypes = v
				case []any:
					b, _ := json.Marshal(v)
					changeTypes = string(b)
				}
			}

			insert = append(insert, db.FortTrackingAPI{
				ID:           human.ID,
				ProfileNo:    profileNo,
				Ping:         "",
				Template:     template,
				Distance:     distance,
				FortType:     fortType,
				IncludeEmpty: includeEmpty,
				ChangeTypes:  changeTypes,
			})
		}

		tracked, err := deps.Tracking.Forts.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing forts: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.FortGetUID, store.FortSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				ft := toFortTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				ft := toFortTracking(&diff.Updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				ft := toFortTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
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
			if err := deps.Tracking.Forts.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated forts: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Forts.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert fort: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Forts.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert fort: %s", err)
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

// HandleBulkDeleteFort returns the POST /api/tracking/fort/{id}/delete handler.
func HandleBulkDeleteFort(deps *TrackingDeps) gin.HandlerFunc {
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

		human, profileNo, err := lookupHuman(deps, c)
		var existing []db.FortTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "forts", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete forts: %s", err)
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
					sb.WriteString(deps.RowText.FortUpdateRowText(tr, toFortTracking(&e)))
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

// toFortTracking converts a FortTrackingAPI to a FortTracking for rowtext generation.
func toFortTracking(api *db.FortTrackingAPI) *db.FortTracking {
	return &db.FortTracking{
		ID:           api.ID,
		ProfileNo:    api.ProfileNo,
		Ping:         api.Ping,
		Distance:     api.Distance,
		Template:     api.Template,
		FortType:     api.FortType,
		IncludeEmpty: bool(api.IncludeEmpty),
		ChangeTypes:  api.ChangeTypes,
	}
}
