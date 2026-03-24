package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
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

		if err := db.DeleteByUID(deps.DB, "monsters", id, uid); err != nil {
			log.Errorf("Tracking API: delete monster: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// monsterInsertRequest represents a single monster tracking row from the POST body.
// The JS handler has a cleanRow function that applies defaults and validates.
type monsterInsertRequest struct {
	UID             *json.Number `json:"uid"`
	PokemonID       *json.Number `json:"pokemon_id"`
	ProfileNo       *json.Number `json:"profile_no"`
	Distance        *json.Number `json:"distance"`
	Template        any          `json:"template"`
	Clean           *json.Number `json:"clean"`
	Form            *json.Number `json:"form"`
	MinIV           *json.Number `json:"min_iv"`
	MaxIV           *json.Number `json:"max_iv"`
	MinCP           *json.Number `json:"min_cp"`
	MaxCP           *json.Number `json:"max_cp"`
	MinLevel        *json.Number `json:"min_level"`
	MaxLevel        *json.Number `json:"max_level"`
	ATK             *json.Number `json:"atk"`
	DEF             *json.Number `json:"def"`
	STA             *json.Number `json:"sta"`
	MaxATK          *json.Number `json:"max_atk"`
	MaxDEF          *json.Number `json:"max_def"`
	MaxSTA          *json.Number `json:"max_sta"`
	Gender          *json.Number `json:"gender"`
	MinWeight       *json.Number `json:"min_weight"`
	MaxWeight       *json.Number `json:"max_weight"`
	MinTime         *json.Number `json:"min_time"`
	Rarity          *json.Number `json:"rarity"`
	MaxRarity       *json.Number `json:"max_rarity"`
	Size            *json.Number `json:"size"`
	MaxSize         *json.Number `json:"max_size"`
	PVPRankingLeague *json.Number `json:"pvp_ranking_league"`
	PVPRankingBest   *json.Number `json:"pvp_ranking_best"`
	PVPRankingWorst  *json.Number `json:"pvp_ranking_worst"`
	PVPRankingMinCP  *json.Number `json:"pvp_ranking_min_cp"`
	PVPRankingCap    *json.Number `json:"pvp_ranking_cap"`
}

func jsonNumInt(n *json.Number, def int) int {
	if n == nil {
		return def
	}
	v, err := strconv.Atoi(string(*n))
	if err != nil {
		return def
	}
	return v
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
			if req.PokemonID == nil {
				return db.MonsterTrackingAPI{}, errPokemonIDRequired
			}

			pokemonID := jsonNumInt(req.PokemonID, 0)

			distance := jsonNumInt(req.Distance, 0)
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
			if req.ProfileNo != nil {
				pNo = jsonNumInt(req.ProfileNo, profileNo)
			}

			row := db.MonsterTrackingAPI{
				ID:               human.ID,
				ProfileNo:        pNo,
				Ping:             "",
				Template:         template,
				PokemonID:        pokemonID,
				Distance:         distance,
				MinIV:            jsonNumInt(req.MinIV, -1),
				MaxIV:            jsonNumInt(req.MaxIV, 100),
				MinCP:            jsonNumInt(req.MinCP, 0),
				MaxCP:            jsonNumInt(req.MaxCP, 9000),
				MinLevel:         jsonNumInt(req.MinLevel, 0),
				MaxLevel:         jsonNumInt(req.MaxLevel, 55),
				ATK:              jsonNumInt(req.ATK, 0),
				DEF:              jsonNumInt(req.DEF, 0),
				STA:              jsonNumInt(req.STA, 0),
				MaxATK:           jsonNumInt(req.MaxATK, 15),
				MaxDEF:           jsonNumInt(req.MaxDEF, 15),
				MaxSTA:           jsonNumInt(req.MaxSTA, 15),
				Gender:           jsonNumInt(req.Gender, 0),
				Form:             jsonNumInt(req.Form, 0),
				Clean:            jsonNumInt(req.Clean, 0) != 0,
				MinWeight:        jsonNumInt(req.MinWeight, 0),
				MaxWeight:        jsonNumInt(req.MaxWeight, 9000000),
				MinTime:          jsonNumInt(req.MinTime, 0),
				Rarity:           jsonNumInt(req.Rarity, -1),
				MaxRarity:        jsonNumInt(req.MaxRarity, 6),
				Size:             jsonNumInt(req.Size, -1),
				MaxSize:          jsonNumInt(req.MaxSize, 5),
				PVPRankingLeague: jsonNumInt(req.PVPRankingLeague, 0),
				PVPRankingBest:   jsonNumInt(req.PVPRankingBest, 1),
				PVPRankingWorst:  jsonNumInt(req.PVPRankingWorst, 4096),
				PVPRankingMinCP:  jsonNumInt(req.PVPRankingMinCP, 0),
				PVPRankingCap:    jsonNumInt(req.PVPRankingCap, 0),
			}

			if req.UID != nil {
				row.UID = int64(jsonNumInt(req.UID, 0))
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
			if req.UID != nil {
				updates = append(updates, row)
			} else {
				insert = append(insert, row)
			}
		}

		// Fetch existing for diff (only for new inserts)
		tracked, err := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo)
		if err != nil {
			log.Errorf("Tracking API: select existing monsters: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		var alreadyPresent []db.MonsterTrackingAPI

		// Monster JS: filters by pokemon_id before diff, but uses generic diff with no match keys.
		// Since MonsterTrackingAPI has no diff:"match" fields, diffTracking will compare all
		// non-skipped fields. We pre-filter by pokemon_id to match the JS behavior.
		for i := len(insert) - 1; i >= 0; i-- {
			for _, existing := range tracked {
				if existing.PokemonID != insert[i].PokemonID {
					continue
				}
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
				mt := toMonsterTracking(&alreadyPresent[i])
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
			for i := range insert {
				mt := toMonsterTracking(&insert[i])
				sb.WriteString(tr.T("tracking.new"))
				sb.WriteString(deps.RowText.MonsterRowText(tr, mt))
				sb.WriteByte('\n')
			}
			message = sb.String()
		}

		// JS monster: inserts new rows, then updates existing by uid
		var newUIDs []int64

		for i := range insert {
			uid, err := db.InsertMonster(deps.DB, &insert[i])
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
			"alreadyPresent": len(alreadyPresent),
			"updates":        len(updates),
			"insert":         len(insert),
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

		if err := db.DeleteByUIDs(deps.DB, "monsters", id, uids); err != nil {
			log.Errorf("Tracking API: bulk delete monsters: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// toMonsterTracking converts a MonsterTrackingAPI to a MonsterTracking for rowtext generation.
func toMonsterTracking(api *db.MonsterTrackingAPI) *db.MonsterTracking {
	return &db.MonsterTracking{
		ID:               api.ID,
		ProfileNo:        api.ProfileNo,
		Ping:             api.Ping,
		Clean:            api.Clean,
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
