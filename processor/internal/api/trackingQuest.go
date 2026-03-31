package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// validRewardTypes defines the set of valid reward_type values.
var validRewardTypes = map[int]bool{
	2: true, 3: true, 4: true, 7: true, 12: true,
}

// HandleGetQuest returns the GET /api/tracking/quest/{id} handler.
func HandleGetQuest(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		human, profileNo, err := lookupHuman(deps, r)
		if err != nil {
			trackingJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		quests, err := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get quests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{"quest": result})
	}
}

// HandleDeleteQuest returns the DELETE /api/tracking/quest/{id}/byUid/{uid} handler.
func HandleDeleteQuest(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "quest", id, uid); err != nil {
			log.Errorf("Tracking API: delete quest: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
func HandleCreateQuest(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		human, profileNo, err := lookupHuman(deps, r)
		if err != nil {
			trackingJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		language := resolveLanguage(deps, human)
		tr := translatorFor(deps, human)
		silent := isSilent(r)

		var rawBody json.RawMessage
		if err := readJSONBody(r, &rawBody); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var insertReqs []questInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single questInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
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
				trackingJSONError(w, http.StatusBadRequest, "Unrecognised reward_type value")
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

			clean := db.IntBool(req.Clean.intValue(0) != 0)
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

		tracked, err := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing quests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.QuestTrackingAPI
		var alreadyPresent []db.QuestTrackingAPI

		for i := len(insert) - 1; i >= 0; i-- {
			for _, existing := range tracked {
				noMatch, isDup, uid, isUpd := DiffTracking(&existing, &insert[i])
				if noMatch {
					continue
				}
				if isDup {
					alreadyPresent = append(alreadyPresent, insert[i])
					insert = append(insert[:i], insert[i+1:]...)
					break
				}
				if isUpd {
					update := insert[i]
					update.UID = uid
					updates = append(updates, update)
					insert = append(insert[:i], insert[i+1:]...)
					break
				}
			}
		}

		var message string
		totalChanges := len(alreadyPresent) + len(updates) + len(insert)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range alreadyPresent {
				qt := toQuestTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				qt := toQuestTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
				sb.WriteByte('\n')
			}
			for i := range insert {
				qt := toQuestTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.QuestRowText(tr, qt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "quest", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated quests: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.QuestTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertQuest(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert quest: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{
			"message":        responseMsg,
			"newUids":        newUIDs,
			"alreadyPresent": len(alreadyPresent),
			"updates":        len(updates),
			"insert":         len(insert),
		})
	}
}

// HandleBulkDeleteQuest returns the POST /api/tracking/quest/{id}/delete handler.
func HandleBulkDeleteQuest(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		var rawBody json.RawMessage
		if err := readJSONBody(r, &rawBody); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var uids []int64
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &uids); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single int64
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			uids = []int64{single}
		}

		if err := db.DeleteByUIDs(deps.DB, "quest", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete quests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toQuestTracking converts a QuestTrackingAPI to a QuestTracking for rowtext generation.
func toQuestTracking(api *db.QuestTrackingAPI) *db.QuestTracking {
	return &db.QuestTracking{
		ID:         api.ID,
		ProfileNo:  api.ProfileNo,
		Ping:       api.Ping,
		Clean:      bool(api.Clean),
		Distance:   api.Distance,
		Template:   api.Template,
		RewardType: api.RewardType,
		Reward:     api.Reward,
		Form:       api.Form,
		Shiny:      bool(api.Shiny),
		Amount:     api.Amount,
	}
}
