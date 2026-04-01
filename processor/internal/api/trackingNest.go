package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// HandleGetNest returns the GET /api/tracking/nest/{id} handler.
func HandleGetNest(deps *TrackingDeps) gin.HandlerFunc {
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

		nests, err := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get nests: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
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

		trackingJSONOK(c, map[string]any{"nest": result})
	}
}

// HandleDeleteNest returns the DELETE /api/tracking/nest/{id}/byUid/{uid} handler.
func HandleDeleteNest(deps *TrackingDeps) gin.HandlerFunc {
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
			if err := db.DeleteByUID(deps.DB, "nests", id, uid); err != nil {
				log.Errorf("Tracking API: delete nest: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(c, nil)
			return
		}

		existing, _ := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "nests", id, uid); err != nil {
			log.Errorf("Tracking API: delete nest: %s", err)
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
				message = tr.T("tracking.removed") + deps.RowText.NestRowText(tr, toNestTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// nestInsertRequest represents a single nest tracking row from the POST body.
type nestInsertRequest struct {
	PokemonID   flexInt  `json:"pokemon_id"`
	Distance    flexInt  `json:"distance"`
	Template    any      `json:"template"`
	Clean       flexBool `json:"clean"`
	MinSpawnAvg flexInt  `json:"min_spawn_avg"`
	Form        flexInt  `json:"form"`
}

// HandleCreateNest returns the POST /api/tracking/nest/{id} handler.
func HandleCreateNest(deps *TrackingDeps) gin.HandlerFunc {
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

		var insertReqs []nestInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single nestInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
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
			pokemonID := req.PokemonID.intValue(0)
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
			minSpawnAvg := req.MinSpawnAvg.intValue(0)
			form := req.Form.intValue(0)

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

		tracked, err := deps.Tracking.Nests.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing nests: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.NestGetUID, store.NestSetUID)

		// Build confirmation message before applying changes
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(diff.Updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				nt := toNestTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
				sb.WriteByte('\n')
			}
			for i := range diff.Updates {
				nt := toNestTracking(&diff.Updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				nt := toNestTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.NestRowText(tr, nt))
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
			if err := deps.Tracking.Nests.DeleteByUIDs(human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated nests: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
		}

		var newUIDs []int64
		for i := range diff.Inserts {
			uid, err := deps.Tracking.Nests.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert nest: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}
		for i := range diff.Updates {
			uid, err := deps.Tracking.Nests.Insert(&diff.Updates[i])
			if err != nil {
				log.Errorf("Tracking API: insert nest: %s", err)
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

// HandleBulkDeleteNest returns the POST /api/tracking/nest/{id}/delete handler.
func HandleBulkDeleteNest(deps *TrackingDeps) gin.HandlerFunc {
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
		var existing []db.NestTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "nests", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete nests: %s", err)
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
					sb.WriteString(tr.T("tracking.removed"))
					sb.WriteString(deps.RowText.NestRowText(tr, toNestTracking(&e)))
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

// toNestTracking converts a NestTrackingAPI to a NestTracking for rowtext generation.
func toNestTracking(api *db.NestTrackingAPI) *db.NestTracking {
	return &db.NestTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       bool(api.Clean),
		Distance:    api.Distance,
		Template:    api.Template,
		PokemonID:   api.PokemonID,
		MinSpawnAvg: api.MinSpawnAvg,
		Form:        api.Form,
	}
}
