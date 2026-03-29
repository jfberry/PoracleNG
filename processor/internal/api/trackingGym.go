package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetGym returns the GET /api/tracking/gym/{id} handler.
func HandleGetGym(deps *TrackingDeps) http.HandlerFunc {
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

		gyms, err := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get gyms: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{"gym": result})
	}
}

// HandleDeleteGym returns the DELETE /api/tracking/gym/{id}/byUid/{uid} handler.
func HandleDeleteGym(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "gym", id, uid); err != nil {
			log.Errorf("Tracking API: delete gym: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
func HandleCreateGym(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []gymInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single gymInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
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
				trackingJSONError(w, http.StatusBadRequest, "Invalid team")
				return
			}
			team := req.Team.intValue(0)
			if team < 0 || team > 4 {
				trackingJSONError(w, http.StatusBadRequest, "Invalid team")
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

		tracked, err := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing gyms: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.GymTrackingAPI
		var alreadyPresent []db.GymTrackingAPI

		for i := len(insert) - 1; i >= 0; i-- {
			for _, existing := range tracked {
				noMatch, isDup, uid, isUpd := diffTracking(&existing, &insert[i])
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
				gt := toGymTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				gt := toGymTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
				sb.WriteByte('\n')
			}
			for i := range insert {
				gt := toGymTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.GymRowText(tr, gt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "gym", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated gyms: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.GymTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertGym(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert gym: %s", err)
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

// HandleBulkDeleteGym returns the POST /api/tracking/gym/{id}/delete handler.
func HandleBulkDeleteGym(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "gym", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete gyms: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
