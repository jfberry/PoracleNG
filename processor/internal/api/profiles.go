package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HandleGetProfiles returns the GET /api/profiles/{id} handler.
func HandleGetProfiles(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Profiles API: lookup human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		profiles, err := deps.Humans.GetProfiles(id)
		if err != nil {
			log.Errorf("Profiles API: get profiles: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		trackingJSONOK(c, map[string]any{"profile": profilesToResponse(profiles)})
	}
}

// HandleDeleteProfile returns the DELETE /api/profiles/{id}/byProfileNo/{profile_no} handler.
func HandleDeleteProfile(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		profileNoStr := c.Param("profile_no")
		profileNo, err := strconv.Atoi(profileNoStr)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid profile_no")
			return
		}

		if err := deps.Humans.DeleteProfile(id, profileNo); err != nil {
			log.Errorf("Profiles API: delete profile: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// profileAddRequest represents a single profile add request from the POST body.
type profileAddRequest struct {
	Name        string `json:"name"`
	ActiveHours any    `json:"active_hours"`
}

// HandleAddProfile returns the POST /api/profiles/{id}/add handler.
func HandleAddProfile(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for add: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		// Parse body: single object or array
		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []profileAddRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single profileAddRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []profileAddRequest{single}
		}

		for _, req := range reqs {
			if req.Name == "" {
				trackingJSONError(c, http.StatusBadRequest, "name must be specified")
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

			if err := deps.Humans.AddProfile(id, req.Name, activeHours); err != nil {
				log.Errorf("Profiles API: add profile: %s", err)
				trackingJSONError(c, http.StatusInternalServerError, "Exception raised during execution")
				return
			}
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// profileUpdateRequest represents a single profile update request from the POST body.
type profileUpdateRequest struct {
	ProfileNo   *int `json:"profile_no"`
	ActiveHours any  `json:"active_hours"`
}

// HandleUpdateProfile returns the POST /api/profiles/{id}/update handler.
func HandleUpdateProfile(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for update: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		// Parse body: single object or array
		rawBody, err := readBody(c)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		var reqs []profileUpdateRequest
		if len(rawBody) > 0 && rawBody[0] == '[' {
			if err := json.Unmarshal(rawBody, &reqs); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
		} else {
			var single profileUpdateRequest
			if err := json.Unmarshal(rawBody, &single); err != nil {
				trackingJSONError(c, http.StatusBadRequest, "invalid request body")
				return
			}
			reqs = []profileUpdateRequest{single}
		}

		for _, req := range reqs {
			if req.ProfileNo == nil {
				trackingJSONError(c, http.StatusBadRequest, "profile_no must be specified")
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
				trackingJSONError(c, http.StatusInternalServerError, "Exception raised during execution")
				return
			}
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// HandleCopyProfile returns the POST /api/profiles/{id}/copy/{from}/{to} handler.
func HandleCopyProfile(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Profiles API: lookup human for copy: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		fromStr := c.Param("from")
		toStr := c.Param("to")
		from, err := strconv.Atoi(fromStr)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid from profile number")
			return
		}
		to, err := strconv.Atoi(toStr)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid to profile number")
			return
		}

		if err := db.CopyProfile(deps.DB, id, from, to); err != nil {
			log.Errorf("Profiles API: copy profile: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "Exception raised during execution")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}
