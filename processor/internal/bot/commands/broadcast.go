package commands

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// broadcastTemplate represents one entry in broadcast.json.
type broadcastTemplate struct {
	ID       any             `json:"id"`
	Platform string          `json:"platform"`
	Template json.RawMessage `json:"template"`
}

var (
	broadcastLatLonRe = regexp.MustCompile(`^(-?\d+\.?\d*),\s*(-?\d+\.?\d*)$`)
	broadcastDRe      = regexp.MustCompile(`(?i)^d(\d+)$`)
	broadcastAreaRe   = regexp.MustCompile(`(?i)^@(.+)$`)
)

type BroadcastCommand struct{}

func (c *BroadcastCommand) Name() string      { return "cmd.broadcast" }
func (c *BroadcastCommand) Aliases() []string { return nil }

func (c *BroadcastCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if !ctx.IsAdmin {
		return []bot.Reply{{React: "🙅"}}
	}

	// Parse args
	var areas []string
	var distance float64
	var latitude, longitude float64
	hasLocation := false
	test := false
	var remaining []string

	for i := len(args) - 1; i >= 0; i-- {
		arg := args[i]
		if m := broadcastAreaRe.FindStringSubmatch(arg); m != nil {
			areas = append(areas, m[1])
		} else if m := broadcastDRe.FindStringSubmatch(arg); m != nil {
			fmt.Sscanf(m[1], "%f", &distance)
		} else {
			remaining = append([]string{arg}, remaining...)
		}
	}

	// Check first remaining arg for lat,lon
	if len(remaining) > 0 {
		if m := broadcastLatLonRe.FindStringSubmatch(remaining[0]); m != nil {
			fmt.Sscanf(m[1], "%f", &latitude)
			fmt.Sscanf(m[2], "%f", &longitude)
			hasLocation = true
			remaining = remaining[1:]
		}
	}

	// Check for "test" keyword
	if len(remaining) > 0 && remaining[0] == "test" {
		test = true
		remaining = remaining[1:]
	}

	if !hasLocation && len(areas) == 0 && !test {
		return []bot.Reply{{Text: "No location or areas specified"}}
	}

	if len(remaining) == 0 {
		return []bot.Reply{{Text: "Blank message!"}}
	}

	if hasLocation && distance == 0 {
		return []bot.Reply{{Text: "Location specified without any distance"}}
	}

	messageID := strings.Join(remaining, " ")

	// Load broadcast templates from config/broadcast.json
	broadcastPath := filepath.Join(ctx.Config.BaseDir, "config", "broadcast.json")
	data, err := os.ReadFile(broadcastPath)
	if err != nil {
		return []bot.Reply{{Text: "No broadcast messages defined - create config/broadcast.json"}}
	}

	var templates []broadcastTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		log.Errorf("broadcast: parse broadcast.json: %v", err)
		return []bot.Reply{{Text: "Failed to parse broadcast.json"}}
	}

	// Find matching templates
	var discordTemplate, telegramTemplate json.RawMessage
	for _, t := range templates {
		idStr := fmt.Sprintf("%v", t.ID)
		if idStr != messageID {
			continue
		}
		if t.Platform == "discord" || t.Platform == "" {
			if discordTemplate == nil {
				discordTemplate = t.Template
			}
		}
		if t.Platform == "telegram" || t.Platform == "" {
			if telegramTemplate == nil {
				telegramTemplate = t.Template
			}
		}
	}

	if discordTemplate == nil && telegramTemplate == nil {
		return []bot.Reply{{Text: "Cannot find this broadcast message"}}
	}

	if test {
		// Send to self only
		var msg json.RawMessage
		if strings.HasPrefix(ctx.TargetType, "telegram") {
			msg = telegramTemplate
		} else {
			msg = discordTemplate
		}
		if msg == nil {
			return []bot.Reply{{Text: "You do not have a message defined for this platform"}}
		}

		if ctx.Dispatcher != nil {
			ctx.Dispatcher.Dispatch(&delivery.Job{
				Target:  ctx.TargetID,
				Type:    ctx.TargetType,
				Name:    ctx.TargetName,
				Message: msg,
				TTH:     delivery.TTH{Hours: 1},
			})
		}
		return []bot.Reply{{React: "✅"}}
	}

	// Query for matching humans
	type humanResult struct {
		ID        string  `db:"id"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		Latitude  float64 `db:"latitude"`
		Longitude float64 `db:"longitude"`
	}

	// Build the WHERE clause
	var conditions []string
	var queryArgs []any

	if hasLocation {
		conditions = append(conditions,
			fmt.Sprintf(`(
				ROUND(
					6371000
					* ACOS(
						COS(RADIANS(%f))
						* COS(RADIANS(humans.latitude))
						* COS(RADIANS(humans.longitude) - RADIANS(%f))
						+ SIN(RADIANS(%f))
						* SIN(RADIANS(humans.latitude))
					)
				) < %f
			)`, latitude, longitude, latitude, distance))
	}

	if len(areas) > 0 {
		var areaConds []string
		for _, area := range areas {
			areaConds = append(areaConds, "humans.area LIKE ?")
			queryArgs = append(queryArgs, fmt.Sprintf(`%%"%s"%%`, strings.ReplaceAll(area, "'", "\\'")))
		}
		conditions = append(conditions, "("+strings.Join(areaConds, " OR ")+")")
	}

	if len(conditions) == 0 {
		return []bot.Reply{{Text: "No location or areas specified"}}
	}

	query := fmt.Sprintf(`
		SELECT humans.id, humans.name, humans.type, humans.latitude, humans.longitude
		FROM humans
		WHERE humans.enabled = 1 AND humans.admin_disable = 0 AND humans.type LIKE '%%:user'
		AND (%s)`, strings.Join(conditions, " OR "))

	var results []humanResult
	if err := ctx.DB.Select(&results, query, queryArgs...); err != nil {
		log.Errorf("broadcast: query humans: %v", err)
		return []bot.Reply{{Text: "Failed to query users"}}
	}

	// Filter by actual haversine distance (MySQL ACOS can be imprecise)
	if hasLocation {
		var filtered []humanResult
		for _, r := range results {
			d := haversineDistance(latitude, longitude, r.Latitude, r.Longitude)
			if d < distance || len(areas) > 0 {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// Check all platforms have templates
	for _, r := range results {
		var msg json.RawMessage
		if strings.HasPrefix(r.Type, "telegram") {
			msg = telegramTemplate
		} else {
			msg = discordTemplate
		}
		if msg == nil {
			return []bot.Reply{{Text: "Not sending any messages - You do not have a message defined for all platforms in your distribution list"}}
		}
	}

	// Dispatch to each user
	var names []string
	for _, r := range results {
		var msg json.RawMessage
		if strings.HasPrefix(r.Type, "telegram") {
			msg = telegramTemplate
		} else {
			msg = discordTemplate
		}

		if ctx.Dispatcher != nil {
			ctx.Dispatcher.Dispatch(&delivery.Job{
				Target:  r.ID,
				Type:    r.Type,
				Name:    r.Name,
				Message: msg,
				TTH:     delivery.TTH{Hours: 1},
			})
		}
		names = append(names, r.Name)
	}

	return []bot.Reply{
		{React: "✅"},
		{Text: fmt.Sprintf("I sent your message to %s", strings.Join(names, ", "))},
	}
}

// haversineDistance returns the distance in meters between two lat/lon points.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // earth radius in meters
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
