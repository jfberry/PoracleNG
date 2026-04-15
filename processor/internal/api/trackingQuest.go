package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// validRewardTypes defines the set of valid reward_type values.
var validRewardTypes = map[int]bool{
	2: true, 3: true, 4: true, 7: true, 12: true,
}

// HandleGetQuest returns the GET /api/tracking/quest/{id} handler.
func HandleGetQuest(deps *TrackingDeps) gin.HandlerFunc {
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

		quests, err := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get quests: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type questWithDesc struct {
			db.QuestTrackingAPI
			Description string `json:"description"`
		}

		result := make([]questWithDesc, len(quests))
		for i := range quests {
			qt := toQuestTracking(&quests[i])
			result[i] = questWithDesc{
				QuestTrackingAPI: quests[i],
				Description:      deps.RowText.QuestRowText(tr, qt),
			}
		}

		trackingJSONOK(c, map[string]any{"quest": result})
	}
}

// HandleDeleteQuest returns the DELETE /api/tracking/quest/{id}/byUid/{uid} handler.
func HandleDeleteQuest(deps *TrackingDeps) gin.HandlerFunc {
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
			if err := db.DeleteByUID(deps.DB, "quest", id, uid); err != nil {
				log.Errorf("Tracking API: delete quest: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "quest", id, uid); err != nil {
			log.Errorf("Tracking API: delete quest: %s", err)
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
				message = tr.T("tracking.removed_prefix") + deps.RowText.QuestRowText(tr, toQuestTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// questInsertRequest represents a single quest tracking row from the POST body.
type questInsertRequest struct {
	RewardType flexInt  `json:"reward_type"`
	Reward     flexInt  `json:"reward"`
	Distance   flexInt  `json:"distance"`
	Template   any      `json:"template"`
	Clean      flexBool `json:"clean"`
	Form       flexInt  `json:"form"`
	Shiny      flexBool `json:"shiny"`
	Amount     flexInt  `json:"amount"`
}

// HandleCreateQuest returns the POST /api/tracking/quest/{id} handler.
func HandleCreateQuest(deps *TrackingDeps) gin.HandlerFunc {
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

		var insertReqs []questInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single questInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []questInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.QuestTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			rewardType := req.RewardType.intValue(0)
			if !validRewardTypes[rewardType] {
				trackingJSONError(c, http.StatusBadRequest, "Unrecognised reward_type value")
				return
			}

			reward := req.Reward.intValue(0)
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
			form := req.Form.intValue(0)
			shiny := db.IntBool(req.Shiny.intValue(0) != 0)
			amount := req.Amount.intValue(0)

			insert = append(insert, db.QuestTrackingAPI{
				ID:         human.ID,
				ProfileNo:  profileNo,
				Ping:       "",
				Template:   template,
				Distance:   distance,
				Clean:      clean,
				RewardType: rewardType,
				Reward:     reward,
				Form:       form,
				Shiny:      shiny,
				Amount:     amount,
			})
		}

		tracked, err := deps.Tracking.Quests.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing quests: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.QuestGetUID, store.QuestSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				qt := toQuestTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				qt := toQuestTracking(&diff.Updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				qt := toQuestTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
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
			if err := deps.Tracking.Quests.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated quests: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Quests.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert quest: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Quests.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert quest: %s", err)
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

// HandleBulkDeleteQuest returns the POST /api/tracking/quest/{id}/delete handler.
func HandleBulkDeleteQuest(deps *TrackingDeps) gin.HandlerFunc {
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
		var existing []db.QuestTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "quest", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete quests: %s", err)
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
					sb.WriteString(deps.RowText.QuestRowText(tr, toQuestTracking(&e)))
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

// toQuestTracking converts a QuestTrackingAPI to a QuestTracking for rowtext generation.
func toQuestTracking(api *db.QuestTrackingAPI) *db.QuestTracking {
	return &db.QuestTracking{
		ID:         api.ID,
		ProfileNo:  api.ProfileNo,
		Ping:       api.Ping,
		Clean:      api.Clean,
		Distance:   api.Distance,
		Template:   api.Template,
		RewardType: api.RewardType,
		Reward:     api.Reward,
		Form:       api.Form,
		Shiny:      bool(api.Shiny),
		Amount:     api.Amount,
	}
}
