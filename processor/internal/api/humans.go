package api

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetOneHuman returns the GET /api/humans/one/{id} handler.
func HandleGetOneHuman(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHumanFull(deps.DB, id)
		if err != nil {
			log.Errorf("Humans API: get human: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		trackingJSONOK(w, map[string]any{"human": human})
	}
}

// HandleStartHuman returns the POST /api/humans/{id}/start handler.
func HandleStartHuman(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Humans API: lookup human for start: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		if err := db.UpdateHumanEnabled(deps.DB, id, true); err != nil {
			log.Errorf("Humans API: start human: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// HandleStopHuman returns the POST /api/humans/{id}/stop handler.
func HandleStopHuman(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Humans API: lookup human for stop: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		if err := db.UpdateHumanEnabled(deps.DB, id, false); err != nil {
			log.Errorf("Humans API: stop human: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// adminDisabledRequest is the JSON body for the adminDisabled endpoint.
type adminDisabledRequest struct {
	State *bool `json:"state"`
}

// HandleAdminDisabled returns the POST /api/humans/{id}/adminDisabled handler.
func HandleAdminDisabled(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Humans API: lookup human for adminDisabled: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		var body adminDisabledRequest
		if err := readJSONBody(r, &body); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.State == nil {
			trackingJSONError(w, http.StatusBadRequest, "state is required (true/false)")
			return
		}

		if err := db.UpdateHumanAdminDisable(deps.DB, id, *body.State); err != nil {
			log.Errorf("Humans API: adminDisabled: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		adminDisable := 0
		if *body.State {
			adminDisable = 1
		}

		reloadState(deps)
		trackingJSONOK(w, map[string]any{"admin_disabled": adminDisable})
	}
}

// HandleSwitchProfile returns the POST /api/humans/{id}/switchProfile/{profile} handler.
func HandleSwitchProfile(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Humans API: lookup human for switchProfile: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		profileStr := r.PathValue("profile")
		profileNo := 0
		if _, err := json.Number(profileStr).Int64(); err == nil {
			n, _ := json.Number(profileStr).Int64()
			profileNo = int(n)
		} else {
			trackingJSONError(w, http.StatusBadRequest, "invalid profile number")
			return
		}

		found, err := db.SwitchProfile(deps.DB, id, profileNo)
		if err != nil {
			log.Errorf("Humans API: switchProfile: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if !found {
			trackingJSONError(w, http.StatusNotFound, "Profile not found")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}
