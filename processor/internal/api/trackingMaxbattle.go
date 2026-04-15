package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetMaxbattle returns the GET /api/tracking/maxbattle/{id} handler.
func HandleGetMaxbattle(deps *TrackingDeps) gin.HandlerFunc {
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

		maxbattles, err := db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get maxbattles: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(c, map[string]any{"maxbattle": result})
	}
}

// HandleDeleteMaxbattle returns the DELETE /api/tracking/maxbattle/{id}/byUid/{uid} handler.
func HandleDeleteMaxbattle(deps *TrackingDeps) gin.HandlerFunc {
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
			if err := db.DeleteByUID(deps.DB, "maxbattle", id, uid); err != nil {
				log.Errorf("Tracking API: delete maxbattle: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "maxbattle", id, uid); err != nil {
			log.Errorf("Tracking API: delete maxbattle: %s", err)
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
				message = tr.T("tracking.removed_prefix") + deps.RowText.MaxbattleRowText(tr, toMaxbattleTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// maxbattleInsertRequest represents a single maxbattle tracking row from the POST body.
type maxbattleInsertRequest struct {
	PokemonID flexInt  `json:"pokemon_id"`
	Level     flexInt  `json:"level"`
	Distance  flexInt  `json:"distance"`
	Template  any      `json:"template"`
	Clean     flexBool `json:"clean"`
	Form      flexInt  `json:"form"`
	Move      flexInt  `json:"move"`
	Gmax      flexBool `json:"gmax"`
	Evolution flexInt  `json:"evolution"`
	StationID *string  `json:"station_id"`
}

// HandleCreateMaxbattle returns the POST /api/tracking/maxbattle/{id} handler.
// The JS handler always inserts (no diff logic).
func HandleCreateMaxbattle(deps *TrackingDeps) gin.HandlerFunc {
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

		var insertReqs []maxbattleInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single maxbattleInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
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
			pokemonID := req.PokemonID.intValue(9000)

			level := 9000
			if pokemonID == 9000 {
				level = req.Level.intValue(9000)
				if level < 1 {
					trackingJSONError(c, http.StatusBadRequest, "Invalid level (must be specified if no pokemon_id)")
					return
				}
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

			clean := req.Clean.intValue(0)
			form := req.Form.intValue(0)
			move := req.Move.intValue(9000)
			gmax := req.Gmax.intValue(0)
			evolution := req.Evolution.intValue(9000)

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
			uid, err := deps.Tracking.Maxbattles.Insert(&insert[i])
			if err != nil {
				log.Errorf("Tracking API: insert maxbattle: %s", err)
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
			"message": responseMsg,
			"newUids": newUIDs,
			"insert":  len(insert),
		})
	}
}

// HandleBulkDeleteMaxbattle returns the POST /api/tracking/maxbattle/{id}/delete handler.
func HandleBulkDeleteMaxbattle(deps *TrackingDeps) gin.HandlerFunc {
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
		var existing []db.MaxbattleTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "maxbattle", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete maxbattles: %s", err)
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
					sb.WriteString(deps.RowText.MaxbattleRowText(tr, toMaxbattleTracking(&e)))
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

// toMaxbattleTracking converts a MaxbattleTrackingAPI to a MaxbattleTracking for rowtext generation.
func toMaxbattleTracking(api *db.MaxbattleTrackingAPI) *db.MaxbattleTracking {
	return &db.MaxbattleTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     api.Clean,
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
