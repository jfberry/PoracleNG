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

// HandleGetRaid returns the GET /api/tracking/raid/{id} handler.
func HandleGetRaid(deps *TrackingDeps) http.HandlerFunc {
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

		raids, err := db.SelectRaidsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get raids: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type raidWithDesc struct {
			db.RaidTrackingAPI
			Description string `json:"description"`
		}

		result := make([]raidWithDesc, len(raids))
		for i := range raids {
			rt := toRaidTracking(&raids[i])
			result[i] = raidWithDesc{
				RaidTrackingAPI: raids[i],
				Description:     deps.RowText.RaidRowText(tr, rt),
			}
		}

		trackingJSONOK(w, map[string]any{"raid": result})
	}
}

// HandleDeleteRaid returns the DELETE /api/tracking/raid/{id}/byUid/{uid} handler.
func HandleDeleteRaid(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		if err := db.DeleteByUID(deps.DB, "raid", id, uid); err != nil {
			log.Errorf("Tracking API: delete raid: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// raidInsertRequest represents a single raid tracking row from the POST body.
// Supports pokemon_form array expansion and level array expansion.
type raidInsertRequest struct {
	PokemonID   flexInt          `json:"pokemon_id"`
	PokemonForm []pokemonFormPair `json:"pokemon_form"`
	Level       json.RawMessage  `json:"level"`
	Distance    flexInt          `json:"distance"`
	Template    any              `json:"template"`
	Clean       flexBool         `json:"clean"`
	Team        flexInt          `json:"team"`
	Exclusive   flexBool         `json:"exclusive"`
	Form        flexInt          `json:"form"`
	Move        flexInt          `json:"move"`
	Evolution   flexInt          `json:"evolution"`
	GymID       *string          `json:"gym_id"`
	RSVPChanges flexInt          `json:"rsvp_changes"`
}

type pokemonFormPair struct {
	PokemonID int `json:"pokemon_id"`
	Form      int `json:"form"`
}

// HandleCreateRaid returns the POST /api/tracking/raid/{id} handler.
func HandleCreateRaid(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []raidInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single raidInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []raidInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		// Helper to build common fields from a request row
		buildRaidCommon := func(req raidInsertRequest) (template string, distance int, team int, clean db.IntBool, exclusive db.IntBool, move int, evolution int, gymID null.String, rsvpChanges int) {
			template = defaultTemplate
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

			distance = req.Distance.intValue(0)

			team = req.Team.intValue(4)
			if team < 0 || team > 4 {
				team = 4
			}

			clean = db.IntBool(req.Clean.intValue(0) != 0)
			exclusive = db.IntBool(req.Exclusive.intValue(0) != 0)
			move = req.Move.intValue(9000)
			evolution = req.Evolution.intValue(9000)

			if req.GymID != nil && *req.GymID != "" {
				gymID = null.StringFrom(*req.GymID)
			}

			n := req.RSVPChanges.intValue(0)
			if n >= 0 && n <= 2 {
				rsvpChanges = n
			}

			return
		}

		insert := make([]db.RaidTrackingAPI, 0, len(insertReqs))
		for _, req := range insertReqs {
			tmpl, dist, team, clean, excl, move, evo, gymID, rsvp := buildRaidCommon(req)

			// pokemon_form expansion
			if len(req.PokemonForm) > 0 {
				for _, pf := range req.PokemonForm {
					insert = append(insert, db.RaidTrackingAPI{
						ID:          human.ID,
						ProfileNo:   profileNo,
						Ping:        "",
						Template:    tmpl,
						Distance:    dist,
						Team:        team,
						Clean:       clean,
						Exclusive:   excl,
						Move:        move,
						Evolution:   evo,
						GymID:       gymID,
						RSVPChanges: rsvp,
						PokemonID:   pf.PokemonID,
						Form:        pf.Form,
						Level:       9000,
					})
				}
				continue
			}

			// Level expansion
			levels := parseLevelArray(req.Level)

			pokemonID := req.PokemonID.intValue(9000)
			form := req.Form.intValue(0)

			for _, lvl := range levels {
				level := 9000
				if pokemonID == 9000 {
					level = lvl
					if level < 1 {
						trackingJSONError(w, http.StatusBadRequest, "Invalid level (must be specified if no pokemon_id)")
						return
					}
				}

				insert = append(insert, db.RaidTrackingAPI{
					ID:          human.ID,
					ProfileNo:   profileNo,
					Ping:        "",
					Template:    tmpl,
					Distance:    dist,
					Team:        team,
					Clean:       clean,
					Exclusive:   excl,
					Move:        move,
					Evolution:   evo,
					GymID:       gymID,
					RSVPChanges: rsvp,
					PokemonID:   pokemonID,
					Form:        form,
					Level:       level,
				})
			}
		}

		tracked, err := db.SelectRaidsByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing raids: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var updates []db.RaidTrackingAPI
		var alreadyPresent []db.RaidTrackingAPI

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
				rt := toRaidTracking(&alreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.RaidRowText(tr, rt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				rt := toRaidTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.RaidRowText(tr, rt))
				sb.WriteByte('\n')
			}
			for i := range insert {
				rt := toRaidTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.RaidRowText(tr, rt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		if len(updates) > 0 {
			uids := make([]int64, len(updates))
			for i, u := range updates {
				uids[i] = u.UID
			}
			if err := db.DeleteByUIDs(deps.DB, "raid", human.ID, uids); err != nil {
				log.Errorf("Tracking API: delete updated raids: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		toInsert := make([]db.RaidTrackingAPI, 0, len(insert)+len(updates))
		toInsert = append(toInsert, insert...)
		toInsert = append(toInsert, updates...)

		var newUIDs []int64
		for i := range toInsert {
			uid, err := db.InsertRaid(deps.DB, &toInsert[i])
			if err != nil {
				log.Errorf("Tracking API: insert raid: %s", err)
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

// HandleBulkDeleteRaid returns the POST /api/tracking/raid/{id}/delete handler.
func HandleBulkDeleteRaid(deps *TrackingDeps) http.HandlerFunc {
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

		if err := db.DeleteByUIDs(deps.DB, "raid", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete raids: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// parseLevelArray parses a JSON value that can be a single int or an array of ints.
func parseLevelArray(raw json.RawMessage) []int {
	if len(raw) == 0 {
		return []int{0}
	}
	// Try array first
	var arr []int
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Try single int
	var single int
	if err := json.Unmarshal(raw, &single); err == nil {
		return []int{single}
	}
	return []int{0}
}

// toRaidTracking converts a RaidTrackingAPI to a RaidTracking for rowtext generation.
func toRaidTracking(api *db.RaidTrackingAPI) *db.RaidTracking {
	return &db.RaidTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       bool(api.Clean),
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		PokemonID:   api.PokemonID,
		Form:        api.Form,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		Move:        api.Move,
		Evolution:   api.Evolution,
		GymID:       api.GymID.NullString,
		RSVPChanges: api.RSVPChanges,
	}
}
