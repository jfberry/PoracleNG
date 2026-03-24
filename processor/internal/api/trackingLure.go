package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// validLureIDs defines the set of valid lure_id values.
var validLureIDs = map[int]bool{
	0: true, 501: true, 502: true, 503: true, 504: true, 505: true, 506: true,
}

// HandleGetLure returns the GET /api/tracking/lure/{id} handler.
func HandleGetLure(deps *TrackingDeps) http.HandlerFunc {
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

		lures, err := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get lures: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{"lure": result})
	}
}

// HandleDeleteLure returns the DELETE /api/tracking/lure/{id}/byUid/{uid} handler.
func HandleDeleteLure(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "lures", id, uid); err != nil {
			log.Errorf("Tracking API: delete lure: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// lureInsertRequest represents a single lure tracking row from the POST body.
type lureInsertRequest struct {
	LureID   *json.Number `json:"lure_id"`
	Distance *json.Number `json:"distance"`
	Template any          `json:"template"`
	Clean    *json.Number `json:"clean"`
}

// HandleCreateLure returns the POST /api/tracking/lure/{id} handler.
func HandleCreateLure(deps *TrackingDeps) http.HandlerFunc {
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

		// Parse body: single object or array
		var rawBody json.RawMessage
		if err := readJSONBody(r, &rawBody); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var insertReqs []lureInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single lureInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
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
			lureID := 0
			if req.LureID != nil {
				n, err := strconv.Atoi(string(*req.LureID))
				if err != nil {
					trackingJSONError(w, http.StatusBadRequest, "invalid lure_id value")
					return
				}
				lureID = n
			}
			if !validLureIDs[lureID] {
				trackingJSONError(w, http.StatusBadRequest, "Unrecognised lure_id value")
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

			clean := false
			if req.Clean != nil {
				n, _ := strconv.Atoi(string(*req.Clean))
				clean = n != 0
			}

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

		// Fetch existing tracking for diff
		tracked, err := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing lures: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Diff: categorize into unchanged, updates, and new inserts
		var updates []db.LureTrackingAPI
		var alreadyPresent []db.LureTrackingAPI

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

		// Build confirmation message
		var message string
		totalChanges := len(alreadyPresent) + len(updates) + len(insert)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range alreadyPresent {
				lt := toLureTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				lt := toLureTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
				sb.WriteByte('\n')
			}
			for i := range insert {
				lt := toLureTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.LureRowText(tr, lt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		// Delete rows being updated
		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "lures", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated lures: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		// Insert new and updated rows
		toInsert := make([]db.LureTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertLure(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert lure: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}

		reloadState(deps)

		// Send confirmation message
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

// HandleBulkDeleteLure returns the POST /api/tracking/lure/{id}/delete handler.
func HandleBulkDeleteLure(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		// Parse body: single uid or array of uids
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

		if err := db.DeleteByUIDs(deps.DB, "lures", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete lures: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
