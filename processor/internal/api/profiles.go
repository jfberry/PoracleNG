package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetProfiles returns the GET /api/profiles/{id} handler.
func HandleGetProfiles(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Profiles API: lookup human: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		profiles, err := db.SelectProfiles(deps.DB, id)
		if err != nil {
			log.Errorf("Profiles API: get profiles: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		trackingJSONOK(w, map[string]any{"profile": profiles})
	}
}

// HandleDeleteProfile returns the DELETE /api/profiles/{id}/byProfileNo/{profile_no} handler.
func HandleDeleteProfile(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		profileNoStr := r.PathValue("profile_no")
		profileNo, err := strconv.Atoi(profileNoStr)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid profile_no")
			return
		}

		if err := db.DeleteProfile(deps.DB, id, profileNo); err != nil {
			log.Errorf("Profiles API: delete profile: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// profileAddRequest represents a single profile add request from the POST body.
type profileAddRequest struct {
	Name        string `json:"name"`
	ActiveHours any    `json:"active_hours"`
}

// HandleAddProfile returns the POST /api/profiles/{id}/add handler.
func HandleAddProfile(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for add: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		// Parse body: single object or array
		var rawBody json.RawMessage
		if err := readJSONBody(r, &rawBody); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []profileAddRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single profileAddRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []profileAddRequest{single}
		}

		for _, req := range reqs {
			if req.Name == "" {
				trackingJSONError(w, http.StatusBadRequest, "name must be specified")
				return
			}

			activeHours := "{}"
			if req.ActiveHours != nil {
				// The JS converts active_hours to a string via JSON.stringify if it's an object,
				// or uses the string directly if already a string.
				switch v := req.ActiveHours.(type) {
				case string:
					activeHours = v
				default:
					b, err := json.Marshal(v)
					if err == nil {
						activeHours = string(b)
					}
				}
			}

			if err := db.AddProfile(deps.DB, id, req.Name, activeHours); err != nil {
				log.Errorf("Profiles API: add profile: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "Exception raised during execution")
				return
			}
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// profileUpdateRequest represents a single profile update request from the POST body.
type profileUpdateRequest struct {
	ProfileNo   *int `json:"profile_no"`
	ActiveHours any  `json:"active_hours"`
}

// HandleUpdateProfile returns the POST /api/profiles/{id}/update handler.
func HandleUpdateProfile(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for update: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		// Parse body: single object or array
		var rawBody json.RawMessage
		if err := readJSONBody(r, &rawBody); err != nil {
			trackingJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []profileUpdateRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single profileUpdateRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []profileUpdateRequest{single}
		}

		for _, req := range reqs {
			if req.ProfileNo == nil {
				trackingJSONError(w, http.StatusBadRequest, "profile_no must be specified")
				return
			}

			activeHours := "{}"
			if req.ActiveHours != nil {
				switch v := req.ActiveHours.(type) {
				case string:
					activeHours = v
				default:
					b, err := json.Marshal(v)
					if err == nil {
						activeHours = string(b)
					}
				}
			}

			if err := db.UpdateProfileHours(deps.DB, id, *req.ProfileNo, activeHours); err != nil {
				log.Errorf("Profiles API: update profile hours: %s", err)
				trackingJSONError(w, http.StatusInternalServerError, "Exception raised during execution")
				return
			}
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}

// HandleCopyProfile returns the POST /api/profiles/{id}/copy/{from}/{to} handler.
func HandleCopyProfile(deps *TrackingDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			trackingJSONError(w, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := db.SelectOneHuman(deps.DB, id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for copy: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(w, http.StatusNotFound, "User not found")
			return
		}

		fromStr := r.PathValue("from")
		toStr := r.PathValue("to")
		from, err := strconv.Atoi(fromStr)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid from profile number")
			return
		}
		to, err := strconv.Atoi(toStr)
		if err != nil {
			trackingJSONError(w, http.StatusBadRequest, "invalid to profile number")
			return
		}

		if err := db.CopyProfile(deps.DB, id, from, to); err != nil {
			log.Errorf("Profiles API: copy profile: %s", err)
			trackingJSONError(w, http.StatusInternalServerError, "Exception raised during execution")
			return
		}

		reloadState(deps)
		trackingJSONOK(w, nil)
	}
}
