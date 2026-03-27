package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// validFortTypes defines the set of valid fort_type values.
var validFortTypes = map[string]bool{
	"pokestop": true, "gym": true, "everything": true,
}

// HandleGetFort returns the GET /api/tracking/fort/{id} handler.
func HandleGetFort(deps *TrackingDeps) http.HandlerFunc {
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

		forts, err := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get forts: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{"fort": result})
	}
}

// HandleDeleteFort returns the DELETE /api/tracking/fort/{id}/byUid/{uid} handler.
func HandleDeleteFort(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "forts", id, uid); err != nil {
			log.Errorf("Tracking API: delete fort: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// fortInsertRequest represents a single fort tracking row from the POST body.
type fortInsertRequest struct {
	FortType     *string `json:"fort_type"`
	Distance     *int    `json:"distance"`
	Template     any     `json:"template"`
	IncludeEmpty *bool   `json:"include_empty"`
	ChangeTypes  any     `json:"change_types"`
}

// HandleCreateFort returns the POST /api/tracking/fort/{id} handler.
func HandleCreateFort(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []fortInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single fortInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
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
				trackingJSONError(w, http.StatusBadRequest, "Invalid fort_type: "+fortType+" (must be pokestop, gym, or everything)")
				return
			}

			distance := 0
			if req.Distance != nil {
				distance = *req.Distance
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

			var includeEmpty db.IntBool
			if req.IncludeEmpty != nil {
				includeEmpty = db.IntBool(*req.IncludeEmpty)
			}

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

		tracked, err := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing forts: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.FortTrackingAPI
		var alreadyPresent []db.FortTrackingAPI

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
				ft := toFortTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
				sb.WriteByte('\n')
			}
			for i := range updates {
				ft := toFortTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
				sb.WriteByte('\n')
			}
			for i := range insert {
				ft := toFortTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.FortUpdateRowText(tr, ft))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "forts", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated forts: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.FortTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertFort(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert fort: %s", err)
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

// HandleBulkDeleteFort returns the POST /api/tracking/fort/{id}/delete handler.
func HandleBulkDeleteFort(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "forts", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete forts: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
