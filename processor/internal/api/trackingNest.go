package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetNest returns the GET /api/tracking/nest/{id} handler.
func HandleGetNest(deps *TrackingDeps) http.HandlerFunc {
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

		nests, err := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get nests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type nestWithDesc struct {
			db.NestTrackingAPI
			Description string `json:"description"`
		}

		result := make([]nestWithDesc, len(nests))
		for i := range nests {
			nt := toNestTracking(&nests[i])
			result[i] = nestWithDesc{
				NestTrackingAPI: nests[i],
				Description:     deps.RowText.NestRowText(tr, nt),
			}
		}

		trackingJSONOK(w, map[string]any{"nest": result})
	}
}

// HandleDeleteNest returns the DELETE /api/tracking/nest/{id}/byUid/{uid} handler.
func HandleDeleteNest(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "nests", id, uid); err != nil {
			log.Errorf("Tracking API: delete nest: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// nestInsertRequest represents a single nest tracking row from the POST body.
type nestInsertRequest struct {
	PokemonID   *json.Number `json:"pokemon_id"`
	Distance    *json.Number `json:"distance"`
	Template    any          `json:"template"`
	Clean       *json.Number `json:"clean"`
	MinSpawnAvg *json.Number `json:"min_spawn_avg"`
	Form        *json.Number `json:"form"`
}

// HandleCreateNest returns the POST /api/tracking/nest/{id} handler.
func HandleCreateNest(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []nestInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single nestInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []nestInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		insert := make([]db.NestTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			pokemonID := 0
			if req.PokemonID != nil {
				n, _ := strconv.Atoi(string(*req.PokemonID))
				pokemonID = n
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

			minSpawnAvg := 0
			if req.MinSpawnAvg != nil {
				n, _ := strconv.Atoi(string(*req.MinSpawnAvg))
				minSpawnAvg = n
			}

			form := 0
			if req.Form != nil {
				n, _ := strconv.Atoi(string(*req.Form))
				form = n
			}

			insert = append(insert, db.NestTrackingAPI{
				ID:          human.ID,
				ProfileNo:   profileNo,
				Ping:        "",
				Template:    template,
				Distance:    distance,
				Clean:       clean,
				PokemonID:   pokemonID,
				MinSpawnAvg: minSpawnAvg,
				Form:        form,
			})
		}

		tracked, err := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing nests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.NestTrackingAPI
		var alreadyPresent []db.NestTrackingAPI

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
				nt := toNestTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				nt := toNestTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
				sb.WriteByte('\n')
			}
			for i := range insert {
				nt := toNestTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "nests", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated nests: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.NestTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertNest(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert nest: %s", err)
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

// HandleBulkDeleteNest returns the POST /api/tracking/nest/{id}/delete handler.
func HandleBulkDeleteNest(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "nests", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete nests: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toNestTracking converts a NestTrackingAPI to a NestTracking for rowtext generation.
func toNestTracking(api *db.NestTrackingAPI) *db.NestTracking {
	return &db.NestTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       api.Clean,
		Distance:    api.Distance,
		Template:    api.Template,
		PokemonID:   api.PokemonID,
		MinSpawnAvg: api.MinSpawnAvg,
		Form:        api.Form,
	}
}
