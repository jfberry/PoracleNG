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

// HandleGetGym returns the GET /api/tracking/gym/{id} handler.
func HandleGetGym(deps *TrackingDeps) gin.HandlerFunc {
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

		gyms, err := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get gyms: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type gymWithDesc struct {
			db.GymTrackingAPI
			Description string `json:"description"`
		}

		result := make([]gymWithDesc, len(gyms))
		for i := range gyms {
			gt := toGymTracking(&gyms[i])
			result[i] = gymWithDesc{
				GymTrackingAPI: gyms[i],
				Description:    deps.RowText.GymRowText(tr, gt),
			}
		}

		trackingJSONOK(c, map[string]any{"gym": result})
	}
}

// HandleDeleteGym returns the DELETE /api/tracking/gym/{id}/byUid/{uid} handler.
func HandleDeleteGym(deps *TrackingDeps) gin.HandlerFunc {
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
			if err := db.DeleteByUID(deps.DB, "gym", id, uid); err != nil {
				log.Errorf("Tracking API: delete gym: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "gym", id, uid); err != nil {
			log.Errorf("Tracking API: delete gym: %s", err)
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
				message = tr.T("tracking.removed") + deps.RowText.GymRowText(tr, toGymTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// gymInsertRequest represents a single gym tracking row from the POST body.
type gymInsertRequest struct {
	Team          flexInt  `json:"team"`
	Distance      flexInt  `json:"distance"`
	Template      any      `json:"template"`
	Clean         flexBool `json:"clean"`
	SlotChanges   flexBool `json:"slot_changes"`
	BattleChanges flexBool `json:"battle_changes"`
	GymID         *string  `json:"gym_id"`
}

// HandleCreateGym returns the POST /api/tracking/gym/{id} handler.
func HandleCreateGym(deps *TrackingDeps) gin.HandlerFunc {
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

		var insertReqs []gymInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single gymInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []gymInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.GymTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			if !req.Team.isSet() {
				trackingJSONError(c, http.StatusBadRequest, "Invalid team")
				return
			}
			team := req.Team.intValue(0)
			if team < 0 || team > 4 {
				trackingJSONError(c, http.StatusBadRequest, "Invalid team")
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

			clean := db.IntBool(req.Clean.intValue(0) != 0)
			slotChanges := db.IntBool(req.SlotChanges.intValue(0) != 0)
			battleChanges := db.IntBool(req.BattleChanges.intValue(0) != 0)

			var gymID *string
			if req.GymID != nil {
				gymID = req.GymID
			}

			insert = append(insert, db.GymTrackingAPI{
				ID:            human.ID,
				ProfileNo:     profileNo,
				Ping:          "",
				Template:      template,
				Distance:      distance,
				Team:          team,
				Clean:         clean,
				SlotChanges:   slotChanges,
				BattleChanges: battleChanges,
				GymID:         gymID,
			})
		}

		tracked, err := deps.Tracking.Gyms.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing gyms: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.GymGetUID, store.GymSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				gt := toGymTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				gt := toGymTracking(&diff.Updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				gt := toGymTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
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
			if err := deps.Tracking.Gyms.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated gyms: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Gyms.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert gym: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Gyms.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert gym: %s", err)
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

// HandleBulkDeleteGym returns the POST /api/tracking/gym/{id}/delete handler.
func HandleBulkDeleteGym(deps *TrackingDeps) gin.HandlerFunc {
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
		var existing []db.GymTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "gym", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete gyms: %s", err)
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
					sb.WriteString(tr.T("tracking.removed"))
					sb.WriteString(deps.RowText.GymRowText(tr, toGymTracking(&e)))
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

// toGymTracking converts a GymTrackingAPI to a GymTracking for rowtext generation.
func toGymTracking(api *db.GymTrackingAPI) *db.GymTracking {
	return &db.GymTracking{
		ID:            api.ID,
		ProfileNo:     api.ProfileNo,
		Ping:          api.Ping,
		Clean:         bool(api.Clean),
		Distance:      api.Distance,
		Template:      api.Template,
		Team:          api.Team,
		SlotChanges:   bool(api.SlotChanges),
		BattleChanges: bool(api.BattleChanges),
		GymID:         api.GymID,
	}
}
