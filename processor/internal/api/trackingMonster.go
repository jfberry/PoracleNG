package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// HandleGetMonster returns the GET /api/tracking/pokemon/{id} handler.
func HandleGetMonster(deps *TrackingDeps) http.HandlerFunc {
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

		monsters, err := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: get monsters: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		tr := translatorFor(deps, human)
		type monsterWithDesc struct {
			db.MonsterTrackingAPI
			Description string `json:"description"`
		}

		result := make([]monsterWithDesc, len(monsters))
		for i := range monsters {
			mt := toMonsterTracking(&monsters[i])
			result[i] = monsterWithDesc{
				MonsterTrackingAPI: monsters[i],
				Description:        deps.RowText.MonsterRowText(tr, mt),
			}
		}

		trackingJSONOK(w, map[string]any{"pokemon": result})
	}
}

// HandleDeleteMonster returns the DELETE /api/tracking/pokemon/{id}/byUid/{uid} handler.
func HandleDeleteMonster(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		uidStr := r.PathValue("uid")
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid uid")
			return
		}

		human, profileNo, err := lookupHuman(deps, r)
		if err != nil || human == nil {
			if err := db.DeleteByUID(deps.DB, "monsters", id, uid); err != nil {
				log.Errorf("Tracking API: delete monster: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
			reloadState(deps)
			trackingJSONOK(w, nil)
			return
		}

		existing, _ := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo)

		if err := db.DeleteByUID(deps.DB, "monsters", id, uid); err != nil {
			log.Errorf("Tracking API: delete monster: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		tr := translatorFor(deps, human)
		language := resolveLanguage(deps, human)
		silent := isSilent(r)
		var message string
		for _, e := range existing {
			if e.UID == uid {
				message = tr.T("tracking.removed") + deps.RowText.MonsterRowText(tr, toMonsterTracking(&e))
				break
			}
		}
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}
		trackingJSONOK(w, map[string]any{"message": message})
	}
}

// monsterInsertRequest represents a single monster tracking row from the POST body.
// The JS handler has a cleanRow function that applies defaults and validates.
type monsterInsertRequest struct {
	UID              flexInt  `json:"uid"`
	PokemonID        flexInt  `json:"pokemon_id"`
	ProfileNo        flexInt  `json:"profile_no"`
	Distance         flexInt  `json:"distance"`
	Template         any      `json:"template"`
	Clean            flexBool `json:"clean"`
	Form             flexInt  `json:"form"`
	MinIV            flexInt  `json:"min_iv"`
	MaxIV            flexInt  `json:"max_iv"`
	MinCP            flexInt  `json:"min_cp"`
	MaxCP            flexInt  `json:"max_cp"`
	MinLevel         flexInt  `json:"min_level"`
	MaxLevel         flexInt  `json:"max_level"`
	ATK              flexInt  `json:"atk"`
	DEF              flexInt  `json:"def"`
	STA              flexInt  `json:"sta"`
	MaxATK           flexInt  `json:"max_atk"`
	MaxDEF           flexInt  `json:"max_def"`
	MaxSTA           flexInt  `json:"max_sta"`
	Gender           flexInt  `json:"gender"`
	MinWeight        flexInt  `json:"min_weight"`
	MaxWeight        flexInt  `json:"max_weight"`
	MinTime          flexInt  `json:"min_time"`
	Rarity           flexInt  `json:"rarity"`
	MaxRarity        flexInt  `json:"max_rarity"`
	Size             flexInt  `json:"size"`
	MaxSize          flexInt  `json:"max_size"`
	PVPRankingLeague flexInt  `json:"pvp_ranking_league"`
	PVPRankingBest   flexInt  `json:"pvp_ranking_best"`
	PVPRankingWorst  flexInt  `json:"pvp_ranking_worst"`
	PVPRankingMinCP  flexInt  `json:"pvp_ranking_min_cp"`
	PVPRankingCap    flexInt  `json:"pvp_ranking_cap"`
}

// HandleCreateMonster returns the POST /api/tracking/pokemon/{id} handler.
// The JS handler splits rows by uid presence: rows with uid are updates, without are inserts.
// Then it diffs inserts against existing rows to find duplicates and auto-updates.
func HandleCreateMonster(deps *TrackingDeps) http.HandlerFunc {
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

		var insertReqs []monsterInsertRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &insertReqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single monsterInsertRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			insertReqs = []monsterInsertRequest{single}
		}

		defaultTemplate := deps.RowText.DefaultTemplateName
		if defaultTemplate == "" {
			defaultTemplate = "1"
		}

		// cleanRow: apply defaults and validate, matching the JS cleanRow function.
		cleanRow := func(req monsterInsertRequest) (db.MonsterTrackingAPI, error) {
			if !req.PokemonID.isSet() {
				return db.MonsterTrackingAPI{}, errPokemonIDRequired
			}

			pokemonID := req.PokemonID.intValue(0)

			distance := req.Distance.intValue(0)
			const maxDistanceDefault = 40000000 // circumference of Earth
			if distance > maxDistanceDefault {
				distance = maxDistanceDefault
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

			pNo := profileNo
			if req.ProfileNo.isSet() {
				pNo = req.ProfileNo.intValue(profileNo)
			}

			row := db.MonsterTrackingAPI{
				ID:               human.ID,
				ProfileNo:        pNo,
				Ping:             "",
				Template:         template,
				PokemonID:        pokemonID,
				Distance:         distance,
				MinIV:            req.MinIV.intValue(-1),
				MaxIV:            req.MaxIV.intValue(100),
				MinCP:            req.MinCP.intValue(0),
				MaxCP:            req.MaxCP.intValue(9000),
				MinLevel:         req.MinLevel.intValue(0),
				MaxLevel:         req.MaxLevel.intValue(55),
				ATK:              req.ATK.intValue(0),
				DEF:              req.DEF.intValue(0),
				STA:              req.STA.intValue(0),
				MaxATK:           req.MaxATK.intValue(15),
				MaxDEF:           req.MaxDEF.intValue(15),
				MaxSTA:           req.MaxSTA.intValue(15),
				Gender:           req.Gender.intValue(0),
				Form:             req.Form.intValue(0),
				Clean:            db.IntBool(req.Clean.intValue(0) != 0),
				MinWeight:        req.MinWeight.intValue(0),
				MaxWeight:        req.MaxWeight.intValue(9000000),
				MinTime:          req.MinTime.intValue(0),
				Rarity:           req.Rarity.intValue(-1),
				MaxRarity:        req.MaxRarity.intValue(6),
				Size:             req.Size.intValue(-1),
				MaxSize:          req.MaxSize.intValue(5),
				PVPRankingLeague: req.PVPRankingLeague.intValue(0),
				PVPRankingBest:   req.PVPRankingBest.intValue(1),
				PVPRankingWorst:  req.PVPRankingWorst.intValue(4096),
				PVPRankingMinCP:  req.PVPRankingMinCP.intValue(0),
				PVPRankingCap:    req.PVPRankingCap.intValue(0),
			}

			if req.UID.isSet() {
				row.UID = int64(req.UID.intValue(0))
			}

			return row, nil
		}

		// Split: rows with uid are explicit updates, without are inserts
		var insert []db.MonsterTrackingAPI
		var updates []db.MonsterTrackingAPI

		for _, req := range insertReqs {
			row, err := cleanRow(req)
			if err != nil {
				trackingJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			if req.UID.isSet() {
				updates = append(updates, row)
			} else {
				insert = append(insert, row)
			}
		}

		// Fetch existing for diff (only for new inserts)
		tracked, err := deps.Tracking.Monsters.SelectByIDProfile(human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing monsters: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		diff := store.DiffAndClassify(tracked, insert, store.MonsterGetUID, store.MonsterSetUID)

		// Merge: diff-classified updates go into the explicit updates slice
		updates = append(updates, diff.Updates...)

		// Build confirmation message
		var message string
		totalChanges := len(diff.AlreadyPresent) + len(updates) + len(diff.Inserts)
		if totalChanges > 50 {
			message = tr.Tf("tracking.bulk_changes",
				deps.Config.Discord.Prefix, tr.T("tracking.tracked"))
		} else {
			var sb strings.Builder
			for i := range diff.AlreadyPresent {
				mt := toMonsterTracking(&diff.AlreadyPresent[i])
				sb.WriteString(tr.T("tracking.unchanged"))
				sb.WriteString(deps.RowText.MonsterRowText(tr, mt))
				sb.WriteByte('\n')
			}
			for i := range updates {
				mt := toMonsterTracking(&updates[i])
				sb.WriteString(tr.T("tracking.updated"))
				sb.WriteString(deps.RowText.MonsterRowText(tr, mt))
				sb.WriteByte('\n')
			}
			for i := range diff.Inserts {
				mt := toMonsterTracking(&diff.Inserts[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.MonsterRowText(tr, mt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		// JS monster: inserts new rows, then updates existing by uid
		var newUIDs []int64

		for i := range diff.Inserts {
			uid, err := deps.Tracking.Monsters.Insert(&diff.Inserts[i])
			if err != nil {
				log.Errorf("Tracking API: insert monster: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, uid)
		}

		for i := range updates {
			if err := db.UpdateMonsterByUID(deps.DB, &updates[i]); err != nil {
				log.Errorf("Tracking API: update monster: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "database error")
				return
			}
			newUIDs = append(newUIDs, updates[i].UID)
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
			"alreadyPresent": len(diff.AlreadyPresent),
			"updates":        len(updates),
			"insert":         len(diff.Inserts),
		})
	}
}

// errPokemonIDRequired is returned when pokemon_id is missing from the request.
var errPokemonIDRequired = errMsg("Pokemon id must be specified")

type errMsg string

func (e errMsg) Error() string { return string(e) }

// HandleBulkDeleteMonster returns the POST /api/tracking/pokemon/{id}/delete handler.
func HandleBulkDeleteMonster(deps *TrackingDeps) http.HandlerFunc {
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

		human, profileNo, err := lookupHuman(deps, r)
		var existing []db.MonsterTrackingAPI
		if err == nil && human != nil {
			existing, _ = db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo)
		}

		if err := db.DeleteByUIDs(deps.DB, "monsters", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete monsters: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		var message string
		if human != nil && len(existing) > 0 {
			tr := translatorFor(deps, human)
			language := resolveLanguage(deps, human)
			silent := isSilent(r)
			uidSet := make(map[int64]bool, len(uids))
			for _, u := range uids {
				uidSet[u] = true
			}
			var sb strings.Builder
			for _, e := range existing {
				if uidSet[e.UID] {
					sb.WriteString(tr.T("tracking.removed"))
					sb.WriteString(deps.RowText.MonsterRowText(tr, toMonsterTracking(&e)))
					sb.WriteByte('\n')
				}
			}
			message = sb.String()
			if !silent && message != "" {
				sendConfirmation(deps, human, message, language)
			}
		}
		trackingJSONOK(w, map[string]any{"message": message})
	}
}

// toMonsterTracking converts a MonsterTrackingAPI to a MonsterTracking for rowtext generation.
func toMonsterTracking(api *db.MonsterTrackingAPI) *db.MonsterTracking {
	return &db.MonsterTracking{
		ID:               api.ID,
		ProfileNo:        api.ProfileNo,
		Ping:             api.Ping,
		Clean:            bool(api.Clean),
		Distance:         api.Distance,
		Template:         api.Template,
		PokemonID:        api.PokemonID,
		Form:             api.Form,
		MinIV:            api.MinIV,
		MaxIV:            api.MaxIV,
		MinCP:            api.MinCP,
		MaxCP:            api.MaxCP,
		MinLevel:         api.MinLevel,
		MaxLevel:         api.MaxLevel,
		ATK:              api.ATK,
		DEF:              api.DEF,
		STA:              api.STA,
		MaxATK:           api.MaxATK,
		MaxDEF:           api.MaxDEF,
		MaxSTA:           api.MaxSTA,
		Gender:           api.Gender,
		MinWeight:        api.MinWeight,
		MaxWeight:        api.MaxWeight,
		MinTime:          api.MinTime,
		Rarity:           api.Rarity,
		MaxRarity:        api.MaxRarity,
		Size:             api.Size,
		MaxSize:          api.MaxSize,
		PVPRankingLeague: api.PVPRankingLeague,
		PVPRankingBest:   api.PVPRankingBest,
		PVPRankingWorst:  api.PVPRankingWorst,
		PVPRankingMinCP:  api.PVPRankingMinCP,
		PVPRankingCap:    api.PVPRankingCap,
	}
}
