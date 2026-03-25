package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetInvasion returns the GET /api/tracking/invasion/{id} handler.
func HandleGetInvasion(deps *TrackingDeps) http.HandlerFunc {
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

		invasions, err := db.SelectInvasionsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get invasions: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type invasionWithDesc struct {
			db.InvasionTrackingAPI
			Description string `json:"description"`
		}

		result := make([]invasionWithDesc, len(invasions))
		for i := range invasions {
			it := toInvasionTracking(&invasions[i])
			result[i] = invasionWithDesc{
				InvasionTrackingAPI: invasions[i],
				Description:         deps.RowText.InvasionRowText(tr, it),
			}
		}

		trackingJSONOK(w, map[string]any{"invasion": result})
	}
}

// HandleDeleteInvasion returns the DELETE /api/tracking/invasion/{id}/byUid/{uid} handler.
func HandleDeleteInvasion(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "invasion", id, uid); err != nil {
			log.Errorf("Tracking API: delete invasion: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// invasionInsertRequest represents a single invasion tracking row from the POST body.
type invasionInsertRequest struct {
	GruntType *string      `json:"grunt_type"`
	Distance  *json.Number `json:"distance"`
	Template  any          `json:"template"`
	Clean     *json.Number `json:"clean"`
	Gender    *json.Number `json:"gender"`
}

// HandleCreateInvasion returns the POST /api/tracking/invasion/{id} handler.
func HandleCreateInvasion(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []invasionInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single invasionInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []invasionInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.InvasionTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			if req.GruntType == nil || *req.GruntType == "" {
				trackingJSONError(w, http.StatusBadRequest, "Grunt type mandatory")
				return
			}

			distance := 0
			if req.Distance != nil {
				n, _ := strconv.Atoi(string(*req.Distance))
				distance = n
			}

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

			var clean db.IntBool
			if req.Clean != nil {
				n, _ := strconv.Atoi(string(*req.Clean))
				clean = db.IntBool(n != 0)
			}

			gender := 0
			if req.Gender != nil {
				n, _ := strconv.Atoi(string(*req.Gender))
				gender = n
			}

			insert = append(insert, db.InvasionTrackingAPI{
				ID:        human.ID,
				ProfileNo: profileNo,
				Ping:      "",
				Template:  template,
				Distance:  distance,
				Clean:     clean,
				Gender:    gender,
				GruntType: *req.GruntType,
			})
		}

		tracked, err := db.SelectInvasionsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing invasions: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.InvasionTrackingAPI
		var alreadyPresent []db.InvasionTrackingAPI

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
				it := toInvasionTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.InvasionRowText(tr, it))
				sb.WriteByte('\n')
			}
			for i := range updates {
				it := toInvasionTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.InvasionRowText(tr, it))
				sb.WriteByte('\n')
			}
			for i := range insert {
				it := toInvasionTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.InvasionRowText(tr, it))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "invasion", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated invasions: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.InvasionTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertInvasion(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert invasion: %s", err)
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

// HandleBulkDeleteInvasion returns the POST /api/tracking/invasion/{id}/delete handler.
func HandleBulkDeleteInvasion(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "invasion", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete invasions: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toInvasionTracking converts an InvasionTrackingAPI to an InvasionTracking for rowtext generation.
func toInvasionTracking(api *db.InvasionTrackingAPI) *db.InvasionTracking {
	return &db.InvasionTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     bool(api.Clean),
		Distance:  api.Distance,
		Template:  api.Template,
		Gender:    api.Gender,
		GruntType: api.GruntType,
	}
}
