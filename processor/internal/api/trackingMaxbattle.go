package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetMaxbattle returns the GET /api/tracking/maxbattle/{id} handler.
func HandleGetMaxbattle(deps *TrackingDeps) http.HandlerFunc {
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

		maxbattles, err := db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get maxbattles: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type maxbattleWithDesc struct {
			db.MaxbattleTrackingAPI
			Description string `json:"description"`
		}

		result := make([]maxbattleWithDesc, len(maxbattles))
		for i := range maxbattles {
			mt := toMaxbattleTracking(&maxbattles[i])
			result[i] = maxbattleWithDesc{
				MaxbattleTrackingAPI: maxbattles[i],
				Description:          deps.RowText.MaxbattleRowText(tr, mt),
			}
		}

		trackingJSONOK(w, map[string]any{"maxbattle": result})
	}
}

// HandleDeleteMaxbattle returns the DELETE /api/tracking/maxbattle/{id}/byUid/{uid} handler.
func HandleDeleteMaxbattle(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "maxbattle", id, uid); err != nil {
			log.Errorf("Tracking API: delete maxbattle: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// maxbattleInsertRequest represents a single maxbattle tracking row from the POST body.
type maxbattleInsertRequest struct {
	PokemonID *json.Number `json:"pokemon_id"`
	Level     *json.Number `json:"level"`
	Distance  *json.Number `json:"distance"`
	Template  any          `json:"template"`
	Clean     *json.Number `json:"clean"`
	Form      *json.Number `json:"form"`
	Move      *json.Number `json:"move"`
	Gmax      *json.Number `json:"gmax"`
	Evolution *json.Number `json:"evolution"`
	StationID *string      `json:"station_id"`
}

// HandleCreateMaxbattle returns the POST /api/tracking/maxbattle/{id} handler.
// The JS handler always inserts (no diff logic).
func HandleCreateMaxbattle(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []maxbattleInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single maxbattleInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []maxbattleInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.MaxbattleTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			pokemonID := 9000
			if req.PokemonID != nil {
				n, _ := strconv.Atoi(string(*req.PokemonID))
				pokemonID = n
			}

			level := 9000
			if pokemonID == 9000 {
				if req.Level != nil {
					n, _ := strconv.Atoi(string(*req.Level))
					level = n
				}
				if level < 1 {
					trackingJSONError(w, http.StatusBadRequest, "Invalid level (must be specified if no pokemon_id)")
					return
				}
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

			form := 0
			if req.Form != nil {
				n, _ := strconv.Atoi(string(*req.Form))
				form = n
			}

			move := 9000
			if req.Move != nil {
				n, _ := strconv.Atoi(string(*req.Move))
				move = n
			}

			gmax := 0
			if req.Gmax != nil {
				n, _ := strconv.Atoi(string(*req.Gmax))
				gmax = n
			}

			evolution := 9000
			if req.Evolution != nil {
				n, _ := strconv.Atoi(string(*req.Evolution))
				evolution = n
			}

			var stationID *string
			if req.StationID != nil && *req.StationID != "" {
				stationID = req.StationID
			}

			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:        human.ID,
				ProfileNo: profileNo,
				Ping:      "",
				Template:  template,
				Distance:  distance,
				Clean:     clean,
				PokemonID: pokemonID,
				Form:      form,
				Level:     level,
				Move:      move,
				Gmax:      gmax,
				Evolution: evolution,
				StationID: stationID,
			})
		}

		// Maxbattle JS: no diff logic, always inserts
		var message string
		if len(insert) > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range insert {
				mt := toMaxbattleTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.MaxbattleRowText(tr, mt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		var newUIDs []int64
		for i := range insert {
			uid, err := db.InsertMaxbattle(deps.DB, &insert[i])
			if err != nil {
				log.Errorf("Tracking API: insert maxbattle: %s", err)
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
			"message": responseMsg,
			"newUids": newUIDs,
			"insert":  len(insert),
		})
	}
}

// HandleBulkDeleteMaxbattle returns the POST /api/tracking/maxbattle/{id}/delete handler.
func HandleBulkDeleteMaxbattle(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "maxbattle", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete maxbattles: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toMaxbattleTracking converts a MaxbattleTrackingAPI to a MaxbattleTracking for rowtext generation.
func toMaxbattleTracking(api *db.MaxbattleTrackingAPI) *db.MaxbattleTracking {
	return &db.MaxbattleTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     bool(api.Clean),
		Distance:  api.Distance,
		Template:  api.Template,
		PokemonID: api.PokemonID,
		Form:      api.Form,
		Level:     api.Level,
		Move:      api.Move,
		Gmax:      api.Gmax,
		Evolution: api.Evolution,
		StationID: api.StationID,
	}
}
