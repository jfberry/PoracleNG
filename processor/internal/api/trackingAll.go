package api

import (
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetAllTracking returns GET /api/tracking/all/{id} — all tracking for current profile.
func HandleGetAllTracking(deps *TrackingDeps) http.HandlerFunc {
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

		result := map[string]any{
			"human": human,
		}

		if pokemon, err := db.SelectMonstersByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["pokemon"] = pokemon
		} else {
			log.Warnf("Tracking API: get all monsters: %s", err)
		}
		if raid, err := db.SelectRaidsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["raid"] = raid
		} else {
			log.Warnf("Tracking API: get all raids: %s", err)
		}
		if egg, err := db.SelectEggsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["egg"] = egg
		} else {
			log.Warnf("Tracking API: get all eggs: %s", err)
		}
		if quest, err := db.SelectQuestsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["quest"] = quest
		} else {
			log.Warnf("Tracking API: get all quests: %s", err)
		}
		if invasion, err := db.SelectInvasionsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["invasion"] = invasion
		} else {
			log.Warnf("Tracking API: get all invasions: %s", err)
		}
		if lure, err := db.SelectLuresByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["lure"] = lure
		} else {
			log.Warnf("Tracking API: get all lures: %s", err)
		}
		if nest, err := db.SelectNestsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["nest"] = nest
		} else {
			log.Warnf("Tracking API: get all nests: %s", err)
		}
		if gym, err := db.SelectGymsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["gym"] = gym
		} else {
			log.Warnf("Tracking API: get all gyms: %s", err)
		}
		if maxbattle, err := db.SelectMaxbattlesByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["maxbattle"] = maxbattle
		} else {
			log.Warnf("Tracking API: get all maxbattles: %s", err)
		}
		if fort, err := db.SelectFortsByIDProfile(deps.DB, human.ID, profileNo); err == nil {
			result["fort"] = fort
		} else {
			log.Warnf("Tracking API: get all forts: %s", err)
		}

		trackingJSONOK(w, result)
	}
}
