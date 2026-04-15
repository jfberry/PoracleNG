package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// isTruthy returns true for "true", "1", "yes" (case-insensitive).
func isTruthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// isFalsy returns true for "false", "0", "no" (case-insensitive).
func isFalsy(s string) bool {
	switch strings.ToLower(s) {
	case "false", "0", "no":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Description wrapper types for each tracking type
// ---------------------------------------------------------------------------

type monsterAllDesc struct {
	db.MonsterTrackingAPI
	Description string `json:"description"`
}

type raidAllDesc struct {
	db.RaidTrackingAPI
	Description string `json:"description"`
}

type eggAllDesc struct {
	db.EggTrackingAPI
	Description string `json:"description"`
}

type questAllDesc struct {
	db.QuestTrackingAPI
	Description string `json:"description"`
}

type invasionAllDesc struct {
	db.InvasionTrackingAPI
	Description string `json:"description"`
}

type lureAllDesc struct {
	db.LureTrackingAPI
	Description string `json:"description"`
}

type nestAllDesc struct {
	db.NestTrackingAPI
	Description string `json:"description"`
}

type gymAllDesc struct {
	db.GymTrackingAPI
	Description string `json:"description"`
}

type maxbattleAllDesc struct {
	db.MaxbattleTrackingAPI
	Description string `json:"description"`
}

type fortAllDesc struct {
	db.FortTrackingAPI
	Description string `json:"description"`
}

// ---------------------------------------------------------------------------
// Description enrichment helpers
// ---------------------------------------------------------------------------

func enrichMonsters(deps *TrackingDeps, tr *i18n.Translator, rows []db.MonsterTrackingAPI) []monsterAllDesc {
	result := make([]monsterAllDesc, len(rows))
	for i := range rows {
		mt := toMonsterTracking(&rows[i])
		result[i] = monsterAllDesc{
			MonsterTrackingAPI: rows[i],
			Description:        deps.RowText.MonsterRowText(tr, mt),
		}
	}
	return result
}

func enrichRaids(deps *TrackingDeps, tr *i18n.Translator, rows []db.RaidTrackingAPI) []raidAllDesc {
	result := make([]raidAllDesc, len(rows))
	for i := range rows {
		rt := toRaidTracking(&rows[i])
		result[i] = raidAllDesc{
			RaidTrackingAPI: rows[i],
			Description:     deps.RowText.RaidRowText(tr, rt),
		}
	}
	return result
}

func enrichEggs(deps *TrackingDeps, tr *i18n.Translator, rows []db.EggTrackingAPI) []eggAllDesc {
	result := make([]eggAllDesc, len(rows))
	for i := range rows {
		et := toEggTracking(&rows[i])
		result[i] = eggAllDesc{
			EggTrackingAPI: rows[i],
			Description:    deps.RowText.EggRowText(tr, et),
		}
	}
	return result
}

func enrichQuests(deps *TrackingDeps, tr *i18n.Translator, rows []db.QuestTrackingAPI) []questAllDesc {
	result := make([]questAllDesc, len(rows))
	for i := range rows {
		qt := toQuestTracking(&rows[i])
		result[i] = questAllDesc{
			QuestTrackingAPI: rows[i],
			Description:      deps.RowText.QuestRowText(tr, qt),
		}
	}
	return result
}

func enrichInvasions(deps *TrackingDeps, tr *i18n.Translator, rows []db.InvasionTrackingAPI) []invasionAllDesc {
	result := make([]invasionAllDesc, len(rows))
	for i := range rows {
		it := toInvasionTracking(&rows[i])
		result[i] = invasionAllDesc{
			InvasionTrackingAPI: rows[i],
			Description:         deps.RowText.InvasionRowText(tr, it),
		}
	}
	return result
}

func enrichLures(deps *TrackingDeps, tr *i18n.Translator, rows []db.LureTrackingAPI) []lureAllDesc {
	result := make([]lureAllDesc, len(rows))
	for i := range rows {
		lt := toLureTracking(&rows[i])
		result[i] = lureAllDesc{
			LureTrackingAPI: rows[i],
			Description:     deps.RowText.LureRowText(tr, lt),
		}
	}
	return result
}

func enrichNests(deps *TrackingDeps, tr *i18n.Translator, rows []db.NestTrackingAPI) []nestAllDesc {
	result := make([]nestAllDesc, len(rows))
	for i := range rows {
		nt := toNestTracking(&rows[i])
		result[i] = nestAllDesc{
			NestTrackingAPI: rows[i],
			Description:     deps.RowText.NestRowText(tr, nt),
		}
	}
	return result
}

func enrichGyms(deps *TrackingDeps, tr *i18n.Translator, rows []db.GymTrackingAPI) []gymAllDesc {
	result := make([]gymAllDesc, len(rows))
	for i := range rows {
		gt := toGymTracking(&rows[i])
		result[i] = gymAllDesc{
			GymTrackingAPI: rows[i],
			Description:    deps.RowText.GymRowText(tr, gt),
		}
	}
	return result
}

func enrichMaxbattles(deps *TrackingDeps, tr *i18n.Translator, rows []db.MaxbattleTrackingAPI) []maxbattleAllDesc {
	result := make([]maxbattleAllDesc, len(rows))
	for i := range rows {
		mt := toMaxbattleTracking(&rows[i])
		result[i] = maxbattleAllDesc{
			MaxbattleTrackingAPI: rows[i],
			Description:          deps.RowText.MaxbattleRowText(tr, mt),
		}
	}
	return result
}

func enrichForts(deps *TrackingDeps, tr *i18n.Translator, rows []db.FortTrackingAPI) []fortAllDesc {
	result := make([]fortAllDesc, len(rows))
	for i := range rows {
		ft := toFortTracking(&rows[i])
		result[i] = fortAllDesc{
			FortTrackingAPI: rows[i],
			Description:     deps.RowText.FortUpdateRowText(tr, ft),
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// HandleGetAllTracking — GET /api/tracking/all/{id}
// Queries by user ID + current profile (existing behavior).
// Query param includeDescriptions: if "true"/"1"/"yes", adds descriptions.
// ---------------------------------------------------------------------------

// HandleGetAllTracking returns GET /api/tracking/all/{id} — all tracking for current profile.
func HandleGetAllTracking(deps *TrackingDeps) gin.HandlerFunc {
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

		// Fetch full human record for the response (includes lat/lon/area)
		humanFull, err := deps.Humans.Get(human.ID)
		if err != nil {
			log.Errorf("Tracking API: get full human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		wantDesc := isTruthy(c.Query("includeDescriptions"))
		var tr *i18n.Translator
		if wantDesc {
			tr = translatorFor(deps, human)
		}

		result := map[string]any{
			"human": humanToResponse(humanFull),
		}

		if pokemon, err := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["pokemon"] = enrichMonsters(deps, tr, pokemon)
			} else {
				result["pokemon"] = pokemon
			}
		} else {
			log.Warnf("Tracking API: get all monsters: %s", err)
		}
		if raid, err := db.SelectRaidsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["raid"] = enrichRaids(deps, tr, raid)
			} else {
				result["raid"] = raid
			}
		} else {
			log.Warnf("Tracking API: get all raids: %s", err)
		}
		if egg, err := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["egg"] = enrichEggs(deps, tr, egg)
			} else {
				result["egg"] = egg
			}
		} else {
			log.Warnf("Tracking API: get all eggs: %s", err)
		}
		if quest, err := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["quest"] = enrichQuests(deps, tr, quest)
			} else {
				result["quest"] = quest
			}
		} else {
			log.Warnf("Tracking API: get all quests: %s", err)
		}
		if invasion, err := db.SelectInvasionsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["invasion"] = enrichInvasions(deps, tr, invasion)
			} else {
				result["invasion"] = invasion
			}
		} else {
			log.Warnf("Tracking API: get all invasions: %s", err)
		}
		if lure, err := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["lure"] = enrichLures(deps, tr, lure)
			} else {
				result["lure"] = lure
			}
		} else {
			log.Warnf("Tracking API: get all lures: %s", err)
		}
		if nest, err := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["nest"] = enrichNests(deps, tr, nest)
			} else {
				result["nest"] = nest
			}
		} else {
			log.Warnf("Tracking API: get all nests: %s", err)
		}
		if gym, err := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["gym"] = enrichGyms(deps, tr, gym)
			} else {
				result["gym"] = gym
			}
		} else {
			log.Warnf("Tracking API: get all gyms: %s", err)
		}
		if maxbattle, err := db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["maxbattle"] = enrichMaxbattles(deps, tr, maxbattle)
			} else {
				result["maxbattle"] = maxbattle
			}
		} else {
			log.Warnf("Tracking API: get all maxbattles: %s", err)
		}
		if fort, err := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			if wantDesc {
				result["fort"] = enrichForts(deps, tr, fort)
			} else {
				result["fort"] = fort
			}
		} else {
			log.Warnf("Tracking API: get all forts: %s", err)
		}

		trackingJSONOK(c, result)
	}
}

// ---------------------------------------------------------------------------
// HandleGetAllProfilesTracking — GET /api/tracking/allProfiles/{id}
// Queries by user ID only (ALL profiles, no profile_no filter).
// Query param includeDescriptions: default TRUE (backward compatible).
// If "false"/"0"/"no", descriptions are skipped.
// Also returns "profile" list and "human" (full record).
// ---------------------------------------------------------------------------

// HandleGetAllProfilesTracking returns GET /api/tracking/allProfiles/{id}.
func HandleGetAllProfilesTracking(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		humanFull, err := deps.Humans.Get(id)
		if err != nil {
			trackingJSONError(c, http.StatusInternalServerError, fmt.Sprintf("lookup human: %s", err))
			return
		}
		if humanFull == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		// Default: descriptions enabled (backward compatible with JS behavior).
		wantDesc := true
		if descParam := c.Query("includeDescriptions"); descParam != "" && isFalsy(descParam) {
			wantDesc = false
		}

		// Resolve translator for descriptions.
		var tr *i18n.Translator
		if wantDesc {
			lang := humanFull.Language
			if lang == "" {
				lang = deps.Config.General.Locale
			}
			tr = deps.Translations.For(lang)
		}

		result := map[string]any{
			"human": humanToResponse(humanFull),
		}

		// Profiles
		if profiles, err := db.SelectProfiles(deps.DB, id); err == nil {
			result["profile"] = profiles
		} else {
			log.Warnf("Tracking API: get all profiles: %s", err)
		}

		// All tracking types — no profile filter
		if pokemon, err := db.SelectMonstersByID(deps.DB, id); err == nil {
			if wantDesc {
				result["pokemon"] = enrichMonsters(deps, tr, pokemon)
			} else {
				result["pokemon"] = pokemon
			}
		} else {
			log.Warnf("Tracking API: get allProfiles monsters: %s", err)
		}
		if raid, err := db.SelectRaidsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["raid"] = enrichRaids(deps, tr, raid)
			} else {
				result["raid"] = raid
			}
		} else {
			log.Warnf("Tracking API: get allProfiles raids: %s", err)
		}
		if egg, err := db.SelectEggsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["egg"] = enrichEggs(deps, tr, egg)
			} else {
				result["egg"] = egg
			}
		} else {
			log.Warnf("Tracking API: get allProfiles eggs: %s", err)
		}
		if quest, err := db.SelectQuestsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["quest"] = enrichQuests(deps, tr, quest)
			} else {
				result["quest"] = quest
			}
		} else {
			log.Warnf("Tracking API: get allProfiles quests: %s", err)
		}
		if invasion, err := db.SelectInvasionsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["invasion"] = enrichInvasions(deps, tr, invasion)
			} else {
				result["invasion"] = invasion
			}
		} else {
			log.Warnf("Tracking API: get allProfiles invasions: %s", err)
		}
		if lure, err := db.SelectLuresByID(deps.DB, id); err == nil {
			if wantDesc {
				result["lure"] = enrichLures(deps, tr, lure)
			} else {
				result["lure"] = lure
			}
		} else {
			log.Warnf("Tracking API: get allProfiles lures: %s", err)
		}
		if nest, err := db.SelectNestsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["nest"] = enrichNests(deps, tr, nest)
			} else {
				result["nest"] = nest
			}
		} else {
			log.Warnf("Tracking API: get allProfiles nests: %s", err)
		}
		if gym, err := db.SelectGymsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["gym"] = enrichGyms(deps, tr, gym)
			} else {
				result["gym"] = gym
			}
		} else {
			log.Warnf("Tracking API: get allProfiles gyms: %s", err)
		}
		if maxbattle, err := db.SelectMaxbattlesByID(deps.DB, id); err == nil {
			if wantDesc {
				result["maxbattle"] = enrichMaxbattles(deps, tr, maxbattle)
			} else {
				result["maxbattle"] = maxbattle
			}
		} else {
			log.Warnf("Tracking API: get allProfiles maxbattles: %s", err)
		}
		if fort, err := db.SelectFortsByID(deps.DB, id); err == nil {
			if wantDesc {
				result["fort"] = enrichForts(deps, tr, fort)
			} else {
				result["fort"] = fort
			}
		} else {
			log.Warnf("Tracking API: get allProfiles forts: %s", err)
		}

		trackingJSONOK(c, result)
	}
}
