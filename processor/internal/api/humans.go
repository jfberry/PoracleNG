package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/community"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// HandleGetOneHuman returns the GET /api/humans/one/{id} handler.
func HandleGetOneHuman(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: get human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		trackingJSONOK(c, map[string]any{"human": humanToResponse(human)})
	}
}

// HandleStartHuman returns the POST /api/humans/{id}/start handler.
func HandleStartHuman(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.GetLite(id)
		if err != nil {
			log.Errorf("Humans API: lookup human for start: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		if err := deps.Humans.SetEnabledWithFails(id); err != nil {
			log.Errorf("Humans API: start human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		tr := translatorFor(deps, human)
		language := resolveLanguage(deps, human)
		silent := isSilent(c)
		message := tr.T("msg.start.success")
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}

		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// HandleStopHuman returns the POST /api/humans/{id}/stop handler.
func HandleStopHuman(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.GetLite(id)
		if err != nil {
			log.Errorf("Humans API: lookup human for stop: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		if err := deps.Humans.SetEnabled(id, false); err != nil {
			log.Errorf("Humans API: stop human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)

		tr := translatorFor(deps, human)
		language := resolveLanguage(deps, human)
		silent := isSilent(c)
		message := tr.Tf("msg.stop.success", commandPrefixForHuman(deps, human), tr.T("cmd.start"))
		if !silent && message != "" {
			sendConfirmation(deps, human, message, language)
		}

		trackingJSONOK(c, map[string]any{"message": message})
	}
}

// adminDisabledRequest is the JSON body for the adminDisabled endpoint.
type adminDisabledRequest struct {
	State *bool `json:"state"`
}

// HandleAdminDisabled returns the POST /api/humans/{id}/adminDisabled handler.
func HandleAdminDisabled(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: lookup human for adminDisabled: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		var body adminDisabledRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}
		if body.State == nil {
			trackingJSONError(c, http.StatusBadRequest, "state is required (true/false)")
			return
		}

		// Flag-only semantics: do NOT use humanStore.SetAdminDisable here,
		// which also clears disabled_date and resets enabled/fails. This
		// endpoint has historically toggled just the admin_disable flag.
		adminDisable := 0
		if *body.State {
			adminDisable = 1
		}
		if err := deps.Humans.Update(id, map[string]any{"admin_disable": adminDisable}); err != nil {
			log.Errorf("Humans API: adminDisabled: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, map[string]any{"admin_disabled": adminDisable})
	}
}

// HandleSwitchProfile returns the POST /api/humans/{id}/switchProfile/{profile} handler.
func HandleSwitchProfile(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: lookup human for switchProfile: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		profileStr := c.Param("profile")
		profileNo := 0
		if _, err := json.Number(profileStr).Int64(); err == nil {
			n, _ := json.Number(profileStr).Int64()
			profileNo = int(n)
		} else {
			trackingJSONError(c, http.StatusBadRequest, "invalid profile number")
			return
		}

		found, err := deps.Humans.SwitchProfile(id, profileNo)
		if err != nil {
			log.Errorf("Humans API: switchProfile: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if !found {
			trackingJSONError(c, http.StatusNotFound, "Profile not found")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// isAdmin returns true if the given ID is in the discord or telegram admin lists.
func isAdmin(deps *TrackingDeps, id string) bool {
	if slices.Contains(deps.Config.Discord.Admins, id) {
		return true
	}
	return slices.Contains(deps.Config.Telegram.Admins, id)
}

// parseMembership parses a JSON array string into a []string, returning nil on error or empty.
func parseMembership(raw string) []string {
	if raw == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}
	return result
}

// areaInfoResponse is one element in the areas array returned by GET /api/humans/{id}.
type areaInfoResponse struct {
	Name           string `json:"name"`
	Group          string `json:"group"`
	Description    string `json:"description"`
	UserSelectable bool   `json:"userSelectable"`
}

// HandleGetHumanAreas returns the GET /api/humans/{id} handler.
// Returns the list of geofence areas available to this user.
func HandleGetHumanAreas(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: get human areas: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		st := deps.StateMgr.Get()

		// Build list of all fence names (lowercased).
		allAreas := make([]string, len(st.Fences))
		for i, f := range st.Fences {
			allAreas[i] = strings.ToLower(f.Name)
		}

		allowedAreas := allAreas
		if deps.Config.Area.Enabled && !isAdmin(deps, id) {
			allowedAreas = community.FilterAreas(
				deps.Config.Area.Communities, human.CommunityMembership, allAreas)
		}

		// Build allowed set for fast lookup.
		allowedSet := make(map[string]bool, len(allowedAreas))
		for _, a := range allowedAreas {
			allowedSet[strings.ToLower(a)] = true
		}

		var areas []areaInfoResponse
		for _, f := range st.Fences {
			if allowedSet[strings.ToLower(f.Name)] {
				areas = append(areas, areaInfoResponse{
					Name:           f.Name,
					Group:          f.Group,
					Description:    f.Description,
					UserSelectable: f.UserSelectable,
				})
			}
		}

		if areas == nil {
			areas = []areaInfoResponse{}
		}
		trackingJSONOK(c, map[string]any{"areas": areas})
	}
}

// HandleCheckLocation returns the GET /api/humans/{id}/checkLocation/{lat}/{lon} handler.
// Validates whether the given lat/lon is within the user's allowed area restriction fences.
func HandleCheckLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: check location: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		if !deps.Config.Area.Enabled {
			trackingJSONOK(c, map[string]any{"locationOk": true})
			return
		}

		lat, err := strconv.ParseFloat(c.Param("lat"), 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid latitude")
			return
		}
		lon, err := strconv.ParseFloat(c.Param("lon"), 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid longitude")
			return
		}

		allowedFences := human.AreaRestriction

		st := deps.StateMgr.Get()
		matched := st.Geofence.MatchedAreaNames(lat, lon)

		locationOk := false
		for _, fence := range allowedFences {
			if matched[strings.ToLower(fence)] {
				locationOk = true
				break
			}
		}

		trackingJSONOK(c, map[string]any{"locationOk": locationOk})
	}
}

// HandleSetLocation returns the POST /api/humans/{id}/setLocation/{lat}/{lon} handler.
// Updates the user's location after validating against area restrictions.
func HandleSetLocation(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: set location: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		lat, err := strconv.ParseFloat(c.Param("lat"), 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid latitude")
			return
		}
		lon, err := strconv.ParseFloat(c.Param("lon"), 64)
		if err != nil {
			trackingJSONError(c, http.StatusBadRequest, "invalid longitude")
			return
		}

		// Validate location against area restrictions if enabled.
		if deps.Config.Area.Enabled && len(human.AreaRestriction) > 0 {
			st := deps.StateMgr.Get()
			matched := st.Geofence.MatchedAreaNames(lat, lon)

			permitted := false
			for _, fence := range human.AreaRestriction {
				if matched[strings.ToLower(fence)] {
					permitted = true
					break
				}
			}
			if !permitted {
				trackingJSONError(c, http.StatusForbidden, "Location not permitted")
				return
			}
		}

		if err := deps.Humans.SetLocation(id, human.CurrentProfileNo, lat, lon); err != nil {
			log.Errorf("Humans API: update location: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, nil)
	}
}

// HandleSetAreas returns the POST /api/humans/{id}/setAreas handler.
// Sets the user's selected areas after validating against allowed areas and community membership.
func HandleSetAreas(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			trackingJSONError(c, http.StatusBadRequest, "missing id parameter")
			return
		}

		human, err := deps.Humans.Get(id)
		if err != nil {
			log.Errorf("Humans API: set areas: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if human == nil {
			trackingJSONError(c, http.StatusNotFound, "User not found")
			return
		}

		var requestedAreas []string
		if err := c.ShouldBindJSON(&requestedAreas); err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		// Lowercase all requested areas.
		for i := range requestedAreas {
			requestedAreas[i] = strings.ToLower(requestedAreas[i])
		}

		st := deps.StateMgr.Get()
		admin := isAdmin(deps, id)

		// Build allowed areas: start with all fences.
		var allowedAreas []string
		for _, f := range st.Fences {
			// Non-admins can only select userSelectable fences.
			if !admin && !f.UserSelectable {
				continue
			}
			allowedAreas = append(allowedAreas, strings.ToLower(f.Name))
		}

		// If area security is enabled and user is not admin, filter by community.
		if deps.Config.Area.Enabled && !admin {
			allowedAreas = community.FilterAreas(
				deps.Config.Area.Communities, human.CommunityMembership, allowedAreas)
		}

		// Build allowed set for intersection.
		allowedSet := make(map[string]bool, len(allowedAreas))
		for _, a := range allowedAreas {
			allowedSet[strings.ToLower(a)] = true
		}

		// Intersect requested with allowed, dedup.
		seen := make(map[string]bool)
		var newAreas []string
		for _, a := range requestedAreas {
			if allowedSet[a] && !seen[a] {
				seen[a] = true
				newAreas = append(newAreas, a)
			}
		}
		if newAreas == nil {
			newAreas = []string{}
		}

		if err := deps.Humans.SetArea(id, human.CurrentProfileNo, newAreas); err != nil {
			log.Errorf("Humans API: update areas: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, map[string]any{"setAreas": newAreas})
	}
}

// createHumanRequest is the JSON body for POST /api/humans.
type createHumanRequest struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Enabled      *bool   `json:"enabled"`
	Area         string  `json:"area"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	AdminDisable *bool   `json:"admin_disable"`
	Language     string  `json:"language"`
	Community    any     `json:"community"` // string or []string
	ProfileName  string  `json:"profile_name"`
	Notes        string  `json:"notes"`
}

// HandleCreateHuman returns the POST /api/humans handler.
// Creates a new user with optional community membership and default profile.
func HandleCreateHuman(deps *TrackingDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body createHumanRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			trackingJSONError(c, http.StatusBadRequest, err.Error())
			return
		}

		if body.ID == "" || body.Name == "" {
			trackingJSONError(c, http.StatusBadRequest, "id and name are required")
			return
		}

		// Check user doesn't already exist.
		existing, err := deps.Humans.Get(body.ID)
		if err != nil {
			log.Errorf("Humans API: create human lookup: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}
		if existing != nil {
			trackingJSONError(c, http.StatusConflict, "User already exists")
			return
		}

		// Build the human record.
		human := &store.Human{
			ID:               body.ID,
			Name:             body.Name,
			Type:             body.Type,
			Enabled:          true,
			Area:             parseMembership(body.Area),
			Latitude:         body.Latitude,
			Longitude:        body.Longitude,
			CurrentProfileNo: 1,
			Notes:            body.Notes,
		}

		if human.Type == "" {
			human.Type = "discord:user"
		}
		if body.Enabled != nil && !*body.Enabled {
			human.Enabled = false
		}
		if body.AdminDisable != nil && *body.AdminDisable {
			human.AdminDisable = true
		}

		lang := body.Language
		if lang == "" {
			lang = deps.Config.General.Locale
			if lang == "" {
				lang = "en"
			}
		}
		human.Language = lang

		// Handle community membership.
		if body.Community != nil {
			var communities []string
			switch v := body.Community.(type) {
			case string:
				if v != "" {
					communities = []string{v}
				}
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						communities = append(communities, s)
					}
				}
			}

			if len(communities) > 0 {
				human.CommunityMembership = communities

				st := deps.StateMgr.Get()
				allAreas := make([]string, len(st.Fences))
				for i, f := range st.Fences {
					allAreas[i] = strings.ToLower(f.Name)
				}
				human.AreaRestriction = community.FilterAreas(
					deps.Config.Area.Communities, communities, allAreas)
			}
		}

		if err := deps.Humans.Create(human); err != nil {
			log.Errorf("Humans API: create human: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		// Create default profile.
		profileName := body.ProfileName
		if profileName == "" {
			profileName = "Default"
		}
		if err := deps.Humans.CreateDefaultProfile(body.ID, profileName, human.Area, human.Latitude, human.Longitude); err != nil {
			log.Errorf("Humans API: create default profile: %s", err)
			trackingJSONError(c, http.StatusInternalServerError, "database error")
			return
		}

		reloadState(deps)
		trackingJSONOK(c, map[string]any{
			"message": "User created successfully",
			"human":   humanToResponse(human),
		})
	}
}
