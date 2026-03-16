package main

import (
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// buildMatchedAreas converts geofence areas to webhook MatchedArea structs.
func buildMatchedAreas(areas []geofence.MatchedArea) []webhook.MatchedArea {
	result := make([]webhook.MatchedArea, len(areas))
	for i, a := range areas {
		result[i] = webhook.MatchedArea{
			Name:             a.Name,
			DisplayInMatches: a.DisplayInMatches,
			Group:            a.Group,
		}
	}
	return result
}

// toInt converts a JSON number (float64) to int.
func toInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	}
	return 0
}
