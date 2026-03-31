package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/guregu/null/v6"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetEgg returns the GET /api/tracking/egg/{id} handler.
func HandleGetEgg(deps *TrackingDeps) http.HandlerFunc {
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

		eggs, err := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get eggs: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(w, map[string]any{"egg": result})
	}
}

// HandleDeleteEgg returns the DELETE /api/tracking/egg/{id}/byUid/{uid} handler.
func HandleDeleteEgg(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "egg", id, uid); err != nil {
			log.Errorf("Tracking API: delete egg: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
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
func HandleCreateEgg(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []eggInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single eggInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
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

			clean := db.IntBool(req.Clean.intValue(0) != 0)
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
					trackingJSONError(w, http.StatusBadRequest, "Invalid level")
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

		tracked, err := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing eggs: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.EggTrackingAPI
		var alreadyPresent []db.EggTrackingAPI

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
				et := toEggTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.EggRowText(tr, et))
				sb.WriteByte('\n')
			}
			for i := range updates {
				et := toEggTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.EggRowText(tr, et))
				sb.WriteByte('\n')
			}
			for i := range insert {
				et := toEggTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.EggRowText(tr, et))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "egg", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated eggs: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.EggTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertEgg(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert egg: %s", err)
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

// HandleBulkDeleteEgg returns the POST /api/tracking/egg/{id}/delete handler.
func HandleBulkDeleteEgg(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "egg", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete eggs: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toEggTracking converts an EggTrackingAPI to an EggTracking for rowtext generation.
func toEggTracking(api *db.EggTrackingAPI) *db.EggTracking {
	return &db.EggTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       bool(api.Clean),
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		GymID:       api.GymID.NullString,
		RSVPChanges: api.RSVPChanges,
	}
}
